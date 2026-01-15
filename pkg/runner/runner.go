package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
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
	ReadyTimeout       time.Duration
	ReadyWaitAfter     time.Duration
	TestFilter         string
}

// NewRunner creates a new runner instance.
func NewRunner(
	log logrus.FieldLogger,
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
		log:      log.WithField("component", "runner"),
		cfg:      cfg,
		docker:   dockerMgr,
		registry: registry,
		executor: exec,
		done:     make(chan struct{}),
	}
}

type runner struct {
	log      logrus.FieldLogger
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

// RunInstance runs a single client instance through its lifecycle.
func (r *runner) RunInstance(ctx context.Context, instance *config.ClientInstance) error {
	// Generate a short random ID for this run.
	runID := generateShortID()
	runTimestamp := time.Now().Unix()

	// Create run results directory.
	runResultsDir := filepath.Join(r.cfg.ResultsDir, fmt.Sprintf("%d_%s_%s", runTimestamp, runID, instance.ID))
	if err := os.MkdirAll(runResultsDir, 0755); err != nil {
		return fmt.Errorf("creating run results directory: %w", err)
	}

	// Create Docker volume for this run.
	volumeName := fmt.Sprintf("benchmarkoor-%s-%s", runID, instance.ID)
	volumeLabels := map[string]string{
		"benchmarkoor.instance":   instance.ID,
		"benchmarkoor.client":     instance.Client,
		"benchmarkoor.run-id":     runID,
		"benchmarkoor.managed-by": "benchmarkoor",
	}
	if err := r.docker.CreateVolume(ctx, volumeName, volumeLabels); err != nil {
		return fmt.Errorf("creating volume: %w", err)
	}

	defer func() {
		if rmErr := r.docker.RemoveVolume(context.Background(), volumeName); rmErr != nil {
			r.log.WithError(rmErr).Warn("Failed to remove volume")
		}
	}()

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

	// Create temp files for genesis and JWT.
	tempDir, err := os.MkdirTemp("", "benchmarkoor-"+instance.ID+"-")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}

	defer os.RemoveAll(tempDir)

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
		{
			Type:   "volume",
			Source: volumeName,
			Target: "/data",
		},
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

	// Run init container if required.
	if spec.RequiresInit() {
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
	defer func() {
		log.Info("Removing container")

		if rmErr := r.docker.RemoveContainer(context.Background(), containerID); rmErr != nil {
			log.WithError(rmErr).Warn("Failed to remove container")
		}
	}()

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

	// Get container IP for health checks.
	containerIP, err := r.docker.GetContainerIP(ctx, containerID, r.cfg.DockerNetwork)
	if err != nil {
		return fmt.Errorf("getting container IP: %w", err)
	}

	log.WithField("ip", containerIP).Debug("Container IP address")

	// Wait for RPC to be ready.
	if err := r.waitForRPC(ctx, containerIP, spec.RPCPort()); err != nil {
		return fmt.Errorf("waiting for RPC: %w", err)
	}

	log.Info("RPC endpoint ready")

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
		}

		result, err := r.executor.ExecuteTests(ctx, execOpts)
		if err != nil {
			log.WithError(err).Error("Test execution failed")
		} else {
			log.WithFields(logrus.Fields{
				"total":    result.TotalTests,
				"passed":   result.Passed,
				"failed":   result.Failed,
				"duration": result.TotalDuration,
			}).Info("Test execution completed")
		}
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

// waitForRPC waits for the RPC endpoint to be ready.
func (r *runner) waitForRPC(ctx context.Context, host string, port int) error {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.ReadyTimeout)
	defer cancel()

	url := fmt.Sprintf("http://%s:%d", host, port)
	body := `{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}`

	ticker := time.NewTicker(DefaultHealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for RPC: %w", ctx.Err())
		case <-ticker.C:
			if r.checkRPCHealth(ctx, url, body) {
				return nil
			}
		}
	}
}

// checkRPCHealth performs a single RPC health check.
func (r *runner) checkRPCHealth(ctx context.Context, url, body string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(io.NopCloser(io.Reader(nil)))

	// Create proper request with body.
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, url,
		io.NopCloser(ioStringReader(body)))
	if err != nil {
		return false
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// ioStringReader returns a reader for a string.
func ioStringReader(s string) io.Reader {
	return &stringReader{s: s}
}

type stringReader struct {
	s string
	i int
}

func (r *stringReader) Read(p []byte) (n int, err error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}

	n = copy(p, r.s[r.i:])
	r.i += n

	return n, nil
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

// generateShortID generates a short random hex ID (8 characters).
func generateShortID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails.
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}

	return hex.EncodeToString(b)
}
