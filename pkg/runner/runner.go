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
	"github.com/ethpandaops/benchmarkoor/pkg/blocklog"
	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/cpufreq"
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
	CpusetCpus    string               `json:"cpuset_cpus,omitempty"`
	Memory        string               `json:"memory,omitempty"`
	MemoryBytes   int64                `json:"memory_bytes,omitempty"`
	SwapDisabled  bool                 `json:"swap_disabled,omitempty"`
	BlkioConfig   *ResolvedBlkioConfig `json:"blkio_config,omitempty"`
	CPUFreqKHz    *uint64              `json:"cpu_freq_khz,omitempty"`
	CPUTurboBoost *bool                `json:"cpu_turboboost,omitempty"`
	CPUGovernor   string               `json:"cpu_freq_governor,omitempty"`
}

// ResolvedBlkioConfig contains the resolved blkio configuration for config.json output.
type ResolvedBlkioConfig struct {
	DeviceReadBps   []ResolvedThrottleDevice `json:"device_read_bps,omitempty"`
	DeviceReadIOps  []ResolvedThrottleDevice `json:"device_read_iops,omitempty"`
	DeviceWriteBps  []ResolvedThrottleDevice `json:"device_write_bps,omitempty"`
	DeviceWriteIOps []ResolvedThrottleDevice `json:"device_write_iops,omitempty"`
}

// ResolvedThrottleDevice contains a resolved throttle device for config.json output.
type ResolvedThrottleDevice struct {
	Path string `json:"path"`
	Rate uint64 `json:"rate"`
}

// ResolvedInstance contains the resolved configuration for a client instance.
type ResolvedInstance struct {
	ID                   string                             `json:"id"`
	Client               string                             `json:"client"`
	Image                string                             `json:"image"`
	ImageSHA256          string                             `json:"image_sha256,omitempty"`
	Entrypoint           []string                           `json:"entrypoint,omitempty"`
	Command              []string                           `json:"command,omitempty"`
	ExtraArgs            []string                           `json:"extra_args,omitempty"`
	PullPolicy           string                             `json:"pull_policy"`
	Restart              string                             `json:"restart,omitempty"`
	Environment          map[string]string                  `json:"environment,omitempty"`
	Genesis              string                             `json:"genesis,omitempty"`
	GenesisGroups        map[string]string                  `json:"genesis_groups,omitempty"`
	DataDir              *config.DataDirConfig              `json:"datadir,omitempty"`
	ClientVersion        string                             `json:"client_version,omitempty"`
	RollbackStrategy     string                             `json:"rollback_strategy,omitempty"`
	DropMemoryCaches     string                             `json:"drop_memory_caches,omitempty"`
	ResourceLimits       *ResolvedResourceLimits            `json:"resource_limits,omitempty"`
	BlockExecutionWarmup *config.BlockExecutionWarmupConfig `json:"block_execution_warmup,omitempty"`
}

// NewRunner creates a new runner instance.
func NewRunner(
	log *logrus.Logger,
	cfg *Config,
	dockerMgr docker.Manager,
	registry client.Registry,
	exec executor.Executor,
	cpufreqMgr cpufreq.Manager,
) Runner {
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = DefaultReadyTimeout
	}

	return &runner{
		logger:     log,
		log:        log.WithField("component", "runner"),
		cfg:        cfg,
		docker:     dockerMgr,
		registry:   registry,
		executor:   exec,
		cpufreqMgr: cpufreqMgr,
		done:       make(chan struct{}),
	}
}

