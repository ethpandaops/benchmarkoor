package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/sirupsen/logrus"
)

const (
	// DefaultReadyTimeout is the default timeout for waiting for RPC to be ready.
	DefaultReadyTimeout = 120 * time.Second

	// DefaultReadyWaitAfter is the default wait time after RPC is ready.
	DefaultReadyWaitAfter = 10 * time.Second

	// DefaultHealthCheckInterval is the interval between health checks.
	DefaultHealthCheckInterval = 1 * time.Second
)

// Runner orchestrates client container lifecycle.
type Runner interface {
	Start(ctx context.Context) error
	Stop() error

	// RunInstance runs a single client instance through its lifecycle.
	RunInstance(ctx context.Context, instance *config.ClientInstance) error

	// RunAll runs all configured instances sequentially.
	RunAll(ctx context.Context) error
}

// Config for the runner.
type Config struct {
	ResultsDir         string
	ClientLogsToStdout bool
	DockerNetwork      string
	JWT                string
	GenesisURLs        map[string]string
	DataDirs           map[string]*config.DataDirConfig
	TmpDataDir         string // Directory for temporary datadir copies (empty = system default)
	TmpCacheDir        string // Directory for temporary cache files (empty = system default)
	ReadyTimeout       time.Duration
	ReadyWaitAfter     time.Duration
	TestFilter         string
}

// RunConfig contains configuration for a single test run.
type RunConfig struct {
	Timestamp                      int64             `json:"timestamp"`
	SuiteHash                      string            `json:"suite_hash,omitempty"`
	SystemResourceCollectionMethod string            `json:"system_resource_collection_method,omitempty"`
	System                         *SystemInfo       `json:"system"`
	Instance                       *ResolvedInstance `json:"instance"`
	Status                         string            `json:"status,omitempty"`
	TerminationReason              string            `json:"termination_reason,omitempty"`
	ContainerExitCode              *int64            `json:"container_exit_code,omitempty"`
}

// Run status constants.
const (
	RunStatusCompleted     = "completed"
	RunStatusContainerDied = "container_died"
	RunStatusCancelled     = "cancelled"
)

// SystemInfo contains system hardware and OS information.
type SystemInfo struct {
	Hostname           string  `json:"hostname"`
	OS                 string  `json:"os"`
	Platform           string  `json:"platform"`
	PlatformVersion    string  `json:"platform_version"`
	KernelVersion      string  `json:"kernel_version"`
	Arch               string  `json:"arch"`
	Virtualization     string  `json:"virtualization,omitempty"`
	VirtualizationRole string  `json:"virtualization_role,omitempty"`
	CPUVendor          string  `json:"cpu_vendor"`
	CPUModel           string  `json:"cpu_model"`
	CPUCores           int     `json:"cpu_cores"`
	CPUMhz             float64 `json:"cpu_mhz"`
	CPUCacheKB         int     `json:"cpu_cache_kb"`
	MemoryTotalGB      float64 `json:"memory_total_gb"`
}

// ResolvedInstance contains the resolved configuration for a client instance.
type ResolvedInstance struct {
	ID            string                `json:"id"`
	Client        string                `json:"client"`
	Image         string                `json:"image"`
	ImageSHA256   string                `json:"image_sha256,omitempty"`
	Entrypoint    []string              `json:"entrypoint,omitempty"`
	Command       []string              `json:"command,omitempty"`
	ExtraArgs     []string              `json:"extra_args,omitempty"`
	PullPolicy    string                `json:"pull_policy"`
	Restart       string                `json:"restart,omitempty"`
	Environment   map[string]string     `json:"environment,omitempty"`
	Genesis       string                `json:"genesis"`
	DataDir       *config.DataDirConfig `json:"datadir,omitempty"`
	ClientVersion string                `json:"client_version,omitempty"`
}

// NewRunner creates a new runner instance.
func NewRunner(
	log *logrus.Logger,
	cfg *Config,
	dockerMgr docker.Manager,
	registry client.Registry,
	exec executor.Executor,
) Runner {
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = DefaultReadyTimeout
	}

	if cfg.ReadyWaitAfter == 0 {
		cfg.ReadyWaitAfter = DefaultReadyWaitAfter
	}

	return &runner{
		logger:   log,
		log:      log.WithField("component", "runner"),
		cfg:      cfg,
		docker:   dockerMgr,
		registry: registry,
		executor: exec,
		done:     make(chan struct{}),
	}
}

