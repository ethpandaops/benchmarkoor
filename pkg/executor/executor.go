package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
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
}

// ExecutionResult contains the overall execution summary.
type ExecutionResult struct {
	TotalTests    int
	Passed        int
	Failed        int
	TotalDuration time.Duration
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
		log: log.WithField("component", "executor"),
		cfg: cfg,
	}
}

type executor struct {
	log        logrus.FieldLogger
	cfg        *Config
	source     Source
	testsPath  string
	warmupPath string
	suiteHash  string
}

// Ensure interface compliance.
var _ Executor = (*executor)(nil)

// Start initializes the executor and prepares test sources.
func (e *executor) Start(ctx context.Context) error {
	e.source = NewSource(e.log, e.cfg.Source, e.cfg.CacheDir)
	if e.source == nil {
		return fmt.Errorf("no test source configured")
	}

	// Prepare source early (clone git or verify local dirs).
	e.log.Info("Preparing test sources")

	testsPath, warmupPath, err := e.source.Prepare(ctx)
	if err != nil {
		return fmt.Errorf("preparing source: %w", err)
	}

	e.testsPath = testsPath
	e.warmupPath = warmupPath

	e.log.WithFields(logrus.Fields{
		"tests_path":  testsPath,
		"warmup_path": warmupPath,
	}).Info("Test sources ready")

	// Create suite output if results directory is configured.
	if e.cfg.ResultsDir != "" {
		if err := e.createSuiteOutput(); err != nil {
			return fmt.Errorf("creating suite output: %w", err)
		}
	}

	return nil
}

// createSuiteOutput discovers tests, computes hash, and creates suite directory.
func (e *executor) createSuiteOutput() error {
	// Discover warmup tests.
	warmupFiles, err := DiscoverTests(e.warmupPath, e.cfg.Filter, true)
	if err != nil {
		return fmt.Errorf("discovering warmup tests: %w", err)
	}

	// Discover test files.
	testFiles, err := DiscoverTests(e.testsPath, e.cfg.Filter, false)
	if err != nil {
		return fmt.Errorf("discovering tests: %w", err)
	}

	// Compute suite hash from file contents.
	hash, err := ComputeSuiteHash(warmupFiles, testFiles)
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
	if err := CreateSuiteOutput(e.cfg.ResultsDir, hash, suiteInfo, warmupFiles, testFiles); err != nil {
		return fmt.Errorf("creating suite output: %w", err)
	}

	e.log.WithFields(logrus.Fields{
		"hash":         hash,
		"warmup_files": len(warmupFiles),
		"test_files":   len(testFiles),
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
func (e *executor) ExecuteTests(ctx context.Context, opts *ExecuteOptions) (*ExecutionResult, error) {
	startTime := time.Now()

	// Combine filter from config and options.
	filter := e.cfg.Filter
	if opts.Filter != "" {
		filter = opts.Filter
	}

	// Discover warmup tests.
	warmupTests, err := DiscoverTests(e.warmupPath, filter, true)
	if err != nil {
		return nil, fmt.Errorf("discovering warmup tests: %w", err)
	}

	// Discover actual tests.
	tests, err := DiscoverTests(e.testsPath, filter, false)
	if err != nil {
		return nil, fmt.Errorf("discovering tests: %w", err)
	}

	e.log.WithFields(logrus.Fields{
		"warmup_tests": len(warmupTests),
		"tests":        len(tests),
		"filter":       filter,
	}).Info("Discovered tests")

	// Run warmup tests first (no output).
	if len(warmupTests) > 0 {
		e.log.Info("Running warmup tests")

		for _, test := range warmupTests {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			e.log.WithField("test", test.Name).Debug("Running warmup test")

			if err := e.runTest(ctx, opts, test, nil); err != nil {
				e.log.WithError(err).WithField("test", test.Name).Warn("Warmup test failed")
			}
		}

		e.log.Info("Warmup tests completed")
	}

	// Run actual tests with result collection.
	result := &ExecutionResult{
		TotalTests: len(tests),
	}

	for _, test := range tests {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		log := e.log.WithField("test", test.Name)
		log.Info("Running test")

		testResult := NewTestResult(test.Name)

		if err := e.runTest(ctx, opts, test, testResult); err != nil {
			log.WithError(err).Error("Test failed")
			result.Failed++

			continue
		}

		// Write output files.
		if err := WriteResults(opts.ResultsDir, test.Name, testResult); err != nil {
			log.WithError(err).Warn("Failed to write results")
		}

		log.WithFields(logrus.Fields{
			"succeeded": testResult.Succeeded,
			"failed":    testResult.Failed,
		}).Info("Test completed")

		result.Passed++
	}

	result.TotalDuration = time.Since(startTime)

	// Generate and write the run result summary.
	runResult, err := GenerateRunResult(opts.ResultsDir)
	if err != nil {
		e.log.WithError(err).Warn("Failed to generate run result")
	} else {
		if err := WriteRunResult(opts.ResultsDir, runResult); err != nil {
			e.log.WithError(err).Warn("Failed to write run result")
		} else {
			e.log.WithField("tests_count", len(runResult.Tests)).Info("Run result written")
		}
	}

	return result, nil
}

// runTest executes a single test file.
func (e *executor) runTest(ctx context.Context, opts *ExecuteOptions, test TestFile, result *TestResult) error {
	file, err := os.Open(test.Path)
	if err != nil {
		return fmt.Errorf("opening test file: %w", err)
	}

	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
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
				"test": test.Name,
			}).WithError(err).Warn("Failed to parse JSON-RPC payload")

			if result != nil {
				result.AddResult("unknown", "", 0, false)
			}

			continue
		}

		// Execute RPC call.
		response, elapsed, err := e.executeRPC(ctx, opts.EngineEndpoint, opts.JWT, line)
		succeeded := err == nil

		if err != nil {
			e.log.WithFields(logrus.Fields{
				"line":   lineNum,
				"method": method,
				"test":   test.Name,
			}).WithError(err).Warn("RPC call failed")
		}

		if result != nil {
			result.AddResult(method, response, elapsed, succeeded)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading test file: %w", err)
	}

	return nil
}

// executeRPC executes a single JSON-RPC call against the Engine API.
func (e *executor) executeRPC(ctx context.Context, endpoint, jwt, payload string) (string, int64, error) {
	token, err := GenerateJWTToken(jwt)
	if err != nil {
		return "", 0, fmt.Errorf("generating JWT: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(payload))
	if err != nil {
		return "", 0, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	elapsed := time.Since(start).Nanoseconds()

	if err != nil {
		return "", elapsed, fmt.Errorf("executing request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", elapsed, fmt.Errorf("reading response: %w", err)
	}

	return strings.TrimSpace(string(body)), elapsed, nil
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
