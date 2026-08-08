package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/docker/docker/client"
	clientpkg "github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/ethpandaops/benchmarkoor/pkg/jsonrpc"
	"github.com/ethpandaops/benchmarkoor/pkg/stats"
	"github.com/sirupsen/logrus"
)

// Executor runs Engine API tests against a client.
type Executor interface {
	Start(ctx context.Context) error
	Stop() error

	// ExecuteTests runs all tests against the specified endpoint.
	ExecuteTests(ctx context.Context, opts *ExecuteOptions) (*ExecutionResult, error)

	// RunPreRunSteps executes the suite's pre-run steps (if any) against the
	// given endpoint. This is used by checkpoint-restore to run pre-run steps
	// on the live container before checkpointing, so the checkpointed state
	// includes the pre-run effects. Returns the number of steps executed.
	RunPreRunSteps(ctx context.Context, opts *ExecuteOptions) (int, error)

	// GetSuiteHash returns the hash of the test suite.
	GetSuiteHash() string

	// GetTests returns the prepared test list. Returns nil if not yet prepared.
	GetTests() []*TestWithSteps

	// GetSource returns the underlying source, which can be used for genesis resolution.
	GetSource() Source

	// GetProgress returns a snapshot of the current test execution counts.
	// Safe to call concurrently from any goroutine while ExecuteTests runs.
	GetProgress() ProgressSnapshot

	// ResetProgress resets the live progress counters and seeds the total.
	// Call this exactly once at the start of a logical run, BEFORE any
	// ExecuteTests invocation. Strategies that invoke ExecuteTests multiple
	// times per run (e.g. checkpoint-restore, container-recreate) rely on
	// this so the counters accumulate across calls instead of being reset.
	ResetProgress(total int)

	// GetLiveTests returns a defensive copy of the per-test stats map
	// recorded by ExecuteTests as each test completes (success or failure).
	// Entries with Passed=false carry zero gas data. The map is keyed by
	// test name and includes one entry per completed test.
	GetLiveTests() map[string]LiveTestStats
}

// ProgressSnapshot is a point-in-time view of the executor's test progress.
type ProgressSnapshot struct {
	TestsTotal             int
	TestsPassed            int
	TestsFailed            int
	TotalGasUsed           int64 // running sum of test-step gas_used
	TotalGasUsedDurationNs int64 // running sum of test-step gas_used_duration (ns)
}

// LiveTestStats is the per-test record exposed via GetLiveTests for the
// live heatmap. Failed tests are recorded with Passed=false and zero gas
// so the heatmap can render fail tiles in real time.
type LiveTestStats struct {
	Passed            bool
	GasUsed           int64
	GasUsedDurationNs int64
}

// BlockLogCollector is an interface for capturing JSON payloads from client logs.
type BlockLogCollector interface {
	RegisterBlockHash(testName, blockHash string)
}

// ExecuteOptions contains options for test execution.
type ExecuteOptions struct {
	EngineEndpoint                string
	JWT                           string
	ResultsDir                    string
	Filter                        string
	ContainerID                   string                                // Container ID for stats collection.
	DockerClient                  *client.Client                        // Docker client for fallback stats reader.
	DropMemoryCaches              string                                // "tests", "steps", or "" (disabled).
	DropCachesPath                string                                // Path to drop_caches file (default: /proc/sys/vm/drop_caches).
	RollbackStrategy              string                                // "rpc-debug-setHead" or "" (disabled).
	RPCEndpoint                   string                                // RPC endpoint for rollback calls (e.g. http://host:port).
	ClientRPCRollbackSpec         *clientpkg.RPCRollbackSpec            // Client-specific rollback method and param format.
	Tests                         []*TestWithSteps                      // Optional subset of tests to run (nil = run all).
	BlockLogCollector             BlockLogCollector                     // Optional collector for capturing block logs from client.
	RetryNewPayloadsSyncingConfig *config.RetryNewPayloadsSyncingConfig // Retry config for SYNCING responses.
	RetryNewPayloadsFailedConfig  *config.RetryNewPayloadsFailedConfig  // Retry config for any non-SYNCING newPayload failure (RPC error, validation error, INVALID status).
	PostTestRPCCalls              []config.PostTestRPCCall              // Arbitrary RPC calls to execute after the test step.
	OpcodeExtraction              *config.OpcodeExtractionConfig        // When enabled, run debug_traceBlockByNumber per newPayload after the test step and aggregate per-test opcode counts.
	PostTestSleepDuration         time.Duration                         // Sleep duration after each test (0 = disabled).
	FailFast                      bool                                  // If true, return an error from runStepLines on the first failed RPC call.
	PreRunStepSleep               time.Duration                         // Sleep between each RPC call within pre-run step files (0 = disabled).
	SkipUntilBlockNumber          uint64                                // Skip pre-run RPC lines until the first engine_newPayload with blockNumber > this. 0 = no skipping.
	SkipPreRunSteps               bool                                  // Caller already applied the suite's pre-run steps; do not run them again.
}

// ExecutionResult contains the overall execution summary.
type ExecutionResult struct {
	TotalTests        int
	Passed            int
	Failed            int
	TotalDuration     time.Duration
	StatsReaderType   string // "cgroupv2", "dockerstats", or empty if not available
	ContainerDied     bool   // true if container exited during execution
	TerminationReason string // reason for early termination, if any
}

// Config for the executor.
type Config struct {
	Source                          *config.SourceConfig
	Filter                          string
	Metadata                        *config.MetadataConfig     // Suite-level metadata labels
	OpcodeSource                    *config.OpcodeSourceConfig // Optional external opcode metadata
	CacheDir                        string
	ResultsDir                      string
	ResultsOwner                    *fsutil.OwnerConfig // Optional file ownership for results directory
	SystemResourceCollectionEnabled bool                // Enable system resource collection (cgroups/Docker Stats)
	GitHubToken                     string              // Optional GitHub token for API-based artifact downloads
}

// NewExecutor creates a new executor instance.
func NewExecutor(log logrus.FieldLogger, cfg *Config) Executor {
	return &executor{
		log:       log.WithField("component", "executor"),
		cfg:       cfg,
		validator: jsonrpc.DefaultValidator(),
	}
}

type executor struct {
	log         logrus.FieldLogger
	cfg         *Config
	source      Source
	prepared    *PreparedSource
	suiteHash   string
	validator   jsonrpc.Validator
	statsReader stats.Reader
	filter      *filterMatcher // compiled once from cfg.Filter

	// Live progress counters updated during ExecuteTests. Safe to read
	// concurrently via GetProgress().
	progressTotal           atomic.Int64
	progressPassed          atomic.Int64
	progressFailed          atomic.Int64
	progressGasUsed         atomic.Int64 // sum of test-step gas_used for completed tests
	progressGasUsedDuration atomic.Int64 // sum of test-step gas_used_duration (nanoseconds)

	// Per-test live state: full map (one entry per completed test).
	// Snapshot reports include this so the UI can render a live
	// Performance Heatmap that grows as tests complete.
	liveTestsMu sync.RWMutex
	liveTests   map[string]LiveTestStats

	// Per-test aggregated opcode counts captured by extractTestOpcodes
	// when opcode_extraction is enabled. The slice has one entry per
	// engine_newPayload* in the test step, each entry summing per-tx
	// counts across that block's transactions (uppercased).
	opcodesMu   sync.Mutex
	testOpcodes map[string][]map[string]int
}