type runner struct {
	logger   *logrus.Logger     // The actual logger (for hook management)
	log      logrus.FieldLogger // The field logger (for logging with fields)
	cfg      *Config
	docker   docker.Manager
	registry client.Registry
	executor executor.Executor
	done     chan struct{}
	wg       sync.WaitGroup
}

// Ensure interface compliance.
var _ Runner = (*runner)(nil)

// Start initializes the runner.
func (r *runner) Start(ctx context.Context) error {
	// Ensure results directory exists.
	if err := os.MkdirAll(r.cfg.ResultsDir, 0755); err != nil {
		return fmt.Errorf("creating results directory: %w", err)
	}

	// Ensure Docker network exists.
	if err := r.docker.EnsureNetwork(ctx, r.cfg.DockerNetwork); err != nil {
		return fmt.Errorf("ensuring docker network: %w", err)
	}

	r.log.Debug("Runner started")

	return nil
}

// Stop cleans up the runner.
func (r *runner) Stop() error {
	close(r.done)
	r.wg.Wait()

	r.log.Debug("Runner stopped")

	return nil
}

// RunAll runs all configured instances sequentially.
func (r *runner) RunAll(ctx context.Context) error {
	// This would be called with all instances from config.
	// For now, it's a placeholder - the actual implementation
	// would iterate over instances.
	return nil
}

// resolveDataDir returns the datadir config for an instance.
// Instance-level datadir takes precedence over global datadirs.
func (r *runner) resolveDataDir(instance *config.ClientInstance) *config.DataDirConfig {
	// Instance-level override takes precedence.
	if instance.DataDir != nil {
		return instance.DataDir
	}

	// Fall back to global datadir for this client type.
	if r.cfg.DataDirs != nil {
		return r.cfg.DataDirs[instance.Client]
	}

	return nil
}

