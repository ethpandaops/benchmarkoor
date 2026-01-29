package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/go-units"
	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
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
	ResultsOwner       *fsutil.OwnerConfig // Optional file ownership for results directory
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
	FullConfig         *config.Config // Full config for resolving per-instance settings
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
	RunStatusFailed        = "failed"
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

// ResolvedResourceLimits contains the resolved resource limits for config.json output.
type ResolvedResourceLimits struct {
	CpusetCpus   string `json:"cpuset_cpus,omitempty"`
	Memory       string `json:"memory,omitempty"`
	MemoryBytes  int64  `json:"memory_bytes,omitempty"`
	SwapDisabled bool   `json:"swap_disabled,omitempty"`
}

// ResolvedInstance contains the resolved configuration for a client instance.
type ResolvedInstance struct {
	ID               string                  `json:"id"`
	Client           string                  `json:"client"`
	Image            string                  `json:"image"`
	ImageSHA256      string                  `json:"image_sha256,omitempty"`
	Entrypoint       []string                `json:"entrypoint,omitempty"`
	Command          []string                `json:"command,omitempty"`
	ExtraArgs        []string                `json:"extra_args,omitempty"`
	PullPolicy       string                  `json:"pull_policy"`
	Restart          string                  `json:"restart,omitempty"`
	Environment      map[string]string       `json:"environment,omitempty"`
	Genesis          string                  `json:"genesis,omitempty"`
	GenesisGroups    map[string]string       `json:"genesis_groups,omitempty"`
	DataDir          *config.DataDirConfig   `json:"datadir,omitempty"`
	ClientVersion    string                  `json:"client_version,omitempty"`
	DropMemoryCaches string                  `json:"drop_memory_caches,omitempty"`
	ResourceLimits   *ResolvedResourceLimits `json:"resource_limits,omitempty"`
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
	if err := fsutil.MkdirAll(r.cfg.ResultsDir, 0755, r.cfg.ResultsOwner); err != nil {
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

// containerLogInfo contains metadata written to container log markers.
type containerLogInfo struct {
	Name             string
	ContainerID      string
	Image            string
	GenesisGroupHash string
}

// formatStartMarker formats a log start marker with container metadata.
func formatStartMarker(marker string, info *containerLogInfo) string {
	s := "#" + marker + ":START name=" + info.Name +
		" image=" + info.Image
	if info.ContainerID != "" {
		s += " container_id=" + info.ContainerID
	}

	if info.GenesisGroupHash != "" {
		s += " genesis_group=" + info.GenesisGroupHash
	}

	return s + "\n"
}

// containerRunParams contains parameters for a single container lifecycle run.
type containerRunParams struct {
	Instance         *config.ClientInstance
	RunID            string
	RunTimestamp     int64
	RunResultsDir    string
	BenchmarkoorLog  *os.File
	LogHook          *fileHook
	GenesisSource    string                    // Path or URL to genesis file.
	Tests            []*executor.TestWithSteps // Optional test subset (nil = all).
	GenesisGroupHash string                    // Non-empty when running a specific genesis group.
	GenesisGroups    map[string]string         // All genesis hash → path mappings (multi-genesis).
	ImageName        string                    // Resolved image name (pulled once by caller).
	ImageDigest      string                    // Image SHA256 digest (resolved once by caller).
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
	if err := fsutil.MkdirAll(runResultsDir, 0755, r.cfg.ResultsOwner); err != nil {
		return fmt.Errorf("creating run results directory: %w", err)
	}

	// Setup benchmarkoor log file for this run.
	benchmarkoorLogFile, err := fsutil.Create(filepath.Join(runResultsDir, "benchmarkoor.log"), r.cfg.ResultsOwner)
	if err != nil {
		return fmt.Errorf("creating benchmarkoor log file: %w", err)
	}
	defer func() { _ = benchmarkoorLogFile.Close() }()

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

	// Pull image once for this instance (shared across genesis groups).
	imageName := instance.Image
	if imageName == "" {
		imageName = spec.DefaultImage()
	}

	if err := r.docker.PullImage(ctx, imageName, instance.PullPolicy); err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	imageDigest, err := r.docker.GetImageDigest(ctx, imageName)
	if err != nil {
		log.WithError(err).Warn("Failed to get image digest")
	} else {
		log.WithField("digest", imageDigest).Debug("Got image digest")
	}

	// Determine genesis source (URL or local file path).
	// Priority: instance config > global config > EEST source
	genesisSource := instance.Genesis
	if genesisSource == "" {
		genesisSource = r.cfg.GenesisURLs[instance.Client]
	}

	// Check for multi-genesis support (EEST pre_alloc).
	if genesisSource == "" && r.executor != nil {
		if ggp, ok := r.executor.GetSource().(executor.GenesisGroupProvider); ok {
			if groups := ggp.GetGenesisGroups(); len(groups) > 0 {
				log.WithField("groups", len(groups)).Info(
					"Running multi-genesis mode",
				)

				genesisGroups := make(map[string]string, len(groups))
				for _, group := range groups {
					genesisGroups[group.GenesisHash] = ggp.GetGenesisPathForGroup(
						group.GenesisHash, instance.Client,
					)
				}

				for i, group := range groups {
					groupGenesis := genesisGroups[group.GenesisHash]
					if groupGenesis == "" {
						return fmt.Errorf(
							"no genesis file for group %s and client %s",
							group.GenesisHash, instance.Client,
						)
					}

					log.WithFields(logrus.Fields{
						"group":        i + 1,
						"total_groups": len(groups),
						"genesis_hash": group.GenesisHash,
						"tests":        len(group.Tests),
					}).Info("Running genesis group")

					params := &containerRunParams{
						Instance:         instance,
						RunID:            runID,
						RunTimestamp:     runTimestamp,
						RunResultsDir:    runResultsDir,
						BenchmarkoorLog:  benchmarkoorLogFile,
						LogHook:          logHook,
						GenesisSource:    groupGenesis,
						Tests:            group.Tests,
						GenesisGroupHash: group.GenesisHash,
						GenesisGroups:    genesisGroups,
						ImageName:        imageName,
						ImageDigest:      imageDigest,
					}

					if err := r.runContainerLifecycle(
						ctx, params, spec, datadirCfg, useDataDir,
					); err != nil {
						return fmt.Errorf(
							"running genesis group %s: %w",
							group.GenesisHash, err,
						)
					}
				}

				return nil
			}
		}
	}

	// If no genesis configured and executor provides one (e.g., EEST source), use that.
	if genesisSource == "" && r.executor != nil {
		if gp, ok := r.executor.GetSource().(executor.GenesisProvider); ok {
			if path := gp.GetGenesisPath(instance.Client); path != "" {
				genesisSource = path
				log.WithField("source", path).Info("Using genesis from test source")
			}
		}
	}

	// Single-genesis path.
	params := &containerRunParams{
		Instance:        instance,
		RunID:           runID,
		RunTimestamp:    runTimestamp,
		RunResultsDir:   runResultsDir,
		BenchmarkoorLog: benchmarkoorLogFile,
		LogHook:         logHook,
		GenesisSource:   genesisSource,
		ImageName:       imageName,
		ImageDigest:     imageDigest,
	}

	return r.runContainerLifecycle(
		ctx, params, spec, datadirCfg, useDataDir,
	)
}

// runContainerLifecycle runs a single container lifecycle: load genesis,
// create container, start, wait for RPC, execute tests, stop.
//
//nolint:gocognit,cyclop // Container lifecycle is inherently complex.
func (r *runner) runContainerLifecycle(
	ctx context.Context,
	params *containerRunParams,
	spec client.Spec,
	datadirCfg *config.DataDirConfig,
	useDataDir bool,
) error {
	instance := params.Instance
	runID := params.RunID
	runResultsDir := params.RunResultsDir
	benchmarkoorLogFile := params.BenchmarkoorLog
	genesisSource := params.GenesisSource

	log := r.log.WithFields(logrus.Fields{
		"instance": instance.ID,
		"run_id":   runID,
	})

	if params.GenesisGroupHash != "" {
		log = log.WithField("genesis_group", params.GenesisGroupHash)
	}

	// Each container lifecycle manages its own cleanup and crash detection.
	var localCleanupFuncs []func()

	localCleanupStarted := make(chan struct{})

	var localCleanupOnce sync.Once

	defer func() {
		localCleanupOnce.Do(func() { close(localCleanupStarted) })

		for i := len(localCleanupFuncs) - 1; i >= 0; i-- {
			localCleanupFuncs[i]()
		}
	}()

	// Setup data directory: either Docker volume or copied datadir.
	// Each container lifecycle gets a fresh volume/datadir.
	var dataMount docker.Mount

	if useDataDir {
		log.WithFields(logrus.Fields{
			"source": datadirCfg.SourceDir,
			"method": datadirCfg.Method,
		}).Info("Using pre-populated data directory")

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

		localCleanupFuncs = append(localCleanupFuncs, func() {
			if cleanupErr := prepared.Cleanup(); cleanupErr != nil {
				log.WithError(cleanupErr).Warn("Failed to cleanup datadir")
			}
		})

		containerDir := datadirCfg.ContainerDir
		if containerDir == "" {
			containerDir = spec.DataDir()
		}

		dataMount = docker.Mount{
			Type:   "bind",
			Source: prepared.MountPath,
			Target: containerDir,
		}
	} else {
		volumeSuffix := instance.ID
		if params.GenesisGroupHash != "" {
			volumeSuffix = instance.ID + "-" + params.GenesisGroupHash
		}

		volumeName := fmt.Sprintf("benchmarkoor-%s-%s", runID, volumeSuffix)
		volumeLabels := map[string]string{
			"benchmarkoor.instance":   instance.ID,
			"benchmarkoor.client":     instance.Client,
			"benchmarkoor.run-id":     runID,
			"benchmarkoor.managed-by": "benchmarkoor",
		}

		if err := r.docker.CreateVolume(
			ctx, volumeName, volumeLabels,
		); err != nil {
			return fmt.Errorf("creating volume: %w", err)
		}

		localCleanupFuncs = append(localCleanupFuncs, func() {
			if rmErr := r.docker.RemoveVolume(
				context.Background(), volumeName,
			); rmErr != nil {
				log.WithError(rmErr).Warn("Failed to remove volume")
			}
		})

		dataMount = docker.Mount{
			Type:   "volume",
			Source: volumeName,
			Target: spec.DataDir(),
		}
	}

	// Load genesis file if configured.
	var genesisContent []byte

	if genesisSource != "" {
		log.WithField("source", genesisSource).Info("Loading genesis file")

		var loadErr error

		genesisContent, loadErr = r.loadFile(ctx, genesisSource)
		if loadErr != nil {
			return fmt.Errorf("loading genesis: %w", loadErr)
		}
	} else {
		log.Info("No genesis configured, skipping genesis setup")
	}

	// Fail if neither genesis nor datadir is configured.
	if genesisSource == "" && !useDataDir {
		return fmt.Errorf(
			"no genesis file or datadir configured for client %s",
			instance.Client,
		)
	}

	// Image is already pulled by RunInstance; use the resolved name and digest.
	imageName := params.ImageName
	imageDigest := params.ImageDigest

	// Create temp files for genesis and JWT.
	tempDir, err := os.MkdirTemp(
		r.cfg.TmpCacheDir, "benchmarkoor-"+instance.ID+"-",
	)
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}

	localCleanupFuncs = append(localCleanupFuncs, func() {
		if rmErr := os.RemoveAll(tempDir); rmErr != nil {
			log.WithError(rmErr).Warn("Failed to remove temp directory")
		}
	})

	// Write genesis file to temp dir if genesis is configured.
	var genesisFile string

	if genesisSource != "" {
		genesisFile = filepath.Join(tempDir, "genesis.json")
		if err := os.WriteFile(genesisFile, genesisContent, 0644); err != nil {
			return fmt.Errorf("writing genesis file: %w", err)
		}
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
			Source:   jwtFile,
			Target:   spec.JWTPath(),
			ReadOnly: true,
		},
	}

	// Add genesis mount if genesis is configured.
	if genesisFile != "" {
		mounts = append(mounts, docker.Mount{
			Type:     "bind",
			Source:   genesisFile,
			Target:   spec.GenesisPath(),
			ReadOnly: true,
		})
	}

	// Run init container if required (skip when using datadir or no genesis).
	if spec.RequiresInit() && !useDataDir && genesisSource != "" {
		log.Info("Running init container")

		initSuffix := "init"
		if params.GenesisGroupHash != "" {
			initSuffix = "init-" + params.GenesisGroupHash
		}

		initSpec := &docker.ContainerSpec{
			Name: fmt.Sprintf(
				"benchmarkoor-%s-%s-%s", runID, instance.ID, initSuffix,
			),
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

		// Set up init container log streaming (appends to container.log).
		initLogFile := filepath.Join(runResultsDir, "container.log")

		initFile, err := os.OpenFile(
			initLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644,
		)
		if err != nil {
			return fmt.Errorf("opening init log file: %w", err)
		}

		if r.cfg.ResultsOwner != nil {
			fsutil.Chown(initLogFile, r.cfg.ResultsOwner)
		}

		_, _ = fmt.Fprint(initFile, formatStartMarker("INIT_CONTAINER", &containerLogInfo{
			Name:             initSpec.Name,
			Image:            initSpec.Image,
			GenesisGroupHash: params.GenesisGroupHash,
		}))

		var initStdout, initStderr io.Writer = initFile, initFile
		if r.cfg.ClientLogsToStdout {
			prefix := fmt.Sprintf("🟣 [%s-init] ", instance.ID)
			stdoutPrefixWriter := &prefixedWriter{
				prefix: prefix, writer: os.Stdout,
			}
			logFilePrefixWriter := &prefixedWriter{
				prefix: prefix, writer: benchmarkoorLogFile,
			}
			initStdout = io.MultiWriter(
				initFile, stdoutPrefixWriter, logFilePrefixWriter,
			)
			initStderr = io.MultiWriter(
				initFile, stdoutPrefixWriter, logFilePrefixWriter,
			)
		}

		if err := r.docker.RunInitContainer(
			ctx, initSpec, initStdout, initStderr,
		); err != nil {
			_, _ = fmt.Fprintf(initFile, "#INIT_CONTAINER:END\n")
			_ = initFile.Close()

			return fmt.Errorf("running init container: %w", err)
		}

		_, _ = fmt.Fprintf(initFile, "#INIT_CONTAINER:END\n")
		_ = initFile.Close()

		log.Info("Init container completed")
	} else if spec.RequiresInit() && genesisSource == "" {
		log.Info("Skipping init container (no genesis configured)")
	} else if useDataDir {
		log.Info("Skipping init container (using pre-populated datadir)")
	}

	// Determine command.
	cmd := make([]string, len(instance.Command))
	copy(cmd, instance.Command)

	if len(cmd) == 0 {
		cmd = spec.DefaultCommand()
	}

	// Add genesis flag if genesis is configured and client uses a genesis flag.
	if genesisSource != "" && spec.GenesisFlag() != "" {
		cmd = append(cmd, spec.GenesisFlag()+spec.GenesisPath())
	}

	// Append extra args if provided.
	if len(instance.ExtraArgs) > 0 {
		cmd = append(cmd, instance.ExtraArgs...)
	}

	// Build environment (default first, instance overrides).
	env := make(
		map[string]string,
		len(spec.DefaultEnvironment())+len(instance.Environment),
	)
	for k, v := range spec.DefaultEnvironment() {
		env[k] = v
	}

	for k, v := range instance.Environment {
		env[k] = v
	}

	// Resolve drop_memory_caches setting.
	var dropMemoryCaches string
	if r.cfg.FullConfig != nil {
		dropMemoryCaches = r.cfg.FullConfig.GetDropMemoryCaches(instance)
	}

	// Resolve resource limits.
	var dockerResourceLimits *docker.ResourceLimits
	var resolvedResourceLimits *ResolvedResourceLimits

	if r.cfg.FullConfig != nil {
		resourceLimitsCfg := r.cfg.FullConfig.GetResourceLimits(instance)
		if resourceLimitsCfg != nil {
			var err error

			dockerResourceLimits, resolvedResourceLimits, err =
				buildDockerResourceLimits(resourceLimitsCfg)
			if err != nil {
				return fmt.Errorf("building resource limits: %w", err)
			}

			log.WithFields(logrus.Fields{
				"cpuset_cpus":   resolvedResourceLimits.CpusetCpus,
				"memory":        resolvedResourceLimits.Memory,
				"swap_disabled": resolvedResourceLimits.SwapDisabled,
			}).Info("Resource limits configured")
		}
	}

	// Write run configuration with resolved values.
	runConfig := &RunConfig{
		Timestamp: params.RunTimestamp,
		System:    getSystemInfo(),
		Instance: &ResolvedInstance{
			ID:               instance.ID,
			Client:           instance.Client,
			Image:            imageName,
			ImageSHA256:      imageDigest,
			Entrypoint:       instance.Entrypoint,
			Command:          cmd,
			ExtraArgs:        instance.ExtraArgs,
			PullPolicy:       instance.PullPolicy,
			Restart:          instance.Restart,
			Environment:      env,
			DataDir:          datadirCfg,
			DropMemoryCaches: dropMemoryCaches,
			ResourceLimits:   resolvedResourceLimits,
		},
	}

	if len(params.GenesisGroups) > 0 {
		runConfig.Instance.GenesisGroups = params.GenesisGroups
	} else {
		runConfig.Instance.Genesis = genesisSource
	}

	if r.executor != nil {
		runConfig.SuiteHash = r.executor.GetSuiteHash()
	}

	if err := writeRunConfig(
		runResultsDir, runConfig, r.cfg.ResultsOwner,
	); err != nil {
		log.WithError(err).Warn("Failed to write run config")
	}

	// Build container spec.
	containerName := fmt.Sprintf("benchmarkoor-%s-%s", runID, instance.ID)
	if params.GenesisGroupHash != "" {
		containerName = fmt.Sprintf(
			"benchmarkoor-%s-%s-%s",
			runID, instance.ID, params.GenesisGroupHash,
		)
	}

	containerSpec := &docker.ContainerSpec{
		Name:           containerName,
		Image:          imageName,
		Entrypoint:     instance.Entrypoint,
		Command:        cmd,
		Env:            env,
		Mounts:         mounts,
		NetworkName:    r.cfg.DockerNetwork,
		ResourceLimits: dockerResourceLimits,
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
	localCleanupFuncs = append(localCleanupFuncs, func() {
		log.Info("Removing container")

		if rmErr := r.docker.RemoveContainer(
			context.Background(), containerID,
		); rmErr != nil {
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

		if err := r.streamLogs(
			logCtx, instance.ID, containerID, logFile, benchmarkoorLogFile,
			&containerLogInfo{
				Name:             containerName,
				ContainerID:      containerID,
				Image:            imageName,
				GenesisGroupHash: params.GenesisGroupHash,
			},
		); err != nil {
			// Context cancellation during cleanup is expected.
			select {
			case <-localCleanupStarted:
				log.WithError(err).Debug("Log streaming stopped")
			default:
				log.WithError(err).Warn("Log streaming error")
			}
		}
	}()

	// Start container.
	if err := r.docker.StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	log.Info("Container started")

	// Start container death monitoring.
	execCtx, execCancel := context.WithCancel(ctx)
	defer execCancel()

	var containerDied bool
	var containerExitCode *int64
	var mu sync.Mutex

	containerExitCh, containerErrCh := r.docker.WaitForContainerExit(
		ctx, containerID,
	)

	r.wg.Add(1)

	go func() {
		defer r.wg.Done()

		select {
		case exitCode := <-containerExitCh:
			mu.Lock()
			containerDied = true
			containerExitCode = &exitCode
			mu.Unlock()

			select {
			case <-localCleanupStarted:
				log.WithField("exit_code", exitCode).Debug(
					"Container stopped during cleanup",
				)
			default:
				log.WithField("exit_code", exitCode).Warn(
					"Container exited unexpectedly",
				)
			}

			execCancel()
		case err := <-containerErrCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				log.WithError(err).Warn("Container wait error")
			}
		case <-r.done:
			// Runner is stopping.
		}
	}()

	// Get container IP for health checks.
	containerIP, err := r.docker.GetContainerIP(
		ctx, containerID, r.cfg.DockerNetwork,
	)
	if err != nil {
		return fmt.Errorf("getting container IP: %w", err)
	}

	log.WithField("ip", containerIP).Debug("Container IP address")

	// Wait for RPC to be ready.
	clientVersion, err := r.waitForRPC(execCtx, containerIP, spec.RPCPort())
	if err != nil {
		mu.Lock()
		if containerDied {
			runConfig.Status = RunStatusContainerDied
			runConfig.TerminationReason = fmt.Sprintf(
				"container exited while waiting for RPC: %v", err,
			)
			runConfig.ContainerExitCode = containerExitCode
		} else {
			runConfig.Status = RunStatusFailed
			runConfig.TerminationReason = fmt.Sprintf(
				"waiting for RPC: %v", err,
			)
		}
		mu.Unlock()

		if writeErr := writeRunConfig(
			runResultsDir, runConfig, r.cfg.ResultsOwner,
		); writeErr != nil {
			log.WithError(writeErr).Warn(
				"Failed to write run config with failed status",
			)
		}

		return fmt.Errorf("waiting for RPC: %w", err)
	}

	log.WithField("version", clientVersion).Info("RPC endpoint ready")

	// Log the latest block info.
	if blockNum, blockHash, blkErr := r.getLatestBlock(
		execCtx, containerIP, spec.RPCPort(),
	); blkErr != nil {
		log.WithError(blkErr).Warn("Failed to get latest block")
	} else {
		log.WithFields(logrus.Fields{
			"block_number": blockNum,
			"block_hash":   blockHash,
		}).Info("Latest block")
	}

	// Update config with client version.
	runConfig.Instance.ClientVersion = clientVersion

	if err := writeRunConfig(
		runResultsDir, runConfig, r.cfg.ResultsOwner,
	); err != nil {
		log.WithError(err).Warn(
			"Failed to update run config with client version",
		)
	}

	// Wait additional time.
	select {
	case <-time.After(r.cfg.ReadyWaitAfter):
		log.Info("Wait period complete")
	case <-execCtx.Done():
		mu.Lock()
		died := containerDied
		mu.Unlock()

		if died {
			log.Warn("Container died during wait period")
		} else {
			return ctx.Err()
		}
	}

	// Execute tests if executor is configured.
	if r.executor != nil {
		log.Info("Starting test execution")

		var dropCachesPath string
		if r.cfg.FullConfig != nil {
			dropCachesPath = r.cfg.FullConfig.GetDropCachesPath()
		}

		execOpts := &executor.ExecuteOptions{
			EngineEndpoint: fmt.Sprintf(
				"http://%s:%d", containerIP, spec.EnginePort(),
			),
			JWT:              r.cfg.JWT,
			ResultsDir:       runResultsDir,
			Filter:           r.cfg.TestFilter,
			ContainerID:      containerID,
			DockerClient:     r.docker.GetClient(),
			DropMemoryCaches: dropMemoryCaches,
			DropCachesPath:   dropCachesPath,
			Tests:            params.Tests,
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

			if result.StatsReaderType != "" {
				runConfig.SystemResourceCollectionMethod = result.StatsReaderType
			}

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
	if err := writeRunConfig(
		runResultsDir, runConfig, r.cfg.ResultsOwner,
	); err != nil {
		log.WithError(err).Warn("Failed to write final run config with status")
	} else {
		log.WithField("status", runConfig.Status).Info("Run completed")
	}

	// Return an error if the container died so callers (e.g. multi-genesis
	// loop) stop instead of continuing with the next group.
	if containerDied {
		return fmt.Errorf("container died during execution")
	}

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
	defer func() { _ = resp.Body.Close() }()

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
func writeRunConfig(resultsDir string, cfg *RunConfig, owner *fsutil.OwnerConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling run config: %w", err)
	}

	configPath := filepath.Join(resultsDir, "config.json")
	if err := fsutil.WriteFile(configPath, data, 0644, owner); err != nil {
		return fmt.Errorf("writing config.json: %w", err)
	}

	return nil
}

// streamLogs streams container logs to file and optionally stdout/benchmarkoor log.
// The log file is opened in append mode with start/end markers so that
// multiple container runs (e.g. multi-genesis) write to a single file.
func (r *runner) streamLogs(
	ctx context.Context,
	instanceID, containerID, logPath string,
	benchmarkoorLog io.Writer,
	logInfo *containerLogInfo,
) error {
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer func() { _ = file.Close() }()

	if r.cfg.ResultsOwner != nil {
		fsutil.Chown(logPath, r.cfg.ResultsOwner)
	}

	// Write start marker with container metadata.
	_, _ = fmt.Fprint(file, formatStartMarker("CONTAINER", logInfo))

	var stdout, stderr io.Writer = file, file

	if r.cfg.ClientLogsToStdout {
		prefix := fmt.Sprintf("🟣 [%s] ", instanceID)
		stdoutPrefixWriter := &prefixedWriter{prefix: prefix, writer: os.Stdout}
		logFilePrefixWriter := &prefixedWriter{prefix: prefix, writer: benchmarkoorLog}
		stdout = io.MultiWriter(file, stdoutPrefixWriter, logFilePrefixWriter)
		stderr = io.MultiWriter(file, stdoutPrefixWriter, logFilePrefixWriter)
	}

	streamErr := r.docker.StreamLogs(ctx, containerID, stdout, stderr)

	// Write end marker (best-effort, even if streaming failed).
	_, _ = fmt.Fprintf(file, "#CONTAINER:END\n")

	return streamErr
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
	defer func() { _ = resp.Body.Close() }()

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

// getLatestBlock fetches the latest block number and hash from the RPC endpoint.
func (r *runner) getLatestBlock(ctx context.Context, host string, port int) (uint64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://%s:%d", host, port)
	body := `{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":1}`

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return 0, "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("reading response: %w", err)
	}

	var rpcResp struct {
		Result struct {
			Number string `json:"number"`
			Hash   string `json:"hash"`
		} `json:"result"`
	}

	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return 0, "", fmt.Errorf("parsing response: %w", err)
	}

	// Parse hex block number.
	blockNum, err := strconv.ParseUint(strings.TrimPrefix(rpcResp.Result.Number, "0x"), 16, 64)
	if err != nil {
		return 0, "", fmt.Errorf("parsing block number: %w", err)
	}

	return blockNum, rpcResp.Result.Hash, nil
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

// selectRandomCPUs picks count random CPUs from available CPUs using Fisher-Yates shuffle.
func selectRandomCPUs(count int) ([]int, error) {
	numCPUs, err := cpu.Counts(true)
	if err != nil {
		return nil, fmt.Errorf("getting CPU count: %w", err)
	}

	if count > numCPUs {
		return nil, fmt.Errorf("requested %d CPUs but only %d available", count, numCPUs)
	}

	// Create slice of all CPU IDs.
	cpus := make([]int, numCPUs)
	for i := range cpus {
		cpus[i] = i
	}

	// Fisher-Yates shuffle (partial - only shuffle first 'count' elements).
	for i := 0; i < count; i++ {
		j := i + mrand.IntN(numCPUs-i)
		cpus[i], cpus[j] = cpus[j], cpus[i]
	}

	return cpus[:count], nil
}

// cpusetString converts a slice of CPU IDs to a comma-separated string.
func cpusetString(cpus []int) string {
	if len(cpus) == 0 {
		return ""
	}

	strs := make([]string, len(cpus))
	for i, c := range cpus {
		strs[i] = strconv.Itoa(c)
	}

	return strings.Join(strs, ",")
}

// buildDockerResourceLimits builds docker.ResourceLimits from config.ResourceLimits.
func buildDockerResourceLimits(cfg *config.ResourceLimits) (*docker.ResourceLimits, *ResolvedResourceLimits, error) {
	if cfg == nil {
		return nil, nil, nil
	}

	dockerLimits := &docker.ResourceLimits{}
	resolved := &ResolvedResourceLimits{}

	// Handle CPU pinning.
	if cfg.CpusetCount != nil {
		cpus, err := selectRandomCPUs(*cfg.CpusetCount)
		if err != nil {
			return nil, nil, fmt.Errorf("selecting random CPUs: %w", err)
		}

		dockerLimits.CpusetCpus = cpusetString(cpus)
		resolved.CpusetCpus = dockerLimits.CpusetCpus
	} else if len(cfg.Cpuset) > 0 {
		dockerLimits.CpusetCpus = cpusetString(cfg.Cpuset)
		resolved.CpusetCpus = dockerLimits.CpusetCpus
	}

	// Handle memory limit.
	if cfg.Memory != "" {
		memBytes, err := units.RAMInBytes(cfg.Memory)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing memory limit: %w", err)
		}

		dockerLimits.MemoryBytes = memBytes
		resolved.Memory = cfg.Memory
		resolved.MemoryBytes = memBytes

		// Handle swap.
		if cfg.SwapDisabled {
			// Set memory-swap equal to memory to disable swap.
			dockerLimits.MemorySwapBytes = memBytes
			// Set swappiness to 0.
			swappiness := int64(0)
			dockerLimits.MemorySwappiness = &swappiness
			resolved.SwapDisabled = true
		}
	}

	return dockerLimits, resolved, nil
}