// Ensure interface compliance.
var _ Executor = (*executor)(nil)

// Start initializes the executor and prepares test sources.
func (e *executor) Start(ctx context.Context) error {
	filter, err := CompileFilter(e.cfg.Filter)
	if err != nil {
		return fmt.Errorf("compiling test filter: %w", err)
	}

	e.filter = filter

	e.source = NewSource(e.log, e.cfg.Source, e.cfg.CacheDir, filter, e.cfg.GitHubToken)
	if e.source == nil {
		return fmt.Errorf("no test source configured")
	}

	// Prepare source early (clone git or verify local dirs, discover tests).
	e.log.Info("Preparing test sources")

	prepared, err := e.source.Prepare(ctx)
	if err != nil {
		return fmt.Errorf("preparing source: %w", err)
	}

	e.prepared = prepared

	e.log.WithFields(logrus.Fields{
		"pre_run_steps": len(prepared.PreRunSteps),
		"tests":         len(prepared.Tests),
	}).Info("Test sources ready")

	// Load external opcode metadata if configured.
	if e.cfg.OpcodeSource != nil && e.cfg.OpcodeSource.File != "" {
		if err := e.loadOpcodes(ctx); err != nil {
			return fmt.Errorf("loading opcodes: %w", err)
		}
	}

	// Create suite output if results directory is configured.
	if e.cfg.ResultsDir != "" {
		if err := e.createSuiteOutput(); err != nil {
			return fmt.Errorf("creating suite output: %w", err)
		}
	}

	return nil
}

// createSuiteOutput computes hash and creates suite directory.
func (e *executor) createSuiteOutput() error {
	// Compute suite hash from file contents.
	hash, err := ComputeSuiteHash(e.prepared)
	if err != nil {
		return fmt.Errorf("computing suite hash: %w", err)
	}

	e.suiteHash = hash

	// Get source information.
	sourceInfo, err := e.source.GetSourceInfo()
	if err != nil {
		return fmt.Errorf("getting source info: %w", err)
	}

	// Build suite info.
	suiteInfo := &SuiteInfo{
		Hash:     hash,
		Source:   sourceInfo,
		Filter:   e.cfg.Filter,
		Metadata: e.cfg.Metadata,
	}

	// Create suite output directory.
	if err := CreateSuiteOutput(e.log, e.cfg.ResultsDir, hash, suiteInfo, e.prepared, e.cfg.ResultsOwner); err != nil {
		return fmt.Errorf("creating suite output: %w", err)
	}

	e.log.WithFields(logrus.Fields{
		"hash":          hash,
		"pre_run_steps": len(e.prepared.PreRunSteps),
		"tests":         len(e.prepared.Tests),
	}).Info("Suite output created")

	return nil
}

// Stop cleans up the executor.
func (e *executor) Stop() error {
	if e.source != nil {
		if err := e.source.Cleanup(); err != nil {
			e.log.WithError(err).Warn("Failed to cleanup source")
		}
	}

	e.log.Debug("Executor stopped")

	return nil
}

// GetSuiteHash returns the hash of the test suite.
func (e *executor) GetSuiteHash() string {
	return e.suiteHash
}

// GetTests returns the prepared test list.
func (e *executor) GetTests() []*TestWithSteps {
	if e.prepared == nil {
		return nil
	}

	return e.prepared.Tests
}

// GetSource returns the underlying source.
func (e *executor) GetSource() Source {
	return e.source
}

// GetProgress returns a snapshot of the executor's live test counters.
// Safe to call concurrently from any goroutine while ExecuteTests runs.
func (e *executor) GetProgress() ProgressSnapshot {
	return ProgressSnapshot{
		TestsTotal:             int(e.progressTotal.Load()),
		TestsPassed:            int(e.progressPassed.Load()),
		TestsFailed:            int(e.progressFailed.Load()),
		TotalGasUsed:           e.progressGasUsed.Load(),
		TotalGasUsedDurationNs: e.progressGasUsedDuration.Load(),
	}
}

// ResetProgress resets the live counters and seeds the total. Call exactly
// once per logical run before any ExecuteTests invocation.
func (e *executor) ResetProgress(total int) {
	e.progressTotal.Store(int64(total))
	e.progressPassed.Store(0)
	e.progressFailed.Store(0)
	e.progressGasUsed.Store(0)
	e.progressGasUsedDuration.Store(0)

	e.liveTestsMu.Lock()
	e.liveTests = make(map[string]LiveTestStats, total)
	e.liveTestsMu.Unlock()
}

// GetLiveTests returns a defensive copy of the per-test stats map. Safe
// to call concurrently from any goroutine. Returns an empty (non-nil)
// map when no tests have completed yet.
func (e *executor) GetLiveTests() map[string]LiveTestStats {
	e.liveTestsMu.RLock()
	defer e.liveTestsMu.RUnlock()

	out := make(map[string]LiveTestStats, len(e.liveTests))
	maps.Copy(out, e.liveTests)

	return out
}

// recordTestCompletion writes a single completed test into the live map.
// Called from the test loop once per iteration regardless of pass/fail.
// Failed tests carry zero gas data so the heatmap can render fail tiles.
func (e *executor) recordTestCompletion(name string, passed bool, gasUsed, gasUsedDurationNs int64) {
	stats := LiveTestStats{
		Passed:            passed,
		GasUsed:           gasUsed,
		GasUsedDurationNs: gasUsedDurationNs,
	}

	e.liveTestsMu.Lock()
	defer e.liveTestsMu.Unlock()

	if e.liveTests == nil {
		e.liveTests = make(map[string]LiveTestStats)
	}

	e.liveTests[name] = stats
}

// RunPreRunSteps executes the suite's pre-run steps against the given endpoint.
// This is used by checkpoint-restore to run pre-run steps on the live container
// before checkpointing. Returns the number of steps executed.
func (e *executor) RunPreRunSteps(ctx context.Context, opts *ExecuteOptions) (int, error) {
	if e.prepared == nil {
		return 0, fmt.Errorf("executor not prepared: call Start first")
	}

	if len(e.prepared.PreRunSteps) == 0 {
		return 0, nil
	}

	e.log.WithField("pre_run_steps", len(e.prepared.PreRunSteps)).Info("Running pre-run steps")

	for _, step := range e.prepared.PreRunSteps {
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("context cancelled during pre-run steps: %w", ctx.Err())
		default:
		}

		log := e.log.WithField("step", step.Name)
		log.Info("Running pre-run step")

		preRunResult := NewTestResult(step.Name)
		if err := e.runStepFile(ctx, opts, step, preRunResult, false, opts.PreRunStepSleep); err != nil {
			// FailFast: surface the error to the caller without writing partial results.
			if opts.FailFast {
				return 0, fmt.Errorf("pre-run step %q failed: %w", step.Name, err)
			}

			log.WithError(err).Warn("Pre-run step failed")

			if ctx.Err() != nil {
				return 0, fmt.Errorf("context cancelled during pre-run step execution: %w", ctx.Err())
			}
		} else {
			if err := WriteStepResults(
				opts.ResultsDir, step.Name, StepTypePreRun, preRunResult, e.cfg.ResultsOwner,
			); err != nil {
				log.WithError(err).Warn("Failed to write pre-run step results")
			}
		}
	}

	e.log.Info("Pre-run steps completed")

	return len(e.prepared.PreRunSteps), nil
}

