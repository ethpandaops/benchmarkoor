package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/blocklog"
	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/cpufreq"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/ethpandaops/benchmarkoor/pkg/livereport"
	"github.com/ethpandaops/benchmarkoor/pkg/upload"
	"github.com/sirupsen/logrus"
)

const (
	// DefaultReadyTimeout is the default timeout for waiting for RPC to be ready.
	DefaultReadyTimeout = 120 * time.Second

	// DefaultHealthCheckInterval is the interval between health checks.
	DefaultHealthCheckInterval = 1 * time.Second

	// logDrainTimeout is the maximum time to wait for log streaming to
	// finish after a container has been stopped.
	logDrainTimeout = 5 * time.Second
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
	ContainerNetwork   string
	JWT                string
	GenesisURLs        map[string]string
	DataDirs           map[string]*config.DataDirConfig
	TmpDataDir         string // Directory for temporary datadir copies (empty = system default)
	TmpCacheDir        string // Directory for temporary cache files (empty = system default)
	ReadyTimeout       time.Duration
	TestFilter         string
	FullConfig         *config.Config // Full config for resolving per-instance settings
}

// TestCounts contains test count statistics for a run.
type TestCounts struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// StartBlock contains block information captured at the start of a run.
type StartBlock struct {
	Number    uint64 `json:"number"`
	Hash      string `json:"hash"`
	StateRoot string `json:"state_root"`
}

// RunConfig contains configuration for a single test run.
type RunConfig struct {
	Timestamp                      int64                  `json:"timestamp"`
	TimestampEnd                   int64                  `json:"timestamp_end,omitempty"`
	SuiteHash                      string                 `json:"suite_hash,omitempty"`
	SystemResourceCollectionMethod string                 `json:"system_resource_collection_method,omitempty"`
	System                         *SystemInfo            `json:"system"`
	Instance                       *ResolvedInstance      `json:"instance"`
	Metadata                       *config.MetadataConfig `json:"metadata,omitempty"`
	StartBlock                     *StartBlock            `json:"start_block,omitempty"`
	TestCounts                     *TestCounts            `json:"test_counts,omitempty"`
	Status                         string                 `json:"status,omitempty"`
	TerminationReason              string                 `json:"termination_reason,omitempty"`
	ContainerExitCode              *int64                 `json:"container_exit_code,omitempty"`
	ContainerOOMKilled             *bool                  `json:"container_oom_killed,omitempty"`
}

// Run status constants.
const (
	RunStatusRunning       = "running"
	RunStatusCompleted     = "completed"
	RunStatusFailed        = "failed"
	RunStatusContainerDied = "container_died"
	RunStatusCancelled     = "cancelled"
	RunStatusTimedOut      = "timeout"
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
	ID                               string                                   `json:"id"`
	Client                           string                                   `json:"client"`
	ContainerRuntime                 string                                   `json:"container_runtime,omitempty"`
	Image                            string                                   `json:"image"`
	ImageSHA256                      string                                   `json:"image_sha256,omitempty"`
	Entrypoint                       []string                                 `json:"entrypoint,omitempty"`
	Command                          []string                                 `json:"command,omitempty"`
	ExtraArgs                        []string                                 `json:"extra_args,omitempty"`
	PullPolicy                       string                                   `json:"pull_policy"`
	Restart                          string                                   `json:"restart,omitempty"`
	Environment                      map[string]string                        `json:"environment,omitempty"`
	Genesis                          string                                   `json:"genesis,omitempty"`
	GenesisGroups                    map[string]string                        `json:"genesis_groups,omitempty"`
	DataDir                          *config.DataDirConfig                    `json:"datadir,omitempty"`
	ClientVersion                    string                                   `json:"client_version,omitempty"`
	RollbackStrategy                 string                                   `json:"rollback_strategy,omitempty"`
	DropMemoryCaches                 string                                   `json:"drop_memory_caches,omitempty"`
	WaitAfterRPCReady                string                                   `json:"wait_after_rpc_ready,omitempty"`
	RunTimeout                       string                                   `json:"run_timeout,omitempty"`
	RetryNewPayloadsSyncingState     *config.RetryNewPayloadsSyncingConfig    `json:"retry_new_payloads_syncing_state,omitempty"`
	ResourceLimits                   *ResolvedResourceLimits                  `json:"resource_limits,omitempty"`
	PostTestRPCCalls                 []config.PostTestRPCCall                 `json:"post_test_rpc_calls,omitempty"`
	PostTestSleepDuration            string                                   `json:"post_test_sleep_duration,omitempty"`
	BootstrapFCU                     *config.BootstrapFCUConfig               `json:"bootstrap_fcu,omitempty"`
	CheckpointRestoreStrategyOptions *config.CheckpointRestoreStrategyOptions `json:"checkpoint_restore_strategy_options,omitempty"`
}

