package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

	// GetSuiteHash returns the hash of the test suite.
	GetSuiteHash() string

	// GetTests returns the prepared test list. Returns nil if not yet prepared.
	GetTests() []*TestWithSteps

	// GetSource returns the underlying source, which can be used for genesis resolution.
	GetSource() Source
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
	PostTestRPCCalls              []config.PostTestRPCCall              // Arbitrary RPC calls to execute after the test step.
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
}

// Ensure interface compliance.
var _ Executor = (*executor)(nil)

// Start initializes the executor and prepares test sources.
func (e *executor) Start(ctx context.Context) error {
	e.source = NewSource(e.log, e.cfg.Source, e.cfg.CacheDir, e.cfg.Filter, e.cfg.GitHubToken)
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
		Hash:   hash,
		Source: sourceInfo,
		Filter: e.cfg.Filter,
	}

	// Create suite output directory.
	if err := CreateSuiteOutput(e.cfg.ResultsDir, hash, suiteInfo, e.prepared, e.cfg.ResultsOwner); err != nil {
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

	// Determine cache dropping behavior.
	dropBetweenTests := opts.DropMemoryCaches == "tests" || opts.DropMemoryCaches == "steps"
	dropBetweenSteps := opts.DropMemoryCaches == "steps"
	dropCachesPath := opts.DropCachesPath

	// Run pre-run steps first (skip when running a test subset, e.g. multi-genesis).
	if len(e.prepared.PreRunSteps) > 0 && opts.Tests == nil {
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
			if err := e.runStepFile(ctx, opts, step, preRunResult, false); err != nil {
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

		log := e.log.WithField("test", test.Name)
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

		// Run setup step if present.
		if test.Setup != nil {
			log.Info("Running setup step")

			setupResult := NewTestResult(test.Name)

			if err := e.runStepFile(ctx, opts, test.Setup, setupResult, false); err != nil {
				log.WithError(err).Error("Setup step failed")
				testPassed = false

				// Check if the failure was due to context cancellation.
				if ctx.Err() != nil {
					interrupted = true
					interruptReason = "context cancelled during setup step"

					goto writeResults
				}
			} else {
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

			if err := e.runStepFile(ctx, opts, test.Test, testResult, true); err != nil {
				log.WithError(err).Error("Test step failed")
				testPassed = false

				// Check if the failure was due to context cancellation.
				if ctx.Err() != nil {
					interrupted = true
					interruptReason = "context cancelled during test step"

					goto writeResults
				}
			} else {
				// Write test results.
				if err := WriteStepResults(opts.ResultsDir, test.Name, StepTypeTest, testResult, e.cfg.ResultsOwner); err != nil {
					log.WithError(err).Warn("Failed to write test results")
				}
			}
		}

		// Execute post-test RPC calls (not timed, does not affect test results).
		if len(opts.PostTestRPCCalls) > 0 && opts.RPCEndpoint != "" {
			e.executePostTestRPCCalls(ctx, opts, test.Name, log)
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

			if err := e.runStepFile(ctx, opts, test.Cleanup, cleanupResult, false); err != nil {
				log.WithError(err).Error("Cleanup step failed")
				testPassed = false

				// Check if the failure was due to context cancellation.
				if ctx.Err() != nil {
					interrupted = true
					interruptReason = "context cancelled during cleanup step"

					goto writeResults
				}
			} else {
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

		if testPassed {
			log.Info("Test completed successfully")
		} else {
			log.Warn("Test completed with failures")
		}
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

	// Count passed/failed from whatever results were written.
	// We scan the results directory to count actual test outcomes.
	runResult, err := GenerateRunResult(opts.ResultsDir)
	if err != nil {
		e.log.WithError(err).Warn("Failed to generate run result")
	} else {
		// Count passed/failed tests from the generated result.
		for _, test := range runResult.Tests {
			passed := true
			if test.Steps != nil {
				if test.Steps.Setup != nil && test.Steps.Setup.Aggregated.Failed > 0 {
					passed = false
				}

				if test.Steps.Test != nil && test.Steps.Test.Aggregated.Failed > 0 {
					passed = false
				}

				if test.Steps.Cleanup != nil && test.Steps.Cleanup.Aggregated.Failed > 0 {
					passed = false
				}
			}

			if passed {
				result.Passed++
			} else {
				result.Failed++
			}
		}

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

	return result, nil
}

// runStepFile executes a single step file or provider.
// If captureBlockLogs is true, blockHashes from engine_newPayload calls are registered for log matching.
func (e *executor) runStepFile(
	ctx context.Context,
	opts *ExecuteOptions,
	step *StepFile,
	result *TestResult,
	captureBlockLogs bool,
) error {
	// Use provider if available, otherwise read from file.
	if step.Provider != nil {
		return e.runStepLines(ctx, opts, step.Name, step.Provider.Lines(), result, captureBlockLogs)
	}

	return e.runStepFromFile(ctx, opts, step, result, captureBlockLogs)
}

// runStepFromFile reads and executes lines from a file.
func (e *executor) runStepFromFile(
	ctx context.Context,
	opts *ExecuteOptions,
	step *StepFile,
	result *TestResult,
	captureBlockLogs bool,
) error {
	file, err := os.Open(step.Path)
	if err != nil {
		return fmt.Errorf("opening step file: %w", err)
	}

	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	// Increase buffer size to 50MB to handle large JSON-RPC payloads.
	scanner.Buffer(make([]byte, 64*1024), 50*1024*1024)

	var lines []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading step file: %w", err)
	}

	return e.runStepLines(ctx, opts, step.Name, lines, result, captureBlockLogs)
}

// runStepLines executes JSON-RPC lines.
// If captureBlockLogs is true, blockHashes from engine_newPayload calls are registered for log matching.
func (e *executor) runStepLines(
	ctx context.Context,
	opts *ExecuteOptions,
	stepName string,
	lines []string,
	result *TestResult,
	captureBlockLogs bool,
) error {
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

			continue
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

		// Validate response AFTER timing, BEFORE storing result.
		if succeeded && e.validator != nil && response != "" {
			if resp, parseErr := jsonrpc.Parse(response); parseErr != nil {
				e.log.WithFields(logrus.Fields{
					"line":   lineNum + 1,
					"method": method,
					"step":   stepName,
				}).WithError(parseErr).Warn("Failed to parse JSON-RPC response")

				succeeded = false
			} else if validationErr := e.validator.Validate(method, resp); validationErr != nil {
				// Check if this is a SYNCING error and retry is enabled.
				if jsonrpc.IsSyncingError(validationErr) && opts.RetryNewPayloadsSyncingConfig != nil &&
					opts.RetryNewPayloadsSyncingConfig.Enabled {
					retrySucceeded, retryResponse, retryDuration := e.retryNewPayloadSyncing(
						ctx, opts, line, method, stepName, lineNum,
					)
					if retrySucceeded {
						succeeded = true
						response = retryResponse
						duration = retryDuration
					} else {
						succeeded = false
					}
				} else {
					e.log.WithFields(logrus.Fields{
						"line":   lineNum + 1,
						"method": method,
						"step":   stepName,
					}).WithError(validationErr).Warn("Response validation failed")

					succeeded = false
				}
			}
		}

		if result != nil {
			result.AddResult(method, line, response, duration, succeeded, resourceDelta)
		}
	}

	return nil
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

	// Read stats AFTER the request completes and compute delta.
	// This captures resource usage during server processing, not during body read.
	var delta *ResourceDelta
	if e.statsReader != nil && beforeStats != nil {
		if afterStats, readErr := e.statsReader.ReadStats(); readErr == nil {
			statsDelta := stats.ComputeDelta(beforeStats, afterStats)
			if statsDelta != nil {
				delta = &ResourceDelta{
					MemoryDelta:    statsDelta.MemoryDelta,
					CPUDeltaUsec:   statsDelta.CPUDeltaUsec,
					DiskReadBytes:  statsDelta.DiskReadBytes,
					DiskWriteBytes: statsDelta.DiskWriteBytes,
					DiskReadOps:    statsDelta.DiskReadOps,
					DiskWriteOps:   statsDelta.DiskWriteOps,
				}
			}
		}
	}

	if err != nil {
		fullDuration := time.Since(start).Nanoseconds()

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

	if err != nil {
		return "", duration, fullDuration, delta, fmt.Errorf("reading response: %w", err)
	}

	return strings.TrimSpace(string(body)), duration, fullDuration, delta, nil
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
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

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
	postTestDir := filepath.Join(resultsDir, testName, "post_test_rpc_calls")
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