// ExecuteTests runs all tests against the specified Engine API endpoint.
// If the context is cancelled (e.g., due to container death), execution stops
// but partial results are still written.
func (e *executor) ExecuteTests(ctx context.Context, opts *ExecuteOptions) (*ExecutionResult, error) {
	startTime := time.Now()

	// Create stats reader if container ID is provided and collection is enabled.
	if opts.ContainerID != "" && e.cfg.SystemResourceCollectionEnabled {
		reader, err := stats.NewReader(e.log, opts.DockerClient, opts.ContainerID)
		if err != nil {
			e.log.WithError(err).Warn("Failed to create stats reader, continuing without resource metrics")
		} else {
			e.statsReader = reader
			defer func() {
				if closeErr := reader.Close(); closeErr != nil {
					e.log.WithError(closeErr).Debug("Failed to close stats reader")
				}

				e.statsReader = nil
			}()

			e.log.WithField("type", reader.Type()).Info("Stats reader initialized")
		}
	}

	// Determine which tests to run: opts.Tests overrides prepared.Tests.
	tests := e.prepared.Tests
	if opts.Tests != nil {
		tests = opts.Tests
	}

	e.log.WithFields(logrus.Fields{
		"pre_run_steps": len(e.prepared.PreRunSteps),
		"tests":         len(tests),
	}).Info("Starting test execution")

	// Track if execution was interrupted.
	var interrupted bool
	var interruptReason string

	// Track passed/failed counts directly from the test loop to avoid
	// miscounts when the results directory is shared across calls.
	testsPassed := 0
	testsFailed := 0

	// Determine cache dropping behavior.
	dropBetweenTests := opts.DropMemoryCaches == "tests" || opts.DropMemoryCaches == "steps"
	dropBetweenSteps := opts.DropMemoryCaches == "steps"
	dropCachesPath := opts.DropCachesPath

	// Run pre-run steps first (skip when running a test subset, e.g. multi-genesis,
	// or when the caller already ran them itself — the schelk promote path applies
	// them before stopping the client, so re-running them here would replay a
	// bundle the datadir already contains).
	if len(e.prepared.PreRunSteps) > 0 && opts.Tests == nil && !opts.SkipPreRunSteps {
		e.log.Info("Running pre-run steps")

		for _, step := range e.prepared.PreRunSteps {
			select {
			case <-ctx.Done():
				interrupted = true
				interruptReason = "context cancelled during pre-run steps"

				e.log.Warn("Execution interrupted during pre-run steps")

				goto writeResults
			default:
			}

			log := e.log.WithField("step", step.Name)
			log.Info("Running pre-run step")

			preRunResult := NewTestResult(step.Name)
			if err := e.runStepFile(ctx, opts, step, preRunResult, false, opts.PreRunStepSleep); err != nil {
				log.WithError(err).Warn("Pre-run step failed")

				// Check if the failure was due to context cancellation.
				if ctx.Err() != nil {
					interrupted = true
					interruptReason = "context cancelled during pre-run step execution"

					goto writeResults
				}
			} else {
				if err := WriteStepResults(opts.ResultsDir, step.Name, StepTypePreRun, preRunResult, e.cfg.ResultsOwner); err != nil {
					log.WithError(err).Warn("Failed to write pre-run step results")
				}
			}
		}

		e.log.Info("Pre-run steps completed")
	}

	// Run actual tests with result collection.
	for i, test := range tests {
		select {
		case <-ctx.Done():
			interrupted = true
			interruptReason = "context cancelled between tests"

			e.log.Warn("Execution interrupted between tests")

			goto writeResults
		default:
		}

		// Drop caches between tests (not before first test).
		if dropBetweenTests && i > 0 {
			if err := e.dropMemoryCaches(dropCachesPath); err != nil {
				e.log.WithError(err).Warn("Failed to drop memory caches between tests")
			}
		}

		log := e.log.WithFields(logrus.Fields{
			"test": test.Name,
			"pos":  fmt.Sprintf("%d/%d", i+1, len(tests)),
		})
		log.Info("Running test")

		// Capture block info for rollback before the test starts.
		var rollbackInfo *blockInfo
		if opts.RollbackStrategy == config.RollbackStrategyRPCDebugSetHead && opts.RPCEndpoint != "" {
			if opts.ClientRPCRollbackSpec == nil {
				log.Warn("Rollback enabled but not supported for this client, skipping")
			} else {
				var blockErr error

				rollbackInfo, blockErr = e.getBlockInfo(ctx, opts.RPCEndpoint)
				if blockErr != nil {
					log.WithError(blockErr).Warn("Failed to capture block info for rollback")
				} else {
					log.WithFields(logrus.Fields{
						"block_number": rollbackInfo.HexNumber,
						"block_hash":   rollbackInfo.Hash,
					}).Debug("Captured block info for rollback")
				}
			}
		}

		testPassed := true

		// Per-test gas data captured from the test step's aggregated stats
		// (zero unless the test step ran and produced a result). Hoisted
		// out of the test-step branch so we can also feed the per-test
		// live record at the bottom of the loop.
		var (
			testGasUsed           int64
			testGasUsedDurationNs int64
		)

		// Run setup step if present.
		if test.Setup != nil {
			log.Info("Running setup step")

			setupResult := NewTestResult(test.Name)

			if err := e.runStepFile(ctx, opts, test.Setup, setupResult, false, 0); err != nil {
				log.WithError(err).Error("Setup step failed")
				testPassed = false

				// Check if the failure was due to context cancellation.
				if ctx.Err() != nil {
					interrupted = true
					interruptReason = "context cancelled during setup step"

					goto writeResults
				}
			} else {
				if setupResult.Failed > 0 {
					testPassed = false
				}

				// Write setup results.
				if err := WriteStepResults(opts.ResultsDir, test.Name, StepTypeSetup, setupResult, e.cfg.ResultsOwner); err != nil {
					log.WithError(err).Warn("Failed to write setup results")
				}
			}
		}

		// Drop caches between setup and test.
		if dropBetweenSteps && test.Setup != nil && test.Test != nil {
			if err := e.dropMemoryCaches(dropCachesPath); err != nil {
				e.log.WithError(err).Warn("Failed to drop memory caches before test step")
			}
		}

		// Run test step if present.
		if test.Test != nil {
			log.Info("Running test step")

			testResult := NewTestResult(test.Name)

			if err := e.runStepFile(ctx, opts, test.Test, testResult, true, 0); err != nil {
				log.WithError(err).Error("Test step failed")
				testPassed = false

				// Check if the failure was due to context cancellation.
				if ctx.Err() != nil {
					interrupted = true
					interruptReason = "context cancelled during test step"

					goto writeResults
				}
			} else {
				if testResult.Failed > 0 {
					testPassed = false
				}

				// Write test results.
				if err := WriteStepResults(opts.ResultsDir, test.Name, StepTypeTest, testResult, e.cfg.ResultsOwner); err != nil {
					log.WithError(err).Warn("Failed to write test results")
				}

				// Capture this test's gas usage. Recorded into the running
				// totals (drives live MGas/s) and into the per-test map
				// (drives the live heatmap) at the bottom of the loop.
				stats := testResult.CalculateStats()
				if stats.GasUsedTotal > 0 && stats.GasUsedTimeTotal > 0 {
					testGasUsed = int64(stats.GasUsedTotal) //nolint:gosec // gas values fit comfortably in int64
					testGasUsedDurationNs = stats.GasUsedTimeTotal
				}
			}
		}

		// Execute post-test RPC calls (not timed, does not affect test results).
		if len(opts.PostTestRPCCalls) > 0 && opts.RPCEndpoint != "" {
			e.executePostTestRPCCalls(ctx, opts, test.Name, log)
		}

		// Extract per-newPayload opcode counts via debug_traceBlockByNumber.
		// Best-effort: failures are logged but never abort the test.
		if opts.OpcodeExtraction != nil && opts.OpcodeExtraction.Enabled && opts.RPCEndpoint != "" {
			e.extractTestOpcodes(ctx, opts, test, log)
		}

		// Drop caches between test and cleanup.
		if dropBetweenSteps && test.Test != nil && test.Cleanup != nil {
			if err := e.dropMemoryCaches(dropCachesPath); err != nil {
				e.log.WithError(err).Warn("Failed to drop memory caches before cleanup step")
			}
		}

		// Run cleanup step if present.
		if test.Cleanup != nil {
			log.Info("Running cleanup step")

			cleanupResult := NewTestResult(test.Name)

			if err := e.runStepFile(ctx, opts, test.Cleanup, cleanupResult, false, 0); err != nil {
				log.WithError(err).Error("Cleanup step failed")
				testPassed = false

				// Check if the failure was due to context cancellation.
				if ctx.Err() != nil {
					interrupted = true
					interruptReason = "context cancelled during cleanup step"

					goto writeResults
				}
			} else {
				if cleanupResult.Failed > 0 {
					testPassed = false
				}

				// Write cleanup results.
				if err := WriteStepResults(opts.ResultsDir, test.Name, StepTypeCleanup, cleanupResult, e.cfg.ResultsOwner); err != nil {
					log.WithError(err).Warn("Failed to write cleanup results")
				}
			}
		}

		// Rollback to captured block after test completes.
		if rollbackInfo != nil && opts.ClientRPCRollbackSpec != nil && opts.RPCEndpoint != "" {
			log.WithFields(logrus.Fields{
				"block_number": rollbackInfo.HexNumber,
				"rpc_method":   opts.ClientRPCRollbackSpec.RPCMethod,
			}).Info("Rolling back chain state")

			if rbErr := e.rollback(ctx, opts.RPCEndpoint, opts.ClientRPCRollbackSpec, rollbackInfo); rbErr != nil {
				log.WithError(rbErr).Warn("Failed to rollback chain state")
			} else {
				// Verify the rollback succeeded.
				if current, verifyErr := e.getBlockInfo(ctx, opts.RPCEndpoint); verifyErr != nil {
					log.WithError(verifyErr).Warn("Failed to verify rollback block number")
				} else if current.HexNumber != rollbackInfo.HexNumber {
					log.WithFields(logrus.Fields{
						"expected": rollbackInfo.HexNumber,
						"actual":   current.HexNumber,
					}).Warn("Block number mismatch after rollback")
				} else {
					log.WithField("block_number", rollbackInfo.HexNumber).Info(
						"Rollback verified successfully",
					)
				}
			}
		}

		if opts.PostTestSleepDuration > 0 {
			log.WithField("duration", opts.PostTestSleepDuration).Info("Sleeping after test")
			time.Sleep(opts.PostTestSleepDuration)
		}

		if testPassed {
			testsPassed++
			e.progressPassed.Add(1)
			log.Info("Test completed successfully")
		} else {
			testsFailed++
			e.progressFailed.Add(1)
			log.Warn("Test completed with failures")
		}

		// Update live aggregate gas counters and the per-test/recent
		// records together. testGasUsed/testGasUsedDurationNs stay zero
		// for failed tests or tests with no test step — that's fine, the
		// heatmap renders such entries as fail / no-data tiles.
		if testGasUsedDurationNs > 0 {
			e.progressGasUsed.Add(testGasUsed)
			e.progressGasUsedDuration.Add(testGasUsedDurationNs)
		}

		e.recordTestCompletion(test.Name, testPassed, testGasUsed, testGasUsedDurationNs)
	}

writeResults:
	// Build execution result.
	result := &ExecutionResult{
		TotalTests:        len(tests),
		TotalDuration:     time.Since(startTime),
		ContainerDied:     interrupted,
		TerminationReason: interruptReason,
	}

	// Set stats reader type if available.
	if e.statsReader != nil {
		switch e.statsReader.Type() {
		case "cgroup":
			result.StatsReaderType = "cgroupv2"
		case "docker":
			result.StatsReaderType = "dockerstats"
		default:
			result.StatsReaderType = e.statsReader.Type()
		}
	}

	// Use loop-tracked counts (not GenerateRunResult) to avoid miscounting
	// when the results directory is shared across multiple executor calls.
	result.Passed = testsPassed
	result.Failed = testsFailed

	// Write the run result file.
	runResult, err := GenerateRunResult(opts.ResultsDir)
	if err != nil {
		e.log.WithError(err).Warn("Failed to generate run result")
	} else {
		if err := WriteRunResult(opts.ResultsDir, runResult, e.cfg.ResultsOwner); err != nil {
			e.log.WithError(err).Warn("Failed to write run result")
		} else {
			e.log.WithFields(logrus.Fields{
				"tests_count": len(runResult.Tests),
				"interrupted": interrupted,
			}).Info("Run result written")
		}
	}

	if interrupted {
		e.log.WithField("reason", interruptReason).Warn("Test execution was interrupted")
	}

	// Persist aggregated opcode counts (when extraction was enabled).
	// This rewrites the file on every ExecuteTests call so multi-call
	// strategies (container-recreate, checkpoint-restore) accumulate
	// across calls without losing earlier tests.
	e.opcodesMu.Lock()
	opcodesSnapshot := make(map[string][]map[string]int, len(e.testOpcodes))
	for k, v := range e.testOpcodes {
		opcodesSnapshot[k] = v
	}
	e.opcodesMu.Unlock()

	if len(opcodesSnapshot) > 0 {
		if err := WriteTestOpcodes(opts.ResultsDir, opcodesSnapshot, e.cfg.ResultsOwner); err != nil {
			e.log.WithError(err).Warn("Failed to write test-opcodes.json")
		} else {
			e.log.WithField("tests", len(opcodesSnapshot)).Info("Wrote test-opcodes.json")
		}
	}

	return result, nil
}