// NewRunner creates a new runner instance.
func NewRunner(
	log *logrus.Logger,
	cfg *Config,
	containerMgr docker.ContainerManager,
	registry client.Registry,
	exec executor.Executor,
	cpufreqMgr cpufreq.Manager,
	uploader upload.Uploader,
	preRunLogBuffer *BufferHook,
) Runner {
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = DefaultReadyTimeout
	}

	return &runner{
		logger:          log,
		log:             log.WithField("component", "runner"),
		cfg:             cfg,
		containerMgr:    containerMgr,
		registry:        registry,
		executor:        exec,
		cpufreqMgr:      cpufreqMgr,
		uploader:        uploader,
		preRunLogBuffer: preRunLogBuffer,
		done:            make(chan struct{}),
	}
}

type runner struct {
	logger          *logrus.Logger     // The actual logger (for hook management)
	log             logrus.FieldLogger // The field logger (for logging with fields)
	cfg             *Config
	containerMgr    docker.ContainerManager
	registry        client.Registry
	executor        executor.Executor
	cpufreqMgr      cpufreq.Manager
	uploader        upload.Uploader
	preRunLogBuffer *BufferHook // Buffered logs from before RunInstance (may be nil).
	done            chan struct{}
	wg              sync.WaitGroup
}

// Ensure interface compliance.
var _ Runner = (*runner)(nil)

// getDockerClient returns the underlying Docker client if the container manager
// is a Docker manager, or nil otherwise (e.g., when using Podman).
func (r *runner) getDockerClient() *dockerclient.Client {
	if dm, ok := r.containerMgr.(docker.Manager); ok {
		return dm.GetClient()
	}

	return nil
}