// RunInstance runs a single client instance through its lifecycle.
func (r *runner) RunInstance(ctx context.Context, instance *config.ClientInstance) error {
	// Generate a short random ID for this run.
	runID := generateShortID()
	runTimestamp := time.Now().Unix()

	// Create run results directory under runs/.
	runResultsDir := filepath.Join(
		r.cfg.ResultsDir, "runs",
		fmt.Sprintf("%d_%s_%s", runTimestamp, runID, instance.ID),
	)
	if err := os.MkdirAll(runResultsDir, 0755); err != nil {
		return fmt.Errorf("creating run results directory: %w", err)
	}

	// Setup benchmarkoor log file for this run.
	benchmarkoorLogFile, err := os.Create(filepath.Join(runResultsDir, "benchmarkoor.log"))
	if err != nil {
		return fmt.Errorf("creating benchmarkoor log file: %w", err)
	}
	defer benchmarkoorLogFile.Close()

	logHook := &fileHook{
		writer:    benchmarkoorLogFile,
		formatter: r.logger.Formatter,
	}
	r.logger.AddHook(logHook)
	defer r.removeHook(logHook)

	log := r.log.WithFields(logrus.Fields{
		"instance": instance.ID,
		"run_id":   runID,
	})
	log.Info("Starting client instance")

	// Get client spec.
	spec, err := r.registry.Get(client.ClientType(instance.Client))
	if err != nil {
		return fmt.Errorf("getting client spec: %w", err)
	}

	// Resolve datadir configuration.
	datadirCfg := r.resolveDataDir(instance)
	useDataDir := datadirCfg != nil

	// Track cleanup functions.
	var cleanupFuncs []func()

	defer func() {
		for _, cleanup := range cleanupFuncs {
			cleanup()
		}
	}()

	// Setup data directory: either Docker volume or copied datadir.
	var dataMount docker.Mount

	var volumeName string

	if useDataDir {
		log.WithFields(logrus.Fields{
			"source": datadirCfg.SourceDir,
			"method": datadirCfg.Method,
		}).Info("Using pre-populated data directory")

		// Create provider based on configured method.
		provider, err := datadir.NewProvider(log, datadirCfg.Method)
		if err != nil {
			return fmt.Errorf("creating datadir provider: %w", err)
		}

		prepared, err := provider.Prepare(ctx, &datadir.ProviderConfig{
			SourceDir:  datadirCfg.SourceDir,
			InstanceID: instance.ID,
			TmpDir:     r.cfg.TmpDataDir,
		})
		if err != nil {
			return fmt.Errorf("preparing datadir: %w", err)
		}

		cleanupFuncs = append(cleanupFuncs, func() {
			if cleanupErr := prepared.Cleanup(); cleanupErr != nil {
				log.WithError(cleanupErr).Warn("Failed to cleanup datadir")
			}
		})

		// Use bind mount for the prepared data.
		dataMount = docker.Mount{
			Type:   "bind",
			Source: prepared.MountPath,
			Target: datadirCfg.ContainerDir,
		}
	} else {
		// Create Docker volume for this run.
		volumeName = fmt.Sprintf("benchmarkoor-%s-%s", runID, instance.ID)
		volumeLabels := map[string]string{
			"benchmarkoor.instance":   instance.ID,
			"benchmarkoor.client":     instance.Client,
			"benchmarkoor.run-id":     runID,
			"benchmarkoor.managed-by": "benchmarkoor",
		}

		if err := r.docker.CreateVolume(ctx, volumeName, volumeLabels); err != nil {
			return fmt.Errorf("creating volume: %w", err)
		}

		cleanupFuncs = append(cleanupFuncs, func() {
			if rmErr := r.docker.RemoveVolume(context.Background(), volumeName); rmErr != nil {
				log.WithError(rmErr).Warn("Failed to remove volume")
			}
		})

		dataMount = docker.Mount{
			Type:   "volume",
			Source: volumeName,
			Target: spec.DataDir(),
		}
	}

	// Determine genesis source (URL or local file path).
	genesisSource := instance.Genesis
	if genesisSource == "" {
		genesisSource = r.cfg.GenesisURLs[instance.Client]
	}

	if genesisSource == "" {
		return fmt.Errorf("no genesis configured for client %s", instance.Client)
	}

	// Load genesis file (from URL or local path).
	log.WithField("source", genesisSource).Info("Loading genesis file")

	genesisContent, err := r.loadFile(ctx, genesisSource)
	if err != nil {
		return fmt.Errorf("loading genesis: %w", err)
	}

	// Determine image.
	imageName := instance.Image
	if imageName == "" {
		imageName = spec.DefaultImage()
	}

	// Pull image.
	if err := r.docker.PullImage(ctx, imageName, instance.PullPolicy); err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	// Get image digest.
	imageDigest, err := r.docker.GetImageDigest(ctx, imageName)
	if err != nil {
		log.WithError(err).Warn("Failed to get image digest")
	} else {
		log.WithField("digest", imageDigest).Debug("Got image digest")
	}

	// Create temp files for genesis and JWT.
	tempDir, err := os.MkdirTemp(r.cfg.TmpCacheDir, "benchmarkoor-"+instance.ID+"-")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}

	cleanupFuncs = append(cleanupFuncs, func() {
		if rmErr := os.RemoveAll(tempDir); rmErr != nil {
			log.WithError(rmErr).Warn("Failed to remove temp directory")
		}
	})

	genesisFile := filepath.Join(tempDir, "genesis.json")
	if err := os.WriteFile(genesisFile, genesisContent, 0644); err != nil {
		return fmt.Errorf("writing genesis file: %w", err)
	}

	jwtFile := filepath.Join(tempDir, "jwtsecret")
	if err := os.WriteFile(jwtFile, []byte(r.cfg.JWT), 0644); err != nil {
		return fmt.Errorf("writing jwt file: %w", err)
	}

	// Build container mounts.
	mounts := []docker.Mount{
		dataMount,
		{
			Type:     "bind",
			Source:   genesisFile,
			Target:   spec.GenesisPath(),
			ReadOnly: true,
		},
		{
			Type:     "bind",
			Source:   jwtFile,
			Target:   spec.JWTPath(),
			ReadOnly: true,
		},
	}

	// Run init container if required (skip when using datadir).
	if spec.RequiresInit() && !useDataDir {
		log.Info("Running init container")

		initSpec := &docker.ContainerSpec{
			Name:        fmt.Sprintf("benchmarkoor-%s-%s-init", runID, instance.ID),
			Image:       imageName,
			Command:     spec.InitCommand(),
			Mounts:      mounts,
			NetworkName: r.cfg.DockerNetwork,
			Labels: map[string]string{
				"benchmarkoor.instance":   instance.ID,
				"benchmarkoor.client":     instance.Client,
				"benchmarkoor.run-id":     runID,
				"benchmarkoor.type":       "init",
				"benchmarkoor.managed-by": "benchmarkoor",
			},
		}

		// Set up init container log streaming.
		initLogFile := filepath.Join(runResultsDir, "container-init.log")

		initFile, err := os.Create(initLogFile)
		if err != nil {
			return fmt.Errorf("creating init log file: %w", err)
		}

		var initStdout, initStderr io.Writer = initFile, initFile
		if r.cfg.ClientLogsToStdout {
			prefix := fmt.Sprintf("[%s-init] ", instance.ID)
			prefixWriter := &prefixedWriter{prefix: prefix, writer: os.Stdout}
			initStdout = io.MultiWriter(initFile, prefixWriter)
			initStderr = io.MultiWriter(initFile, prefixWriter)
		}

		if err := r.docker.RunInitContainer(ctx, initSpec, initStdout, initStderr); err != nil {
			initFile.Close()

			return fmt.Errorf("running init container: %w", err)
		}

		initFile.Close()

		log.Info("Init container completed")
	} else if useDataDir {
		log.Info("Skipping init container (using pre-populated datadir)")
	}

	// Determine command.
	cmd := instance.Command
	if len(cmd) == 0 {
		cmd = spec.DefaultCommand()
	}

	// Append extra args if provided.
	if len(instance.ExtraArgs) > 0 {
		cmd = append(cmd, instance.ExtraArgs...)
	}

	// Build environment (default first, instance overrides).
	env := make(map[string]string, len(spec.DefaultEnvironment())+len(instance.Environment))
	for k, v := range spec.DefaultEnvironment() {
		env[k] = v
	}

	for k, v := range instance.Environment {
		env[k] = v
	}

	// Write run configuration with resolved values.
	runConfig := &RunConfig{
		Timestamp: runTimestamp,
		System:    getSystemInfo(),
		Instance: &ResolvedInstance{
			ID:          instance.ID,
			Client:      instance.Client,
			Image:       imageName,
			ImageSHA256: imageDigest,
			Entrypoint:  instance.Entrypoint,
			Command:     cmd,
			ExtraArgs:   instance.ExtraArgs,
			PullPolicy:  instance.PullPolicy,
			Restart:     instance.Restart,
			Environment: env,
			Genesis:     genesisSource,
			DataDir:     datadirCfg,
		},
	}

	if r.executor != nil {
		runConfig.SuiteHash = r.executor.GetSuiteHash()
	}

	if err := writeRunConfig(runResultsDir, runConfig); err != nil {
		log.WithError(err).Warn("Failed to write run config")
	}

	// Build container spec.
	containerSpec := &docker.ContainerSpec{
		Name:        fmt.Sprintf("benchmarkoor-%s-%s", runID, instance.ID),
		Image:       imageName,
		Entrypoint:  instance.Entrypoint,
		Command:     cmd,
		Env:         env,
		Mounts:      mounts,
		NetworkName: r.cfg.DockerNetwork,
		Labels: map[string]string{
			"benchmarkoor.instance":   instance.ID,
			"benchmarkoor.client":     instance.Client,
			"benchmarkoor.run-id":     runID,
			"benchmarkoor.managed-by": "benchmarkoor",
		},
	}

	// Create container.
	containerID, err := r.docker.CreateContainer(ctx, containerSpec)
	if err != nil {
		return fmt.Errorf("creating container: %w", err)
	}

	// Ensure cleanup.
	cleanupFuncs = append(cleanupFuncs, func() {
		log.Info("Removing container")

		if rmErr := r.docker.RemoveContainer(context.Background(), containerID); rmErr != nil {
			log.WithError(rmErr).Warn("Failed to remove container")
		}
	})

	// Setup log streaming.
	logCtx, logCancel := context.WithCancel(ctx)
	defer logCancel()

	logFile := filepath.Join(runResultsDir, "container.log")

	r.wg.Add(1)

	go func() {
		defer r.wg.Done()

		if err := r.streamLogs(logCtx, instance.ID, containerID, logFile); err != nil {
			log.WithError(err).Warn("Log streaming error")
		}
	}()

	// Start container.
	if err := r.docker.StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	log.Info("Container started")

	// Start container death monitoring.
	// Create a child context for execution that gets cancelled when container dies.
	execCtx, execCancel := context.WithCancel(ctx)
	defer execCancel()

	var containerDied bool
	var containerExitCode *int64
	var mu sync.Mutex

	containerExitCh, containerErrCh := r.docker.WaitForContainerExit(ctx, containerID)

	r.wg.Add(1)

	go func() {
		defer r.wg.Done()

		select {
		case exitCode := <-containerExitCh:
			mu.Lock()
			containerDied = true
			containerExitCode = &exitCode
			mu.Unlock()

			log.WithField("exit_code", exitCode).Warn("Container exited unexpectedly")
			execCancel() // Cancel test execution context.
		case err := <-containerErrCh:
			if err != nil && err != context.Canceled {
				log.WithError(err).Warn("Container wait error")
			}
		case <-r.done:
			// Runner is stopping.
		}
	}()

	// Get container IP for health checks.
	containerIP, err := r.docker.GetContainerIP(ctx, containerID, r.cfg.DockerNetwork)
	if err != nil {
		return fmt.Errorf("getting container IP: %w", err)
	}

	log.WithField("ip", containerIP).Debug("Container IP address")

	// Wait for RPC to be ready.
	clientVersion, err := r.waitForRPC(ctx, containerIP, spec.RPCPort())
	if err != nil {
		return fmt.Errorf("waiting for RPC: %w", err)
	}

	log.WithField("version", clientVersion).Info("RPC endpoint ready")

	// Update config with client version.
	runConfig.Instance.ClientVersion = clientVersion

	if err := writeRunConfig(runResultsDir, runConfig); err != nil {
		log.WithError(err).Warn("Failed to update run config with client version")
	}

	// Wait additional time.
	select {
	case <-time.After(r.cfg.ReadyWaitAfter):
		log.Info("Wait period complete")
	case <-ctx.Done():
		return ctx.Err()
	}

	// Execute tests if executor is configured.
	if r.executor != nil {
		log.Info("Starting test execution")

		execOpts := &executor.ExecuteOptions{
			EngineEndpoint: fmt.Sprintf("http://%s:%d", containerIP, spec.EnginePort()),
			JWT:            r.cfg.JWT,
			ResultsDir:     runResultsDir,
			Filter:         r.cfg.TestFilter,
			ContainerID:    containerID,
			DockerClient:   r.docker.GetClient(),
		}

		result, err := r.executor.ExecuteTests(execCtx, execOpts)
		if err != nil {
			log.WithError(err).Error("Test execution failed")
		} else {
			log.WithFields(logrus.Fields{
				"total":    result.TotalTests,
				"passed":   result.Passed,
				"failed":   result.Failed,
				"duration": result.TotalDuration,
			}).Info("Test execution completed")

			// Update config with stats reader type if available.
			if result.StatsReaderType != "" {
				runConfig.SystemResourceCollectionMethod = result.StatsReaderType
			}

			// Propagate container death info from executor result.
			if result.ContainerDied {
				mu.Lock()
				containerDied = true
				mu.Unlock()
			}
		}
	}

	// Determine final run status.
	mu.Lock()
	if containerDied {
		runConfig.Status = RunStatusContainerDied
		runConfig.TerminationReason = "container exited during test execution"
		runConfig.ContainerExitCode = containerExitCode
	} else if ctx.Err() != nil {
		runConfig.Status = RunStatusCancelled
		runConfig.TerminationReason = "run was cancelled"
	} else {
		runConfig.Status = RunStatusCompleted
	}
	mu.Unlock()

	// Write final config with status.
	if err := writeRunConfig(runResultsDir, runConfig); err != nil {
		log.WithError(err).Warn("Failed to write final run config with status")
	} else {
		log.WithField("status", runConfig.Status).Info("Run completed")
	}

	// Cleanup happens in defer.
	return nil
}