// runStepFile executes a single step file or provider.
// If captureBlockLogs is true, blockHashes from engine_newPayload calls are registered for log matching.
// betweenLineSleep, when > 0, sleeps for that duration between each RPC call.
func (e *executor) runStepFile(
	ctx context.Context,
	opts *ExecuteOptions,
	step *StepFile,
	result *TestResult,
	captureBlockLogs bool,
	betweenLineSleep time.Duration,
) error {
	// Use provider if available, otherwise read from file.
	if step.Provider != nil {
		return e.runStepLines(ctx, opts, step.Name, step.Provider.Lines(), result, captureBlockLogs, betweenLineSleep)
	}

	return e.runStepFromFile(ctx, opts, step, result, captureBlockLogs, betweenLineSleep)
}

// runStepFromFile reads and executes lines from a file.
func (e *executor) runStepFromFile(
	ctx context.Context,
	opts *ExecuteOptions,
	step *StepFile,
	result *TestResult,
	captureBlockLogs bool,
	betweenLineSleep time.Duration,
) error {
	file, err := os.Open(step.Path)
	if err != nil {
		return fmt.Errorf("opening step file: %w", err)
	}

	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)

	var lines []string

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				lines = append(lines, trimmed)
			}
		}

		if err != nil {
			if err == io.EOF {
				break
			}

			return fmt.Errorf("reading step file: %w", err)
		}
	}

	return e.runStepLines(ctx, opts, step.Name, lines, result, captureBlockLogs, betweenLineSleep)
}