// Start initializes the runner.
func (r *runner) Start(ctx context.Context) error {
	// Ensure results directory exists.
	if err := fsutil.MkdirAll(r.cfg.ResultsDir, 0755, r.cfg.ResultsOwner); err != nil {
		return fmt.Errorf("creating results directory: %w", err)
	}

	// Ensure container network exists.
	if err := r.containerMgr.EnsureNetwork(ctx, r.cfg.ContainerNetwork); err != nil {
		return fmt.Errorf("ensuring container network: %w", err)
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

// uploadResults uploads run results to remote storage if an uploader is configured.
// Uses a fresh context with a 5-minute timeout so uploads complete even if the
// parent context was cancelled. If suiteHash is non-empty, the suite directory
// is also uploaded.
func (r *runner) uploadResults(runResultsDir, suiteHash string) {
	if r.uploader == nil {
		return
	}

	r.log.WithField("dir", runResultsDir).Info("Uploading results to S3")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := r.uploader.Upload(ctx, runResultsDir); err != nil {
		r.log.WithError(err).Warn("Failed to upload results to S3")
	}

	if suiteHash != "" {
		suiteDir := filepath.Join(r.cfg.ResultsDir, "suites", suiteHash)

		if _, err := os.Stat(suiteDir); err == nil {
			r.log.WithField("suite_hash", suiteHash).Info("Uploading suite directory to S3")

			if err := r.uploader.UploadSuiteDir(ctx, suiteDir); err != nil {
				r.log.WithError(err).Warn("Failed to upload suite directory to S3")
			}
		}
	}
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
	Instance             *config.ClientInstance
	RunID                string
	RunTimestamp         int64
	RunResultsDir        string
	BenchmarkoorLog      *os.File
	LogHook              *fileHook
	GenesisSource        string                    // Path or URL to genesis file.
	Tests                []*executor.TestWithSteps // Optional test subset (nil = all).
	GenesisGroupHash     string                    // Non-empty when running a specific genesis group.
	GenesisGroups        map[string]string         // All genesis hash → path mappings (multi-genesis).
	ImageName            string                    // Resolved image name (pulled once by caller).
	ImageDigest          string                    // Image SHA256 digest (resolved once by caller).
	ContainerSpec        *docker.ContainerSpec     // Saved for container-recreate strategy.
	DataDirCfg           *config.DataDirConfig     // Resolved datadir config (nil if not using datadir).
	UseDataDir           bool                      // Whether a pre-populated datadir is used.
	BlockLogCollector    blocklog.Collector        // Optional collector for capturing block logs.
	AccumulatedTestCount *TestCounts               // Shared across genesis groups for accumulation.
	LiveState            *liveRunState             // Optional: live status state shared with the reporter goroutine.
}

// liveRunState is a tiny mutable snapshot of run state shared between the
// lifecycle code (which writes status transitions) and the live-reporting
// goroutine (which reads them to build snapshots).
type liveRunState struct {
	mu                sync.RWMutex
	status            string
	terminationReason string
	timestampEnd      int64
	// configJSON is the most recent serialized RunConfig. Lifecycle code
	// calls SetConfig every time it writes config.json to disk so the
	// reporter can include the latest content in its next snapshot.
	configJSON []byte
	// onChange is optionally set by the reporter; invoked (non-blocking)
	// whenever SetConfig stores a new config, so the reporter can flush an
	// out-of-band report immediately instead of waiting for its next tick.
	onChange func()
}

func (s *liveRunState) SetStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

func (s *liveRunState) SetTerminal(status, reason string, endTs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.terminationReason = reason
	s.timestampEnd = endTs
}

// SetConfig serializes the given config to JSON and stores it so the next
// live report snapshot carries the updated content. Errors are silently
// dropped — the run is more important than the reporter. When an onChange
// callback is set, it is invoked after the update so the reporter can
// flush an immediate out-of-band report.
func (s *liveRunState) SetConfig(cfg any) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return
	}

	s.mu.Lock()
	s.configJSON = b
	cb := s.onChange
	s.mu.Unlock()

	if cb != nil {
		cb()
	}
}

func (s *liveRunState) Snapshot() (status, reason string, endTs int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status, s.terminationReason, s.timestampEnd
}

// SnapshotConfig returns the latest serialized RunConfig. nil if no
// SetConfig call has happened yet.
func (s *liveRunState) SnapshotConfig() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.configJSON
}