type runner struct {
	logger     *logrus.Logger     // The actual logger (for hook management)
	log        logrus.FieldLogger // The field logger (for logging with fields)
	cfg        *Config
	docker     docker.Manager
	registry   client.Registry
	executor   executor.Executor
	cpufreqMgr cpufreq.Manager
	done       chan struct{}
	wg         sync.WaitGroup
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
	Instance          *config.ClientInstance
	RunID             string
	RunTimestamp      int64
	RunResultsDir     string
	BenchmarkoorLog   *os.File
	LogHook           *fileHook
	GenesisSource     string                    // Path or URL to genesis file.
	Tests             []*executor.TestWithSteps // Optional test subset (nil = all).
	GenesisGroupHash  string                    // Non-empty when running a specific genesis group.
	GenesisGroups     map[string]string         // All genesis hash → path mappings (multi-genesis).
	ImageName         string                    // Resolved image name (pulled once by caller).
	ImageDigest       string                    // Image SHA256 digest (resolved once by caller).
	ContainerSpec     *docker.ContainerSpec     // Saved for container-recreate strategy.
	DataDirCfg        *config.DataDirConfig     // Resolved datadir config (nil if not using datadir).
	UseDataDir        bool                      // Whether a pre-populated datadir is used.
	BlockLogCollector blocklog.Collector        // Optional collector for capturing block logs.
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
			pfxFn := clientLogPrefix(instance.ID + "-init")
			stdoutPrefixWriter := &prefixedWriter{
				prefixFn: pfxFn, writer: os.Stdout,
			}
			logFilePrefixWriter := &prefixedWriter{
				prefixFn: pfxFn, writer: benchmarkoorLogFile,
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

	// Resolve block execution warmup config.
	var blockExecutionWarmup *config.BlockExecutionWarmupConfig
	if r.cfg.FullConfig != nil {
		blockExecutionWarmup = r.cfg.FullConfig.GetBlockExecutionWarmup(instance)
	}

	// Resolve resource limits.
	var dockerResourceLimits *docker.ResourceLimits
	var resolvedResourceLimits *ResolvedResourceLimits
	var targetCPUs []int // CPUs to apply cpu_freq settings to

	if r.cfg.FullConfig != nil {
		resourceLimitsCfg := r.cfg.FullConfig.GetResourceLimits(instance)
		if resourceLimitsCfg != nil {
			var err error

			dockerResourceLimits, resolvedResourceLimits, err =
				buildDockerResourceLimits(resourceLimitsCfg)
			if err != nil {
				return fmt.Errorf("building resource limits: %w", err)
			}

			fields := logrus.Fields{
				"cpuset_cpus":   resolvedResourceLimits.CpusetCpus,
				"memory":        resolvedResourceLimits.Memory,
				"swap_disabled": resolvedResourceLimits.SwapDisabled,
			}

			if resolvedResourceLimits.BlkioConfig != nil {
				fields["blkio_read_bps_devices"] = len(resolvedResourceLimits.BlkioConfig.DeviceReadBps)
				fields["blkio_write_bps_devices"] = len(resolvedResourceLimits.BlkioConfig.DeviceWriteBps)
				fields["blkio_read_iops_devices"] = len(resolvedResourceLimits.BlkioConfig.DeviceReadIOps)
				fields["blkio_write_iops_devices"] = len(resolvedResourceLimits.BlkioConfig.DeviceWriteIOps)
			}

			log.WithFields(fields).Info("Resource limits configured")

			// Determine target CPUs for cpu_freq settings.
			// Use the resolved cpuset if available.
			if resolvedResourceLimits.CpusetCpus != "" {
				for _, cpuStr := range strings.Split(resolvedResourceLimits.CpusetCpus, ",") {
					if cpuID, err := strconv.Atoi(strings.TrimSpace(cpuStr)); err == nil {
						targetCPUs = append(targetCPUs, cpuID)
					}
				}
			}

			// Apply CPU frequency settings if configured.
			if r.cpufreqMgr != nil && hasCPUFreqSettings(resourceLimitsCfg) {
				cpufreqCfg := buildCPUFreqConfig(resourceLimitsCfg)

				if err := r.cpufreqMgr.Apply(ctx, cpufreqCfg, targetCPUs); err != nil {
					return fmt.Errorf("applying CPU frequency settings: %w", err)
				}

				// Log CPU frequency info.
				logCPUFreqInfo(log, r.cpufreqMgr, targetCPUs)

				// Add restore to cleanup.
				localCleanupFuncs = append(localCleanupFuncs, func() {
					if restoreErr := r.cpufreqMgr.Restore(context.Background()); restoreErr != nil {
						log.WithError(restoreErr).Warn("Failed to restore CPU frequency settings")
					}
				})

				// Update resolved limits with CPU freq info.
				if cpufreqCfg.Frequency != "" && strings.ToUpper(cpufreqCfg.Frequency) != "MAX" {
					if freqKHz, err := cpufreq.ParseFrequency(cpufreqCfg.Frequency); err == nil {
						resolvedResourceLimits.CPUFreqKHz = &freqKHz
					}
				}
				resolvedResourceLimits.CPUTurboBoost = cpufreqCfg.TurboBoost
				resolvedResourceLimits.CPUGovernor = cpufreqCfg.Governor
			}
		}
	}

	// Write run configuration with resolved values.
	runConfig := &RunConfig{
		Timestamp: params.RunTimestamp,
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
			DataDir:     datadirCfg,
			RollbackStrategy: func() string {
				if r.cfg.FullConfig != nil {
					return r.cfg.FullConfig.GetRollbackStrategy(instance)
				}
				return ""
			}(),
			DropMemoryCaches:     dropMemoryCaches,
			ResourceLimits:       resolvedResourceLimits,
			BlockExecutionWarmup: blockExecutionWarmup,
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

	// Save container spec and datadir info for runner-level rollback strategies.
	params.ContainerSpec = containerSpec
	params.DataDirCfg = datadirCfg
	params.UseDataDir = useDataDir

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

	logFilePath := filepath.Join(runResultsDir, "container.log")

	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening container log file: %w", err)
	}

	localCleanupFuncs = append(localCleanupFuncs, func() {
		_ = logFile.Close()
	})

	if r.cfg.ResultsOwner != nil {
		fsutil.Chown(logFilePath, r.cfg.ResultsOwner)
	}

	// Create block log collector to capture JSON payloads from client logs.
	blockLogParser := blocklog.NewParser(client.ClientType(instance.Client))
	blockLogCollector := blocklog.NewCollector(blockLogParser, logFile)
	params.BlockLogCollector = blockLogCollector

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
			blockLogCollector,
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

	// Execute tests if executor is configured.
	if r.executor != nil {
		log.Info("Starting test execution")

		var dropCachesPath string
		if r.cfg.FullConfig != nil {
			dropCachesPath = r.cfg.FullConfig.GetDropCachesPath()
		}

		// Resolve rollback strategy.
		var rollbackStrategy string
		if r.cfg.FullConfig != nil {
			rollbackStrategy = r.cfg.FullConfig.GetRollbackStrategy(instance)
		}

		isRunnerLevel := rollbackStrategy == config.RollbackStrategyContainerRecreate

		var (
			result  *executor.ExecutionResult
			execErr error
		)

		if isRunnerLevel {
			// Runner-level strategies intentionally stop and restart
			// containers. Signal cleanup-started so the death monitor
			// treats container exits as expected (debug-level logging),
			// and cancel execCtx so the monitor's execCancel() is a no-op.
			localCleanupOnce.Do(func() { close(localCleanupStarted) })
			execCancel()

			result, execErr = r.runTestsWithContainerStrategy(
				ctx, params, spec, containerID, containerIP,
				rollbackStrategy, dropMemoryCaches, dropCachesPath,
				blockExecutionWarmup, runResultsDir, &logCancel, benchmarkoorLogFile,
				&localCleanupFuncs, localCleanupStarted,
			)
		} else {
			execOpts := &executor.ExecuteOptions{
				EngineEndpoint: fmt.Sprintf(
					"http://%s:%d", containerIP, spec.EnginePort(),
				),
				JWT:                   r.cfg.JWT,
				ResultsDir:            runResultsDir,
				Filter:                r.cfg.TestFilter,
				ContainerID:           containerID,
				DockerClient:          r.docker.GetClient(),
				DropMemoryCaches:      dropMemoryCaches,
				DropCachesPath:        dropCachesPath,
				RollbackStrategy:      rollbackStrategy,
				ClientRPCRollbackSpec: spec.RPCRollbackSpec(),
				RPCEndpoint: fmt.Sprintf(
					"http://%s:%d", containerIP, spec.RPCPort(),
				),
				Tests:                params.Tests,
				BlockLogCollector:    params.BlockLogCollector,
				BlockExecutionWarmup: blockExecutionWarmup,
			}

			result, execErr = r.executor.ExecuteTests(execCtx, execOpts)
		}

		if execErr != nil {
			log.WithError(execErr).Error("Test execution failed")

			mu.Lock()
			runConfig.Status = RunStatusFailed
			runConfig.TerminationReason = fmt.Sprintf(
				"test execution failed: %v", execErr,
			)
			mu.Unlock()
		}

		if result != nil {
			log.WithFields(logrus.Fields{
				"total":    result.TotalTests,
				"passed":   result.Passed,
				"failed":   result.Failed,
				"duration": result.TotalDuration,
			}).Info("Test execution completed")

			if result.StatsReaderType != "" {
				runConfig.SystemResourceCollectionMethod = result.StatsReaderType
			}

			if isRunnerLevel {
				// Runner-level strategies intentionally stop containers,
				// which causes the death monitor to set containerDied.
				// Reset it and trust only the strategy's result.
				mu.Lock()
				containerDied = result.ContainerDied
				containerExitCode = nil
				mu.Unlock()
			} else if result.ContainerDied {
				mu.Lock()
				containerDied = true
				mu.Unlock()
			}
		}
	}

	// Determine final run status (don't overwrite if already set by executor).
	mu.Lock()
	if containerDied {
		runConfig.Status = RunStatusContainerDied
		runConfig.TerminationReason = "container exited during test execution"
		runConfig.ContainerExitCode = containerExitCode
	} else if ctx.Err() != nil {
		runConfig.Status = RunStatusCancelled
		runConfig.TerminationReason = "run was cancelled"
	} else if runConfig.Status == "" {
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

	// Write block logs if any were captured.
	if params.BlockLogCollector != nil {
		blockLogs := params.BlockLogCollector.GetBlockLogs()
		if len(blockLogs) > 0 {
			if err := executor.WriteBlockLogsResult(
				runResultsDir, blockLogs, r.cfg.ResultsOwner,
			); err != nil {
				log.WithError(err).Warn("Failed to write block logs result")
			} else {
				log.WithField("count", len(blockLogs)).Info("Block logs written")
			}
		}
	}

	// Return an error if the container died so callers (e.g. multi-genesis
	// loop) stop instead of continuing with the next group.
	if containerDied {
		return fmt.Errorf("container died during execution")
	}

	return nil
}

// runTestsWithContainerStrategy executes tests one at a time, manipulating
// the container between tests according to the given strategy.
//
//nolint:gocognit,cyclop // Per-test container manipulation is inherently complex.
func (r *runner) runTestsWithContainerStrategy(
	ctx context.Context,
	params *containerRunParams,
	spec client.Spec,
	containerID string,
	containerIP string,
	strategy string,
	dropMemoryCaches string,
	dropCachesPath string,
	blockExecutionWarmup *config.BlockExecutionWarmupConfig,
	resultsDir string,
	logCancel *context.CancelFunc,
	benchmarkoorLog *os.File,
	cleanupFuncs *[]func(),
	cleanupStarted chan struct{},
) (*executor.ExecutionResult, error) {
	log := r.log.WithFields(logrus.Fields{
		"instance": params.Instance.ID,
		"run_id":   params.RunID,
		"strategy": strategy,
	})

	// Resolve the test list.
	tests := params.Tests
	if tests == nil {
		tests = r.executor.GetTests()
	}

	if len(tests) == 0 {
		return &executor.ExecutionResult{}, nil
	}

	log.WithField("tests", len(tests)).Info(
		"Running tests with container-level rollback strategy",
	)

	combined := &executor.ExecutionResult{}
	startTime := time.Now()
	currentContainerID := containerID
	currentContainerIP := containerIP

	for i, test := range tests {
		select {
		case <-ctx.Done():
			(*logCancel)()
			combined.TotalDuration = time.Since(startTime)

			return combined, ctx.Err()
		default:
		}

		testLog := log.WithFields(logrus.Fields{
			"test":  test.Name,
			"index": fmt.Sprintf("%d/%d", i+1, len(tests)),
		})

		// Restore state before test.
		switch {
		case strategy == config.RollbackStrategyContainerRecreate && i > 0:
			testLog.Info("Recreating container for next test")

			// Cancel previous log streaming.
			(*logCancel)()

			// Stop and remove the current container.
			if err := r.docker.StopContainer(
				ctx, currentContainerID,
			); err != nil {
				testLog.WithError(err).Warn("Failed to stop container")
			}

			if err := r.docker.RemoveContainer(
				ctx, currentContainerID,
			); err != nil {
				testLog.WithError(err).Warn("Failed to remove container")
			}

			// Create a fresh data volume/datadir for the new container.
			newSpec := *params.ContainerSpec
			newSpec.Name = fmt.Sprintf("%s-%d", params.ContainerSpec.Name, i)
			newSpec.Mounts = make([]docker.Mount, len(params.ContainerSpec.Mounts))
			copy(newSpec.Mounts, params.ContainerSpec.Mounts)

			freshMount, mountCleanup, err := r.createFreshDataMount(
				ctx, params, spec, i,
			)
			if err != nil {
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf(
					"creating fresh data mount for test %d: %w", i, err,
				)
			}

			if mountCleanup != nil {
				*cleanupFuncs = append(*cleanupFuncs, mountCleanup)
			}

			// Replace the data mount (index 0) with the fresh one.
			newSpec.Mounts[0] = freshMount

			// Run init container if required to populate the fresh volume.
			if spec.RequiresInit() && !params.UseDataDir &&
				params.GenesisSource != "" {
				testLog.Info("Running init container for fresh volume")

				initMounts := make([]docker.Mount, len(newSpec.Mounts))
				copy(initMounts, newSpec.Mounts)

				if err := r.runInitForRecreate(
					ctx, params, spec, initMounts, resultsDir,
					benchmarkoorLog, i,
				); err != nil {
					combined.TotalDuration = time.Since(startTime)

					return combined, fmt.Errorf(
						"running init container for test %d: %w", i, err,
					)
				}
			}

			newID, err := r.docker.CreateContainer(ctx, &newSpec)
			if err != nil {
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf("creating container for test %d: %w", i, err)
			}

			currentContainerID = newID

			// Update cleanup to remove this container on exit.
			*cleanupFuncs = append(*cleanupFuncs, func() {
				if rmErr := r.docker.RemoveContainer(
					context.Background(), newID,
				); rmErr != nil {
					testLog.WithError(rmErr).Warn(
						"Failed to remove recreated container",
					)
				}
			})

			// Start fresh log streaming.
			var logCtx context.Context
			var newLogCancel context.CancelFunc
			logCtx, newLogCancel = context.WithCancel(ctx)
			*logCancel = newLogCancel

			// Open log file for this container (append mode).
			recreateLogPath := filepath.Join(resultsDir, "container.log")
			recreateLogFile, logErr := os.OpenFile(
				recreateLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644,
			)
			if logErr != nil {
				(*logCancel)()
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf("opening log file for test %d: %w", i, logErr)
			}

			*cleanupFuncs = append(*cleanupFuncs, func() {
				_ = recreateLogFile.Close()
			})

			r.wg.Add(1)

			go func() {
				defer r.wg.Done()

				if err := r.streamLogs(
					logCtx, params.Instance.ID, newID, recreateLogFile,
					benchmarkoorLog,
					&containerLogInfo{
						Name:             newSpec.Name,
						ContainerID:      newID,
						Image:            newSpec.Image,
						GenesisGroupHash: params.GenesisGroupHash,
					},
					params.BlockLogCollector,
				); err != nil {
					select {
					case <-cleanupStarted:
					default:
						testLog.WithError(err).Debug("Log streaming ended")
					}
				}
			}()

			// Start the new container.
			if err := r.docker.StartContainer(ctx, newID); err != nil {
				(*logCancel)()
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf("starting container for test %d: %w", i, err)
			}

			// Get new container IP.
			newIP, err := r.docker.GetContainerIP(
				ctx, newID, r.cfg.DockerNetwork,
			)
			if err != nil {
				(*logCancel)()
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf("getting container IP for test %d: %w", i, err)
			}

			currentContainerIP = newIP

			// Wait for RPC to be ready.
			if _, err := r.waitForRPC(
				ctx, currentContainerIP, spec.RPCPort(),
			); err != nil {
				(*logCancel)()
				combined.TotalDuration = time.Since(startTime)

				return combined, fmt.Errorf("waiting for RPC on test %d: %w", i, err)
			}

		}

		testLog.Info("Executing test")

		// Execute single test via executor with no executor-level rollback.
		execOpts := &executor.ExecuteOptions{
			EngineEndpoint: fmt.Sprintf(
				"http://%s:%d", currentContainerIP, spec.EnginePort(),
			),
			JWT:              r.cfg.JWT,
			ResultsDir:       resultsDir,
			Filter:           r.cfg.TestFilter,
			ContainerID:      currentContainerID,
			DockerClient:     r.docker.GetClient(),
			DropMemoryCaches: dropMemoryCaches,
			DropCachesPath:   dropCachesPath,
			RollbackStrategy: config.RollbackStrategyNone,
			RPCEndpoint: fmt.Sprintf(
				"http://%s:%d", currentContainerIP, spec.RPCPort(),
			),
			Tests:                []*executor.TestWithSteps{test},
			BlockLogCollector:    params.BlockLogCollector,
			BlockExecutionWarmup: blockExecutionWarmup,
		}

		result, err := r.executor.ExecuteTests(ctx, execOpts)
		if err != nil {
			testLog.WithError(err).Error("Test execution failed")

			continue
		}

		// Aggregate results.
		combined.TotalTests += result.TotalTests
		combined.Passed += result.Passed
		combined.Failed += result.Failed

		if result.StatsReaderType != "" {
			combined.StatsReaderType = result.StatsReaderType
		}

		if result.ContainerDied {
			combined.ContainerDied = true
			combined.TotalDuration = time.Since(startTime)

			(*logCancel)()

			return combined, nil
		}
	}

	combined.TotalDuration = time.Since(startTime)

	(*logCancel)()

	return combined, nil
}

// createFreshDataMount creates a new volume or datadir for a recreated container.
// Returns the mount, a cleanup function (may be nil), and any error.
func (r *runner) createFreshDataMount(
	ctx context.Context,
	params *containerRunParams,
	spec client.Spec,
	iteration int,
) (docker.Mount, func(), error) {
	log := r.log.WithFields(logrus.Fields{
		"instance":  params.Instance.ID,
		"run_id":    params.RunID,
		"iteration": iteration,
	})

	if params.UseDataDir {
		log.Info("Preparing fresh datadir copy")

		provider, err := datadir.NewProvider(log, params.DataDirCfg.Method)
		if err != nil {
			return docker.Mount{}, nil, fmt.Errorf("creating datadir provider: %w", err)
		}

		prepared, err := provider.Prepare(ctx, &datadir.ProviderConfig{
			SourceDir:  params.DataDirCfg.SourceDir,
			InstanceID: fmt.Sprintf("%s-%d", params.Instance.ID, iteration),
			TmpDir:     r.cfg.TmpDataDir,
		})
		if err != nil {
			return docker.Mount{}, nil, fmt.Errorf("preparing datadir: %w", err)
		}

		containerDir := params.DataDirCfg.ContainerDir
		if containerDir == "" {
			containerDir = spec.DataDir()
		}

		cleanup := func() {
			if cleanupErr := prepared.Cleanup(); cleanupErr != nil {
				log.WithError(cleanupErr).Warn("Failed to cleanup recreate datadir")
			}
		}

		return docker.Mount{
			Type:   "bind",
			Source: prepared.MountPath,
			Target: containerDir,
		}, cleanup, nil
	}

	// Docker volume path.
	volumeSuffix := params.Instance.ID
	if params.GenesisGroupHash != "" {
		volumeSuffix = params.Instance.ID + "-" + params.GenesisGroupHash
	}

	volumeName := fmt.Sprintf(
		"benchmarkoor-%s-%s-%d", params.RunID, volumeSuffix, iteration,
	)
	volumeLabels := map[string]string{
		"benchmarkoor.instance":   params.Instance.ID,
		"benchmarkoor.client":     params.Instance.Client,
		"benchmarkoor.run-id":     params.RunID,
		"benchmarkoor.managed-by": "benchmarkoor",
	}

	if err := r.docker.CreateVolume(ctx, volumeName, volumeLabels); err != nil {
		return docker.Mount{}, nil, fmt.Errorf("creating volume: %w", err)
	}

	log.WithField("volume", volumeName).Debug("Created fresh volume")

	cleanup := func() {
		if rmErr := r.docker.RemoveVolume(
			context.Background(), volumeName,
		); rmErr != nil {
			log.WithError(rmErr).Warn("Failed to remove recreate volume")
		}
	}

	return docker.Mount{
		Type:   "volume",
		Source: volumeName,
		Target: spec.DataDir(),
	}, cleanup, nil
}

// runInitForRecreate runs an init container to populate a fresh volume
// during container-recreate strategy.
func (r *runner) runInitForRecreate(
	ctx context.Context,
	params *containerRunParams,
	spec client.Spec,
	mounts []docker.Mount,
	resultsDir string,
	benchmarkoorLog *os.File,
	iteration int,
) error {
	instance := params.Instance

	initName := fmt.Sprintf(
		"benchmarkoor-%s-%s-init-%d", params.RunID, instance.ID, iteration,
	)
	if params.GenesisGroupHash != "" {
		initName = fmt.Sprintf(
			"benchmarkoor-%s-%s-%s-init-%d",
			params.RunID, instance.ID, params.GenesisGroupHash, iteration,
		)
	}

	initSpec := &docker.ContainerSpec{
		Name:        initName,
		Image:       params.ImageName,
		Command:     spec.InitCommand(),
		Mounts:      mounts,
		NetworkName: r.cfg.DockerNetwork,
		Labels: map[string]string{
			"benchmarkoor.instance":   instance.ID,
			"benchmarkoor.client":     instance.Client,
			"benchmarkoor.run-id":     params.RunID,
			"benchmarkoor.type":       "init",
			"benchmarkoor.managed-by": "benchmarkoor",
		},
	}

	initLogFile := filepath.Join(resultsDir, "container.log")

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
		pfxFn := clientLogPrefix(instance.ID + "-init")
		stdoutPW := &prefixedWriter{prefixFn: pfxFn, writer: os.Stdout}
		logPW := &prefixedWriter{prefixFn: pfxFn, writer: benchmarkoorLog}
		initStdout = io.MultiWriter(initFile, stdoutPW, logPW)
		initStderr = io.MultiWriter(initFile, stdoutPW, logPW)
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
// The log file should be opened in append mode before calling this function.
// If blockLogCollector is provided, the collector's writer wraps the file writer
// to intercept and parse JSON payloads from log lines.
func (r *runner) streamLogs(
	ctx context.Context,
	instanceID, containerID string,
	file *os.File,
	benchmarkoorLog io.Writer,
	logInfo *containerLogInfo,
	blockLogCollector blocklog.Collector,
) error {
	// Write start marker with container metadata.
	_, _ = fmt.Fprint(file, formatStartMarker("CONTAINER", logInfo))

	// Base writer is the file, optionally wrapped by block log collector.
	var baseWriter io.Writer = file
	if blockLogCollector != nil {
		baseWriter = blockLogCollector.Writer()
	}

	stdout, stderr := baseWriter, baseWriter

	if r.cfg.ClientLogsToStdout {
		pfxFn := clientLogPrefix(instanceID)
		stdoutPrefixWriter := &prefixedWriter{prefixFn: pfxFn, writer: os.Stdout}
		logFilePrefixWriter := &prefixedWriter{prefixFn: pfxFn, writer: benchmarkoorLog}
		stdout = io.MultiWriter(baseWriter, stdoutPrefixWriter, logFilePrefixWriter)
		stderr = io.MultiWriter(baseWriter, stdoutPrefixWriter, logFilePrefixWriter)
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
// If prefixFn is set, it is called per line to generate the prefix dynamically.
// Otherwise, the static prefix field is used.
type prefixedWriter struct {
	prefix   string
	prefixFn func() string
	writer   io.Writer
	buf      []byte
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

		pfx := w.prefix
		if w.prefixFn != nil {
			pfx = w.prefixFn()
		}

		if _, err := fmt.Fprintf(w.writer, "%s%s", pfx, line); err != nil {
			return n, err
		}
	}

	return n, nil
}

// clientLogPrefix returns a function that generates a consistent log prefix
// for client container logs: "🟣 $TIMESTAMP CLIE | $clientName | ".
func clientLogPrefix(clientName string) func() string {
	return func() string {
		ts := time.Now().UTC().Format(config.LogTimestampFormat)

		return fmt.Sprintf("🟣 %s CLIE | %s | ", ts, clientName)
	}
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

	// Handle blkio config.
	if cfg.BlkioConfig != nil {
		blkioCfg := cfg.BlkioConfig
		resolvedBlkio := &ResolvedBlkioConfig{}

		// Process device_read_bps.
		if len(blkioCfg.DeviceReadBps) > 0 {
			dockerLimits.BlkioDeviceReadBps, resolvedBlkio.DeviceReadBps = convertBlkioDevicesBps(blkioCfg.DeviceReadBps)
		}

		// Process device_write_bps.
		if len(blkioCfg.DeviceWriteBps) > 0 {
			dockerLimits.BlkioDeviceWriteBps, resolvedBlkio.DeviceWriteBps = convertBlkioDevicesBps(blkioCfg.DeviceWriteBps)
		}

		// Process device_read_iops.
		if len(blkioCfg.DeviceReadIOps) > 0 {
			dockerLimits.BlkioDeviceReadIOps, resolvedBlkio.DeviceReadIOps = convertBlkioDevicesIOps(blkioCfg.DeviceReadIOps)
		}

		// Process device_write_iops.
		if len(blkioCfg.DeviceWriteIOps) > 0 {
			dockerLimits.BlkioDeviceWriteIOps, resolvedBlkio.DeviceWriteIOps = convertBlkioDevicesIOps(blkioCfg.DeviceWriteIOps)
		}

		// Only set if we have any blkio config.
		if len(resolvedBlkio.DeviceReadBps) > 0 || len(resolvedBlkio.DeviceWriteBps) > 0 ||
			len(resolvedBlkio.DeviceReadIOps) > 0 || len(resolvedBlkio.DeviceWriteIOps) > 0 {
			resolved.BlkioConfig = resolvedBlkio
		}
	}

	return dockerLimits, resolved, nil
}

// convertBlkioDevicesBps converts config blkio devices with bps rates to docker and resolved formats.
func convertBlkioDevicesBps(devices []config.ThrottleDevice) ([]docker.BlkioThrottleDevice, []ResolvedThrottleDevice) {
	dockerDevices := make([]docker.BlkioThrottleDevice, len(devices))
	resolvedDevices := make([]ResolvedThrottleDevice, len(devices))

	for i, dev := range devices {
		// Parse rate using RAMInBytes (validation already done in config.Validate).
		rate, _ := units.RAMInBytes(dev.Rate)

		dockerDevices[i] = docker.BlkioThrottleDevice{
			Path: dev.Path,
			Rate: uint64(rate),
		}
		resolvedDevices[i] = ResolvedThrottleDevice{
			Path: dev.Path,
			Rate: uint64(rate),
		}
	}

	return dockerDevices, resolvedDevices
}

// convertBlkioDevicesIOps converts config blkio devices with IOPS rates to docker and resolved formats.
func convertBlkioDevicesIOps(devices []config.ThrottleDevice) ([]docker.BlkioThrottleDevice, []ResolvedThrottleDevice) {
	dockerDevices := make([]docker.BlkioThrottleDevice, len(devices))
	resolvedDevices := make([]ResolvedThrottleDevice, len(devices))

	for i, dev := range devices {
		// Parse rate as integer (validation already done in config.Validate).
		rate, _ := strconv.ParseUint(dev.Rate, 10, 64)

		dockerDevices[i] = docker.BlkioThrottleDevice{
			Path: dev.Path,
			Rate: rate,
		}
		resolvedDevices[i] = ResolvedThrottleDevice{
			Path: dev.Path,
			Rate: rate,
		}
	}

	return dockerDevices, resolvedDevices
}

// hasCPUFreqSettings returns true if the resource limits have any CPU frequency settings.
func hasCPUFreqSettings(cfg *config.ResourceLimits) bool {
	if cfg == nil {
		return false
	}
	return cfg.CPUFreq != "" || cfg.CPUTurboBoost != nil || cfg.CPUGovernor != ""
}

// buildCPUFreqConfig builds a cpufreq.Config from resource limits.
func buildCPUFreqConfig(cfg *config.ResourceLimits) *cpufreq.Config {
	if cfg == nil {
		return nil
	}

	cpufreqCfg := &cpufreq.Config{
		Frequency:  cfg.CPUFreq,
		TurboBoost: cfg.CPUTurboBoost,
		Governor:   cfg.CPUGovernor,
	}

	// Default governor to "performance" if frequency is set but governor isn't.
	if cpufreqCfg.Frequency != "" && cpufreqCfg.Governor == "" {
		cpufreqCfg.Governor = "performance"
	}

	return cpufreqCfg
}

// logCPUFreqInfo logs CPU frequency information for the target CPUs.
func logCPUFreqInfo(log logrus.FieldLogger, mgr cpufreq.Manager, targetCPUs []int) {
	infos, err := mgr.GetCPUInfo()
	if err != nil {
		log.WithError(err).Warn("Failed to get CPU frequency info")
		return
	}

	// Filter to target CPUs if specified.
	targetSet := make(map[int]struct{}, len(targetCPUs))
	for _, cpuID := range targetCPUs {
		targetSet[cpuID] = struct{}{}
	}

	for _, info := range infos {
		// Skip CPUs not in target set if targets were specified.
		if len(targetCPUs) > 0 {
			if _, ok := targetSet[info.ID]; !ok {
				continue
			}
		}

		log.WithFields(logrus.Fields{
			"cpu":         info.ID,
			"min_freq":    cpufreq.FormatFrequency(info.MinFreqKHz),
			"max_freq":    cpufreq.FormatFrequency(info.MaxFreqKHz),
			"current":     cpufreq.FormatFrequency(info.CurrentFreqKHz),
			"governor":    info.Governor,
			"scaling_min": cpufreq.FormatFrequency(info.ScalingMinKHz),
			"scaling_max": cpufreq.FormatFrequency(info.ScalingMaxKHz),
		}).Info("CPU frequency info")
	}
}