// runStepLines executes JSON-RPC lines.
// If captureBlockLogs is true, blockHashes from engine_newPayload calls are registered for log matching.
// betweenLineSleep, when > 0, sleeps for that duration between each RPC call (after the call completes,
// before the next one starts). Skipped after the final call. Cancellable via ctx.
func (e *executor) runStepLines(
	ctx context.Context,
	opts *ExecuteOptions,
	stepName string,
	lines []string,
	result *TestResult,
	captureBlockLogs bool,
	betweenLineSleep time.Duration,
) error {
	stepStart := time.Now()

	// A client that answers with a JSON-RPC error is alive and the step carries
	// on. One that cannot be reached at all may be gone — and a dead endpoint
	// answers nothing, so every remaining line burns its full dial timeout. Left
	// unchecked that turns a dead container into a run that grinds on for days
	// (observed: a stopped client with 1547 payloads left reported eta=128h)
	// instead of failing in seconds. Count consecutive transport failures and
	// give up once it is clear nobody is listening.
	consecutiveUnreachable := 0

	// skipping is true when we're dropping already-applied lines at the
	// start of the file (resume scenario). Cleared once we encounter the
	// first engine_newPayload whose blockNumber > SkipUntilBlockNumber.
	skipping := opts.SkipUntilBlockNumber > 0
	skippedCount := 0

	for lineNum, line := range lines {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Parse JSON to extract method name.
		method, err := extractMethod(line)
		if err != nil {
			e.log.WithFields(logrus.Fields{
				"line": lineNum + 1,
				"step": stepName,
			}).WithError(err).Warn("Failed to parse JSON-RPC payload")

			if result != nil {
				result.AddResult("unknown", line, "", 0, false, nil)
			}

			if opts.FailFast {
				return fmt.Errorf(
					"step %q line %d: failed to parse JSON-RPC payload: %w",
					stepName, lineNum+1, err,
				)
			}

			continue
		}

		// Skip already-applied lines if requested. Stay in skipping mode
		// until we see a newPayload past the target; everything before
		// (other newPayloads, FCUs, etc.) is for a block already on
		// disk and would just be redundant work.
		if skipping {
			drop := true

			if strings.HasPrefix(method, "engine_newPayload") {
				if bn, ok := extractBlockNumber(line); ok && bn > opts.SkipUntilBlockNumber {
					skipping = false
					drop = false

					e.log.WithFields(logrus.Fields{
						"step":             stepName,
						"resumed_at_line":  lineNum + 1,
						"resumed_at_block": bn,
						"skipped_lines":    skippedCount,
						"skip_until_block": opts.SkipUntilBlockNumber,
					}).Info("Resuming pre-run replay")
				}
			}

			if drop {
				skippedCount++

				continue
			}
		}

		// Register blockHash BEFORE the RPC call for engine_newPayload methods.
		if captureBlockLogs && strings.HasPrefix(method, "engine_newPayload") &&
			opts.BlockLogCollector != nil && result != nil {
			if blockHash, hashErr := extractBlockHash(line); hashErr == nil {
				opts.BlockLogCollector.RegisterBlockHash(result.TestFile, blockHash)
			}
		}

		// Execute RPC call.
		response, duration, fullDuration, resourceDelta, err := e.executeRPC(ctx, opts.EngineEndpoint, opts.JWT, line)
		succeeded := err == nil

		e.log.WithFields(logrus.Fields{
			"step":          stepName,
			"pos":           fmt.Sprintf("%d/%d", lineNum+1, len(lines)),
			"progress":      fmt.Sprintf("%.1f%%", float64(lineNum+1)*100/float64(len(lines))),
			"eta":           estimateETA(stepStart, lineNum+1, len(lines)),
			"method":        method,
			"duration":      time.Duration(duration),
			"full_duration": time.Duration(fullDuration),
			"overhead":      time.Duration(fullDuration - duration),
		}).Info("RPC call completed")

		if err != nil {
			e.log.WithFields(logrus.Fields{
				"line":   lineNum + 1,
				"method": method,
				"step":   stepName,
			}).WithError(err).Warn("RPC call failed")
		}

		if isTransportError(err) {
			consecutiveUnreachable++

			if consecutiveUnreachable >= unreachableClientThreshold {
				return fmt.Errorf(
					"client at %s unreachable for %d consecutive calls (last: %w) — "+
						"it has most likely exited; abandoning %q at line %d of %d",
					opts.EngineEndpoint, consecutiveUnreachable, err,
					stepName, lineNum+1, len(lines),
				)
			}
		} else {
			consecutiveUnreachable = 0
		}

		// Validate response AFTER timing, BEFORE storing result. Track the
		// validation error so the retry decision below can distinguish
		// SYNCING from other failure modes.
		var validationErr error

		if succeeded && e.validator != nil && response != "" {
			if resp, parseErr := jsonrpc.Parse(response); parseErr != nil {
				e.log.WithFields(logrus.Fields{
					"line":   lineNum + 1,
					"method": method,
					"step":   stepName,
				}).WithError(parseErr).Warn("Failed to parse JSON-RPC response")

				succeeded = false
			} else if validationErr = e.validator.Validate(method, resp); validationErr != nil {
				succeeded = false
			}
		}

		// Decide retry policy when a newPayload call failed. SYNCING errors
		// take the SYNCING retry config when enabled; everything else
		// (RPC/network error, parse error, INVALID/INVALID_BLOCK_HASH,
		// JSON-RPC error) takes the failed-state retry config when enabled.
		// Non-newPayload methods are not retried.
		if !succeeded && strings.HasPrefix(method, "engine_newPayload") {
			isSyncing := validationErr != nil && jsonrpc.IsSyncingError(validationErr)

			switch {
			case isSyncing && opts.RetryNewPayloadsSyncingConfig != nil &&
				opts.RetryNewPayloadsSyncingConfig.Enabled:
				retrySucceeded, retryResponse, retryDuration := e.retryNewPayloadSyncing(
					ctx, opts, line, method, stepName, lineNum,
				)
				if retrySucceeded {
					succeeded = true
					response = retryResponse
					duration = retryDuration
				}
			case opts.RetryNewPayloadsFailedConfig != nil &&
				opts.RetryNewPayloadsFailedConfig.Enabled:
				retrySucceeded, retryResponse, retryDuration := e.retryNewPayloadFailed(
					ctx, opts, line, method, stepName, lineNum,
				)
				if retrySucceeded {
					succeeded = true
					response = retryResponse
					duration = retryDuration
				}
			default:
				if validationErr != nil {
					e.log.WithFields(logrus.Fields{
						"line":   lineNum + 1,
						"method": method,
						"step":   stepName,
					}).WithError(validationErr).Warn("Response validation failed")
				}
			}
		} else if !succeeded && validationErr != nil {
			// Non-newPayload validation failure: log without retry.
			e.log.WithFields(logrus.Fields{
				"line":   lineNum + 1,
				"method": method,
				"step":   stepName,
			}).WithError(validationErr).Warn("Response validation failed")
		}

		if result != nil {
			result.AddResult(method, line, response, duration, succeeded, resourceDelta)
		}

		if !succeeded && opts.FailFast {
			return fmt.Errorf(
				"step %q line %d: RPC call %s failed",
				stepName, lineNum+1, method,
			)
		}

		// Pace the replay if requested. Skip the wait after the final call.
		if betweenLineSleep > 0 && lineNum < len(lines)-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(betweenLineSleep):
			}
		}
	}

	return nil
}