// liveSnapshotter returns a function that builds a LiveRunReport from the
// current run state plus the executor's live test counters. Used by the
// reporter goroutine.
func (r *runner) liveSnapshotter(
	instance *config.ClientInstance,
	runID string,
	runTimestamp int64,
	state *liveRunState,
) func() indexstore.LiveRunReport {
	return func() indexstore.LiveRunReport {
		status, reason, endTs := state.Snapshot()

		report := indexstore.LiveRunReport{
			RunID:             runID,
			Timestamp:         runTimestamp,
			TimestampEnd:      endTs,
			Status:            status,
			TerminationReason: reason,
			InstanceID:        instance.ID,
			Client:            instance.Client,
			Image:             instance.Image,
		}

		if r.cfg.FullConfig != nil {
			report.RollbackStrategy = r.cfg.FullConfig.GetRollbackStrategy(instance)
			report.SuiteHash = ""
			if r.executor != nil {
				report.SuiteHash = r.executor.GetSuiteHash()
			}
			if labels := r.cfg.FullConfig.GetMetadataLabels(instance); len(labels) > 0 {
				report.Metadata = labels
			}
		}

		if r.executor != nil {
			progress := r.executor.GetProgress()
			report.TestsTotal = progress.TestsTotal
			report.TestsPassed = progress.TestsPassed
			report.TestsFailed = progress.TestsFailed

			// Before ExecuteTests has started, progress.TestsTotal is 0
			// because the executor hasn't seeded its live counters yet.
			// Fall back to the prepared test list so the UI shows the
			// planned total from the very first report.
			if report.TestsTotal == 0 {
				report.TestsTotal = len(r.executor.GetTests())
			}
		}

		// Attach the latest config.json snapshot if the lifecycle has
		// written one; the UI uses it to render the full configuration
		// panels for in-progress runs.
		if cfg := state.SnapshotConfig(); len(cfg) > 0 {
			report.Config = cfg
		}

		return report
	}
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

	var suiteHash string
	if r.executor != nil {
		suiteHash = r.executor.GetSuiteHash()
	}

	defer r.uploadResults(runResultsDir, suiteHash)

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

	// Flush any pre-instance logs (config validation, cleanup, image pulls,
	// etc.) that were buffered before RunInstance was called.
	if r.preRunLogBuffer != nil {
		if err := r.preRunLogBuffer.FlushTo(benchmarkoorLogFile); err != nil {
			return fmt.Errorf("flushing pre-run logs: %w", err)
		}
	}

	log := r.log.WithFields(logrus.Fields{
		"instance": instance.ID,
		"run_id":   runID,
	})
	log.Info("Starting client instance")

	// Reset the executor's live progress counters and seed the total with
	// the full prepared test count. Strategies that call ExecuteTests once
	// per test (checkpoint-restore, container-recreate) rely on this being
	// done exactly once per RunInstance so counters accumulate.
	if r.executor != nil {
		r.executor.ResetProgress(len(r.executor.GetTests()))
	}

	// Set up live status reporting if enabled. The reporter posts periodic
	// snapshots of the run state to the configured benchmarkoor API. The
	// liveState struct is shared with the lifecycle code, which writes
	// status transitions into it.
	liveState := &liveRunState{status: RunStatusRunning}

	var reporter livereport.Reporter

	if r.cfg.FullConfig != nil &&
		r.cfg.FullConfig.Runner.LiveReporting != nil &&
		r.cfg.FullConfig.Runner.LiveReporting.Enabled {
		lrCfg := r.cfg.FullConfig.Runner.LiveReporting

		// Use the composite run directory name as the run_id so live rows
		// match the canonical Run rows produced by the on-disk indexer.
		// The UI links to runs by this id via the /runs/$runId route.
		compositeRunID := fmt.Sprintf("%d_%s_%s", runTimestamp, runID, instance.ID)

		snap := r.liveSnapshotter(instance, compositeRunID, runTimestamp, liveState)
		reporter = livereport.New(r.logger, lrCfg, snap)
		reporter.Start(ctx)

		// Whenever the lifecycle writes a new config, push a report
		// immediately so the API shows it without waiting for the next
		// periodic tick.
		liveState.mu.Lock()
		liveState.onChange = func() {
			reporter.ReportNow(ctx)
		}
		liveState.mu.Unlock()

		defer reporter.Stop()
	}

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

	if err := r.containerMgr.PullImage(ctx, imageName, instance.PullPolicy); err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	imageDigest, err := r.containerMgr.GetImageDigest(ctx, imageName)
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

				// Shared test counts accumulator across all genesis groups.
				accumulatedTestCounts := &TestCounts{}

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
						Instance:             instance,
						RunID:                runID,
						RunTimestamp:         runTimestamp,
						RunResultsDir:        runResultsDir,
						BenchmarkoorLog:      benchmarkoorLogFile,
						LogHook:              logHook,
						GenesisSource:        groupGenesis,
						Tests:                group.Tests,
						GenesisGroupHash:     group.GenesisHash,
						GenesisGroups:        genesisGroups,
						ImageName:            imageName,
						ImageDigest:          imageDigest,
						AccumulatedTestCount: accumulatedTestCounts,
						LiveState:            liveState,
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
		LiveState:       liveState,
	}

	return r.runContainerLifecycle(
		ctx, params, spec, datadirCfg, useDataDir,
	)
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