// loadFile loads content from a URL or local file path.
func (r *runner) loadFile(ctx context.Context, source string) ([]byte, error) {
	// Check if source is a URL.
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return r.downloadFromURL(ctx, source)
	}

	// Treat as local file path.
	return r.readFromFile(source)
}

// downloadFromURL downloads content from a URL.
func (r *runner) downloadFromURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return data, nil
}

// readFromFile reads content from a local file.
func (r *runner) readFromFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}

	return data, nil
}

// getSystemInfo gathers system hardware and OS information.
func getSystemInfo() *SystemInfo {
	info := &SystemInfo{}

	if hostInfo, err := host.Info(); err == nil {
		info.Hostname = hostInfo.Hostname
		info.OS = hostInfo.OS
		info.Platform = hostInfo.Platform
		info.PlatformVersion = hostInfo.PlatformVersion
		info.KernelVersion = hostInfo.KernelVersion
		info.Arch = hostInfo.KernelArch
		info.Virtualization = hostInfo.VirtualizationSystem
		info.VirtualizationRole = hostInfo.VirtualizationRole
	}

	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		info.CPUVendor = cpuInfo[0].VendorID
		info.CPUModel = cpuInfo[0].ModelName
		info.CPUMhz = cpuInfo[0].Mhz
		info.CPUCacheKB = int(cpuInfo[0].CacheSize)
	}

	if cores, err := cpu.Counts(false); err == nil {
		info.CPUCores = cores
	}

	if memInfo, err := mem.VirtualMemory(); err == nil {
		info.MemoryTotalGB = float64(memInfo.Total) / (1024 * 1024 * 1024)
	}

	return info
}