// estimateETA returns the projected remaining duration to finish a step,
// based on the average time per call so far. Returns 0 when there is no
// data yet (first call) or no work remains.
func estimateETA(start time.Time, completed, total int) time.Duration {
	if completed <= 0 || completed >= total {
		return 0
	}

	avg := time.Since(start) / time.Duration(completed)

	return (avg * time.Duration(total-completed)).Round(time.Second)
}

// retryNewPayloadFailed retries an engine_newPayload call after any kind of
// failure (RPC/network error, parse error, validation error including
// SYNCING). Each attempt counts toward MaxRetries regardless of the failure
// mode. Returns whether one of the retries succeeded along with that
// attempt's response and duration; on exhaustion it returns false.
func (e *executor) retryNewPayloadFailed(
	ctx context.Context,
	opts *ExecuteOptions,
	payload, method, stepName string,
	lineNum int,
) (succeeded bool, response string, duration int64) {
	cfg := opts.RetryNewPayloadsFailedConfig
	backoff, _ := time.ParseDuration(cfg.Backoff) // Already validated in config

	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		e.log.WithFields(logrus.Fields{
			"line":        lineNum + 1,
			"method":      method,
			"step":        stepName,
			"attempt":     attempt,
			"max_retries": cfg.MaxRetries,
			"backoff":     backoff,
		}).Info("Retrying newPayload after failure")

		select {
		case <-ctx.Done():
			return false, "", 0
		case <-time.After(backoff):
		}

		retryResponse, retryDuration, _, _, rpcErr := e.executeRPC(ctx, opts.EngineEndpoint, opts.JWT, payload)
		if rpcErr != nil {
			e.log.WithFields(logrus.Fields{
				"line":    lineNum + 1,
				"method":  method,
				"step":    stepName,
				"attempt": attempt,
			}).WithError(rpcErr).Warn("Retry RPC call failed")

			continue
		}

		resp, parseErr := jsonrpc.Parse(retryResponse)
		if parseErr != nil {
			e.log.WithFields(logrus.Fields{
				"line":    lineNum + 1,
				"method":  method,
				"step":    stepName,
				"attempt": attempt,
			}).WithError(parseErr).Warn("Failed to parse retry response")

			continue
		}

		if validationErr := e.validator.Validate(method, resp); validationErr != nil {
			e.log.WithFields(logrus.Fields{
				"line":    lineNum + 1,
				"method":  method,
				"step":    stepName,
				"attempt": attempt,
			}).WithError(validationErr).Debug("Retry validation failed, will retry")

			continue
		}

		e.log.WithFields(logrus.Fields{
			"line":    lineNum + 1,
			"method":  method,
			"step":    stepName,
			"attempt": attempt,
		}).Info("Retry succeeded")

		return true, retryResponse, retryDuration
	}

	e.log.WithFields(logrus.Fields{
		"line":        lineNum + 1,
		"method":      method,
		"step":        stepName,
		"max_retries": cfg.MaxRetries,
	}).Warn("Max retries exceeded for failed newPayload")

	return false, "", 0
}

// retryNewPayloadSyncing retries an engine_newPayload call when it returns SYNCING status.
// Returns whether the retry succeeded, the response, and the duration.
func (e *executor) retryNewPayloadSyncing(
	ctx context.Context,
	opts *ExecuteOptions,
	payload, method, stepName string,
	lineNum int,
) (succeeded bool, response string, duration int64) {
	cfg := opts.RetryNewPayloadsSyncingConfig
	backoff, _ := time.ParseDuration(cfg.Backoff) // Already validated in config

	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		e.log.WithFields(logrus.Fields{
			"line":        lineNum + 1,
			"method":      method,
			"step":        stepName,
			"attempt":     attempt,
			"max_retries": cfg.MaxRetries,
			"backoff":     backoff,
		}).Info("Retrying newPayload after SYNCING status")

		// Wait for backoff duration.
		select {
		case <-ctx.Done():
			return false, "", 0
		case <-time.After(backoff):
		}

		// Re-execute RPC call.
		retryResponse, retryDuration, _, _, err := e.executeRPC(ctx, opts.EngineEndpoint, opts.JWT, payload)
		if err != nil {
			e.log.WithFields(logrus.Fields{
				"line":    lineNum + 1,
				"method":  method,
				"step":    stepName,
				"attempt": attempt,
			}).WithError(err).Warn("Retry RPC call failed")

			continue
		}

		// Validate the retry response.
		resp, parseErr := jsonrpc.Parse(retryResponse)
		if parseErr != nil {
			e.log.WithFields(logrus.Fields{
				"line":    lineNum + 1,
				"method":  method,
				"step":    stepName,
				"attempt": attempt,
			}).WithError(parseErr).Warn("Failed to parse retry response")

			continue
		}

		validationErr := e.validator.Validate(method, resp)
		if validationErr == nil {
			e.log.WithFields(logrus.Fields{
				"line":    lineNum + 1,
				"method":  method,
				"step":    stepName,
				"attempt": attempt,
			}).Info("Retry succeeded")

			return true, retryResponse, retryDuration
		}

		// If still SYNCING, continue retrying.
		if jsonrpc.IsSyncingError(validationErr) {
			e.log.WithFields(logrus.Fields{
				"line":    lineNum + 1,
				"method":  method,
				"step":    stepName,
				"attempt": attempt,
			}).Debug("Still SYNCING, will retry")

			continue
		}

		// Non-SYNCING error, stop retrying.
		e.log.WithFields(logrus.Fields{
			"line":    lineNum + 1,
			"method":  method,
			"step":    stepName,
			"attempt": attempt,
		}).WithError(validationErr).Warn("Retry validation failed with non-SYNCING error")

		return false, retryResponse, retryDuration
	}

	e.log.WithFields(logrus.Fields{
		"line":        lineNum + 1,
		"method":      method,
		"step":        stepName,
		"max_retries": cfg.MaxRetries,
	}).Warn("Max retries exceeded for SYNCING status")

	return false, "", 0
}

