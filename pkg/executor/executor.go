package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
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
}

// ExecuteOptions contains options for test execution.
type ExecuteOptions struct {
	EngineEndpoint string
	JWT            string
	ResultsDir     string
	Filter         string
	ContainerID    string         // Container ID for stats collection.
	DockerClient   *client.Client // Docker client for fallback stats reader.
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
	Source     *config.SourceConfig
	Filter     string
	CacheDir   string
	ResultsDir string
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
	e.source = NewSource(e.log, e.cfg.Source, e.cfg.CacheDir, e.cfg.Filter)
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
	if err := CreateSuiteOutput(e.cfg.ResultsDir, hash, suiteInfo, e.prepared); err != nil {
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

// ExecuteTests runs all tests against the specified Engine API endpoint.
// If the context is cancelled (e.g., due to container death), execution stops
// but partial results are still written.
func (e *executor) ExecuteTests(ctx context.Context, opts *ExecuteOptions) (*ExecutionResult, error) {
	startTime := time.Now()

	// Create stats reader if container ID is provided.
	if opts.ContainerID != "" {
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

	e.log.WithFields(logrus.Fields{
		"pre_run_steps": len(e.prepared.PreRunSteps),
		"tests":         len(e.prepared.Tests),
	}).Info("Starting test execution")

	// Track if execution was interrupted.
	var interrupted bool
	var interruptReason string

	// Run pre-run steps first.
	if len(e.prepared.PreRunSteps) > 0 {
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
			if err := e.runStepFile(ctx, opts, step, preRunResult); err != nil {
				log.WithError(err).Warn("Pre-run step failed")

				// Check if the failure was due to context cancellation.
				if ctx.Err() != nil {
					interrupted = true
					interruptReason = "context cancelled during pre-run step execution"

					goto writeResults
				}
			} else {
				if err := WriteStepResults(opts.ResultsDir, step.Name, StepTypePreRun, preRunResult); err != nil {
					log.WithError(err).Warn("Failed to write pre-run step results")
				}
			}
		}

		e.log.Info("Pre-run steps completed")
	}

	// Run actual tests with result collection.
	for _, test := range e.prepared.Tests {
		select {
		case <-ctx.Done():
			interrupted = true
			interruptReason = "context cancelled between tests"

			e.log.Warn("Execution interrupted between tests")

			goto writeResults
		default:
		}

		log := e.log.WithField("test", test.Name)
		log.Info("Running test")

		testPassed := true

		// Run setup step if present.
		if test.Setup != nil {
			log.Info("Running setup step")

			setupResult := NewTestResult(test.Name)

			if err := e.runStepFile(ctx, opts, test.Setup, setupResult); err != nil {
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
				if err := WriteStepResults(opts.ResultsDir, test.Name, StepTypeSetup, setupResult); err != nil {
					log.WithError(err).Warn("Failed to write setup results")
				}
			}
		}

		// Run test step if present.
		if test.Test != nil {
			log.Info("Running test step")

			testResult := NewTestResult(test.Name)

			if err := e.runStepFile(ctx, opts, test.Test, testResult); err != nil {
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
				if err := WriteStepResults(opts.ResultsDir, test.Name, StepTypeTest, testResult); err != nil {
					log.WithError(err).Warn("Failed to write test results")
				}
			}
		}

		// Run cleanup step if present.
		if test.Cleanup != nil {
			log.Info("Running cleanup step")

			cleanupResult := NewTestResult(test.Name)

			if err := e.runStepFile(ctx, opts, test.Cleanup, cleanupResult); err != nil {
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
				if err := WriteStepResults(opts.ResultsDir, test.Name, StepTypeCleanup, cleanupResult); err != nil {
					log.WithError(err).Warn("Failed to write cleanup results")
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
		TotalTests:        len(e.prepared.Tests),
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

		if err := WriteRunResult(opts.ResultsDir, runResult); err != nil {
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

// runStepFile executes a single step file.
func (e *executor) runStepFile(ctx context.Context, opts *ExecuteOptions, step *StepFile, result *TestResult) error {
	file, err := os.Open(step.Path)
	if err != nil {
		return fmt.Errorf("opening step file: %w", err)
	}

	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	// Increase buffer size to 50MB to handle large JSON-RPC payloads
	scanner.Buffer(make([]byte, 64*1024), 50*1024*1024)
	lineNum := 0

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		lineNum++

		// Parse JSON to extract method name.
		method, err := extractMethod(line)
		if err != nil {
			e.log.WithFields(logrus.Fields{
				"line": lineNum,
				"step": step.Name,
			}).WithError(err).Warn("Failed to parse JSON-RPC payload")

			if result != nil {
				result.AddResult("unknown", line, "", 0, false, nil)
			}

			continue
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
				"line":   lineNum,
				"method": method,
				"step":   step.Name,
			}).WithError(err).Warn("RPC call failed")
		}

		// Validate response AFTER timing, BEFORE storing result.
		if succeeded && e.validator != nil && response != "" {
			if resp, parseErr := jsonrpc.Parse(response); parseErr != nil {
				e.log.WithFields(logrus.Fields{
					"line":   lineNum,
					"method": method,
					"step":   step.Name,
				}).WithError(parseErr).Warn("Failed to parse JSON-RPC response")

				succeeded = false
			} else if validationErr := e.validator.Validate(method, resp); validationErr != nil {
				e.log.WithFields(logrus.Fields{
					"line":   lineNum,
					"method": method,
					"step":   step.Name,
				}).WithError(validationErr).Warn("Response validation failed")

				succeeded = false
			}
		}

		if result != nil {
			result.AddResult(method, line, response, duration, succeeded, resourceDelta)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading step file: %w", err)
	}

	return nil
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

	// Set up httptrace to measure server time (request written → first response byte).
	var wroteRequest, gotFirstByte time.Time

	trace := &httptrace.ClientTrace{
		WroteRequest: func(_ httptrace.WroteRequestInfo) {
			wroteRequest = time.Now()
		},
		GotFirstResponseByte: func() {
			gotFirstByte = time.Now()
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
	fullDuration := time.Since(start).Nanoseconds()

	// Calculate server time (duration from request written to first response byte).
	var duration int64
	if !wroteRequest.IsZero() && !gotFirstByte.IsZero() {
		duration = gotFirstByte.Sub(wroteRequest).Nanoseconds()
	}

	// Read stats AFTER and compute delta.
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
		return "", duration, fullDuration, delta, fmt.Errorf("executing request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
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