// writeRunConfig writes the run configuration to config.json.
func writeRunConfig(resultsDir string, cfg *RunConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling run config: %w", err)
	}

	configPath := filepath.Join(resultsDir, "config.json")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("writing config.json: %w", err)
	}

	return nil
}

// streamLogs streams container logs to file and optionally stdout.
func (r *runner) streamLogs(ctx context.Context, instanceID, containerID, logPath string) error {
	file, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("creating log file: %w", err)
	}
	defer file.Close()

	var stdout, stderr io.Writer = file, file

	if r.cfg.ClientLogsToStdout {
		prefix := fmt.Sprintf("[%s] ", instanceID)
		prefixWriter := &prefixedWriter{prefix: prefix, writer: os.Stdout}
		stdout = io.MultiWriter(file, prefixWriter)
		stderr = io.MultiWriter(file, prefixWriter)
	}

	return r.docker.StreamLogs(ctx, containerID, stdout, stderr)
}

// waitForRPC waits for the RPC endpoint to be ready and returns the client version.
func (r *runner) waitForRPC(ctx context.Context, host string, port int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.ReadyTimeout)
	defer cancel()

	url := fmt.Sprintf("http://%s:%d", host, port)

	ticker := time.NewTicker(DefaultHealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timeout waiting for RPC: %w", ctx.Err())
		case <-ticker.C:
			if version, ok := r.checkRPCHealth(ctx, url); ok {
				return version, nil
			}
		}
	}
}