// executeRPC executes a single JSON-RPC call against the Engine API.
// Returns the response body, duration (server time), full duration (total round-trip),
// resource delta, and error.
func (e *executor) executeRPC(
	ctx context.Context,
	endpoint, jwt, payload string,
) (string, int64, int64, *ResourceDelta, error) {
	token, err := GenerateJWTToken(jwt)
	if err != nil {
		return "", 0, 0, nil, fmt.Errorf("generating JWT: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(payload))
	if err != nil {
		return "", 0, 0, nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	// Set up httptrace to measure server time (request written → body fully read).
	var wroteRequest time.Time

	trace := &httptrace.ClientTrace{
		WroteRequest: func(_ httptrace.WroteRequestInfo) {
			wroteRequest = time.Now()
		},
	}

	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	// Read stats BEFORE the request (if reader available).
	var beforeStats *stats.Stats
	if e.statsReader != nil {
		beforeStats, _ = e.statsReader.ReadStats()
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		fullDuration := time.Since(start).Nanoseconds()

		// Collect the delta after timing is captured (see collectResourceDelta).
		delta := e.collectResourceDelta(beforeStats)

		return "", 0, fullDuration, delta, fmt.Errorf("executing request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	// Read full body to measure time-to-last-byte.
	body, err := io.ReadAll(resp.Body)
	bodyReadComplete := time.Now()
	fullDuration := time.Since(start).Nanoseconds()

	// Calculate server time (duration from request written to body fully read).
	var duration int64
	if !wroteRequest.IsZero() {
		duration = bodyReadComplete.Sub(wroteRequest).Nanoseconds()
	}

	// Read the AFTER stats only now that the timing window is closed. The stats
	// backend can block — the Docker Stats API on macOS takes ~1-2s per read —
	// and any time spent there must NOT count toward the measured RPC duration,
	// which feeds MGas/s. Reading it earlier (between Do() and the body read)
	// inflated every newPayload to ~2s and crushed MGas/s.
	delta := e.collectResourceDelta(beforeStats)

	if err != nil {
		return "", duration, fullDuration, delta, fmt.Errorf("reading response: %w", err)
	}

	return strings.TrimSpace(string(body)), duration, fullDuration, delta, nil
}

// collectResourceDelta reads the post-request stats snapshot and diffs it
// against before, returning nil when stats collection is disabled/unavailable.
//
// It MUST be called OUTSIDE the request-timing window: the stats backend can
// block (notably the Docker Stats API on macOS), and that latency must never be
// attributed to the RPC under measurement.
func (e *executor) collectResourceDelta(before *stats.Stats) *ResourceDelta {
	if e.statsReader == nil || before == nil {
		return nil
	}

	afterStats, err := e.statsReader.ReadStats()
	if err != nil {
		return nil
	}

	statsDelta := stats.ComputeDelta(before, afterStats)
	if statsDelta == nil {
		return nil
	}

	return &ResourceDelta{
		MemoryDelta:    statsDelta.MemoryDelta,
		MemoryAbsBytes: afterStats.Memory,
		CPUDeltaUsec:   statsDelta.CPUDeltaUsec,
		DiskReadBytes:  statsDelta.DiskReadBytes,
		DiskWriteBytes: statsDelta.DiskWriteBytes,
		DiskReadOps:    statsDelta.DiskReadOps,
		DiskWriteOps:   statsDelta.DiskWriteOps,
	}
}

// rpcRequest is used to parse the method from a JSON-RPC request.
type rpcRequest struct {
	Method string `json:"method"`
}

// extractMethod extracts the method name from a JSON-RPC payload.
func extractMethod(payload string) (string, error) {
	var req rpcRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return "", fmt.Errorf("parsing JSON-RPC request: %w", err)
	}

	if req.Method == "" {
		return "", fmt.Errorf("missing method in JSON-RPC request")
	}

	return req.Method, nil
}

// dropMemoryCaches syncs filesystem and drops Linux memory caches.
func (e *executor) dropMemoryCaches(path string) error {
	// Sync to flush pending writes to disk.
	if err := exec.Command("sync").Run(); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	// Drop all caches (3 = pagecache + dentries + inodes).
	if err := os.WriteFile(path, []byte("3"), 0); err != nil {
		return fmt.Errorf("drop_caches: %w", err)
	}

	e.log.Debug("Dropped memory caches")

	return nil
}

// blockInfo holds the block number (hex) and hash for rollback purposes.
type blockInfo struct {
	HexNumber string // e.g. "0x5"
	Hash      string // e.g. "0xabc..."
}

// getBlockInfo calls eth_getBlockByNumber("latest", false) on the RPC endpoint
// and returns the block number (hex) and hash.
func (e *executor) getBlockInfo(ctx context.Context, rpcEndpoint string) (*blockInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	payload := `{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":1}`

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, rpcEndpoint, strings.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var rpcResp struct {
		Result struct {
			Number string `json:"number"`
			Hash   string `json:"hash"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if rpcResp.Result.Number == "" {
		return nil, fmt.Errorf("empty block number in response")
	}

	return &blockInfo{
		HexNumber: rpcResp.Result.Number,
		Hash:      rpcResp.Result.Hash,
	}, nil
}

// rollback calls the client-specific rollback RPC method to revert chain state.
func (e *executor) rollback(
	ctx context.Context,
	rpcEndpoint string,
	spec *clientpkg.RPCRollbackSpec,
	info *blockInfo,
) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Build the params portion based on the rollback method type.
	var payload string

	switch spec.Method {
	case clientpkg.RollbackMethodSetHeadHex:
		// Param is a quoted hex string: "0x5"
		payload = fmt.Sprintf(
			`{"jsonrpc":"2.0","method":%q,"params":[%q],"id":1}`,
			spec.RPCMethod, info.HexNumber,
		)
	case clientpkg.RollbackMethodSetHeadInt:
		// Param is a raw integer: 5
		blockNum, parseErr := strconv.ParseUint(
			strings.TrimPrefix(info.HexNumber, "0x"), 16, 64,
		)
		if parseErr != nil {
			return fmt.Errorf("parsing block number %q: %w", info.HexNumber, parseErr)
		}

		payload = fmt.Sprintf(
			`{"jsonrpc":"2.0","method":%q,"params":[%d],"id":1}`,
			spec.RPCMethod, blockNum,
		)
	case clientpkg.RollbackMethodResetHeadHash:
		// Param is a block hash string: "0xabc..."
		if info.Hash == "" {
			return fmt.Errorf("block hash required for %s but not available", spec.RPCMethod)
		}

		payload = fmt.Sprintf(
			`{"jsonrpc":"2.0","method":%q,"params":[%q],"id":1}`,
			spec.RPCMethod, info.Hash,
		)
	default:
		return fmt.Errorf("unsupported rollback method: %s", spec.Method)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, rpcEndpoint, strings.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	// Check for JSON-RPC error.
	var rpcResp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("%s error %d: %s",
			spec.RPCMethod, rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return nil
}

// PostTestTemplateData contains template variables available in post-test RPC call params.
type PostTestTemplateData struct {
	BlockHash      string // e.g. "0xabc..."
	BlockNumber    string // Decimal string, e.g. "1234"
	BlockNumberHex string // Hex with 0x prefix, e.g. "0x4d2"
}

// executePostTestRPCCalls runs configured post-test RPC calls after the test step.
// These calls are not timed and do not affect test results.
func (e *executor) executePostTestRPCCalls(
	ctx context.Context,
	opts *ExecuteOptions,
	testName string,
	log logrus.FieldLogger,
) {
	// Get latest block info for template variables.
	info, err := e.getBlockInfo(ctx, opts.RPCEndpoint)
	if err != nil {
		log.WithError(err).Warn("Failed to get block info for post-test RPC calls, skipping")

		return
	}

	// Convert hex block number to decimal.
	blockNum, err := strconv.ParseUint(
		strings.TrimPrefix(info.HexNumber, "0x"), 16, 64,
	)
	if err != nil {
		log.WithError(err).Warn("Failed to parse block number for post-test RPC calls, skipping")

		return
	}

	templateData := PostTestTemplateData{
		BlockHash:      info.Hash,
		BlockNumber:    strconv.FormatUint(blockNum, 10),
		BlockNumberHex: info.HexNumber,
	}

	for i, call := range opts.PostTestRPCCalls {
		select {
		case <-ctx.Done():
			log.Warn("Context cancelled, skipping remaining post-test RPC calls")

			return
		default:
		}

		callLog := log.WithFields(logrus.Fields{
			"method":     call.Method,
			"call_index": i,
		})

		// Process template variables in params.
		processedParams, tmplErr := processTemplateParams(call.Params, templateData)
		if tmplErr != nil {
			callLog.WithError(tmplErr).Warn("Failed to process template params, skipping call")

			continue
		}

		// Build JSON-RPC payload.
		payload, buildErr := buildJSONRPCPayload(call.Method, processedParams)
		if buildErr != nil {
			callLog.WithError(buildErr).Warn("Failed to build JSON-RPC payload, skipping call")

			continue
		}

		// Execute the RPC call (no JWT, plain HTTP).
		callTimeout := 30 * time.Second
		if call.Timeout != "" {
			if d, err := time.ParseDuration(call.Timeout); err == nil {
				callTimeout = d
			}
		}

		callCtx, cancel := context.WithTimeout(ctx, callTimeout)

		response, execErr := executeSimpleRPC(callCtx, opts.RPCEndpoint, payload)

		cancel()

		if execErr != nil {
			callLog.WithError(execErr).Warn("Post-test RPC call failed")

			continue
		}

		callLog.Info("Post-test RPC call completed")

		// Dump response if configured.
		if call.Dump.Enabled && call.Dump.Filename != "" {
			if dumpErr := e.dumpPostTestResponse(
				opts.ResultsDir, testName, call.Dump.Filename, response,
			); dumpErr != nil {
				callLog.WithError(dumpErr).Warn("Failed to dump post-test RPC response")
			}
		}
	}
}

// processTemplateParams recursively processes Go text/template syntax in param values.
// String values are treated as templates; non-string values pass through unchanged.
func processTemplateParams(params []any, data PostTestTemplateData) ([]any, error) {
	if len(params) == 0 {
		return params, nil
	}

	result := make([]any, len(params))

	for i, param := range params {
		processed, err := processTemplateValue(param, data)
		if err != nil {
			return nil, fmt.Errorf("param[%d]: %w", i, err)
		}

		result[i] = processed
	}

	return result, nil
}

// processTemplateValue processes a single value, recursing into maps and slices.
func processTemplateValue(value any, data PostTestTemplateData) (any, error) {
	switch v := value.(type) {
	case string:
		tmpl, err := template.New("param").Parse(v)
		if err != nil {
			return nil, fmt.Errorf("parsing template %q: %w", v, err)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("executing template %q: %w", v, err)
		}

		return buf.String(), nil

	case map[string]any:
		result := make(map[string]any, len(v))

		for key, val := range v {
			processed, err := processTemplateValue(val, data)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", key, err)
			}

			result[key] = processed
		}

		return result, nil

	case []any:
		result := make([]any, len(v))

		for i, val := range v {
			processed, err := processTemplateValue(val, data)
			if err != nil {
				return nil, fmt.Errorf("index %d: %w", i, err)
			}

			result[i] = processed
		}

		return result, nil

	default:
		return value, nil
	}
}

// buildJSONRPCPayload constructs a JSON-RPC 2.0 request payload.
func buildJSONRPCPayload(method string, params []any) (string, error) {
	request := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("marshaling JSON-RPC request: %w", err)
	}

	return string(data), nil
}

// executeSimpleRPC executes a JSON-RPC call without JWT authentication.
func executeSimpleRPC(ctx context.Context, endpoint, payload string) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint, strings.NewReader(payload),
	)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	return string(body), nil
}

// dumpPostTestResponse writes a post-test RPC response to a file.
// The file is written to {resultsDir}/{testName}/post_test_rpc_calls/{filename}.json.
func (e *executor) dumpPostTestResponse(
	resultsDir, testName, filename, response string,
) error {
	postTestDir := filepath.Join(resultsDir, sanitizeResultPath(testName), "post_test_rpc_calls")
	if err := fsutil.MkdirAll(postTestDir, 0755, e.cfg.ResultsOwner); err != nil {
		return fmt.Errorf("creating post_test_rpc_calls directory: %w", err)
	}

	// Pretty-print the response if it's valid JSON.
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, []byte(response), "", "  "); err == nil {
		response = prettyJSON.String()
	}

	dumpPath := filepath.Join(postTestDir, filename+".json")
	if err := fsutil.WriteFile(dumpPath, []byte(response), 0644, e.cfg.ResultsOwner); err != nil {
		return fmt.Errorf("writing dump file: %w", err)
	}

	return nil
}

// unreachableClientThreshold is how many consecutive transport failures mean the
// client is gone rather than momentarily unhappy. Small on purpose: the runner
// waits for RPC readiness before a step starts and after any restart it performs,
// so a step should never see the endpoint disappear transiently.
const unreachableClientThreshold = 3

// isTransportError reports whether err is the HTTP request failing to complete —
// the endpoint could not be dialled, the connection dropped, or it timed out.
// executeRPC only returns an error in that case (and for request construction);
// a client that answers with a JSON-RPC error produces a response, not an error.
// So this distinguishes "nobody is listening" from "the client said no".
func isTransportError(err error) bool {
	if err == nil {
		return false
	}

	var urlErr *url.Error

	return errors.As(err, &urlErr)
}