// checkRPCHealth performs a single RPC health check and returns the client version on success.
func (r *runner) checkRPCHealth(ctx context.Context, url string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	body := `{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}`

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return "", false
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}

	var rpcResp struct {
		Result string `json:"result"`
	}

	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return "", false
	}

	return rpcResp.Result, true
}

// prefixedWriter adds a prefix to each line written.
type prefixedWriter struct {
	prefix string
	writer io.Writer
	buf    []byte
}

func (w *prefixedWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	w.buf = append(w.buf, p...)

	for {
		idx := -1

		for i, b := range w.buf {
			if b == '\n' {
				idx = i

				break
			}
		}

		if idx == -1 {
			break
		}

		line := w.buf[:idx+1]
		w.buf = w.buf[idx+1:]

		if _, err := fmt.Fprintf(w.writer, "%s%s", w.prefix, line); err != nil {
			return n, err
		}
	}

	return n, nil
}

// fileHook writes log entries to a file.
type fileHook struct {
	writer    io.Writer
	formatter logrus.Formatter
}

func (h *fileHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *fileHook) Fire(entry *logrus.Entry) error {
	line, err := h.formatter.Format(entry)
	if err != nil {
		return err
	}

	_, err = h.writer.Write(line)

	return err
}

// removeHook removes a hook from the logger.
func (r *runner) removeHook(hook logrus.Hook) {
	for level, hooks := range r.logger.Hooks {
		filtered := make([]logrus.Hook, 0, len(hooks))

		for _, h := range hooks {
			if h != hook {
				filtered = append(filtered, h)
			}
		}

		r.logger.Hooks[level] = filtered
	}
}

// generateShortID generates a short random hex ID (8 characters).
func generateShortID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails.
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}

	return hex.EncodeToString(b)
}
