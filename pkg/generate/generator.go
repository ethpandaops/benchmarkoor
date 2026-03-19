package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/sirupsen/logrus"
)

const (
	// containerName is the prefix for generator containers.
	containerName = "benchmarkoor-generate"

	// rpcHealthCheckInterval is the interval between RPC health checks.
	rpcHealthCheckInterval = 2 * time.Second

	// rpcHealthCheckTimeout is the timeout for waiting for RPC readiness.
	rpcHealthCheckTimeout = 120 * time.Second
)

// Generator orchestrates EEST fixture generation.
type Generator struct {
	log          logrus.FieldLogger
	cfg          *config.GenerateConfig
	containerMgr docker.ContainerManager
	registry     client.Registry
}

// NewGenerator creates a new fixture generator.
func NewGenerator(
	log logrus.FieldLogger,
	cfg *config.GenerateConfig,
	containerMgr docker.ContainerManager,
	registry client.Registry,
) *Generator {
	return &Generator{
		log:          log,
		cfg:          cfg,
		containerMgr: containerMgr,
		registry:     registry,
	}
}

// Run executes the full fixture generation flow.
func (g *Generator) Run(ctx context.Context) error {
	// Resolve client spec.
	spec, err := g.registry.Get(client.ClientType(g.cfg.Client))
	if err != nil {
		return fmt.Errorf("resolving client spec: %w", err)
	}

	// Pull image.
	image := g.cfg.Image
	if image == "" {
		image = spec.DefaultImage()
	}

	g.log.WithField("image", image).Info("Pulling client image")

	if err := g.containerMgr.PullImage(ctx, image, "always"); err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	// Create temp directory for JWT and genesis.
	tempDir, err := os.MkdirTemp("", "benchmarkoor-generate-")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}

	defer func() {
		if rmErr := os.RemoveAll(tempDir); rmErr != nil {
			g.log.WithError(rmErr).Warn("Failed to remove temp directory")
		}
	}()

	// Write JWT file.
	jwt := config.DefaultJWT
	jwtFile := filepath.Join(tempDir, "jwtsecret")

	if err := os.WriteFile(jwtFile, []byte(jwt), 0644); err != nil {
		return fmt.Errorf("writing JWT file: %w", err)
	}

	// Load genesis if configured.
	var genesisFile string

	if g.cfg.Genesis != "" {
		content, readErr := os.ReadFile(g.cfg.Genesis)
		if readErr != nil {
			return fmt.Errorf("reading genesis file: %w", readErr)
		}

		genesisFile = filepath.Join(tempDir, "genesis.json")

		if err := os.WriteFile(genesisFile, content, 0644); err != nil {
			return fmt.Errorf("writing genesis file: %w", err)
		}
	}

	// Build container command.
	cmd := spec.DefaultCommand()
	cmd = append(cmd, g.cfg.ExtraArgs...)

	if genesisFile != "" && spec.GenesisFlag() != "" {
		cmd = append(cmd, spec.GenesisFlag()+spec.GenesisPath())
	}

	// Build mounts.
	mounts := []docker.Mount{
		{
			Type:     "bind",
			Source:   jwtFile,
			Target:   spec.JWTPath(),
			ReadOnly: true,
		},
	}

	if genesisFile != "" {
		mounts = append(mounts, docker.Mount{
			Type:     "bind",
			Source:   genesisFile,
			Target:   spec.GenesisPath(),
			ReadOnly: true,
		})
	}

	// Create a volume for the datadir.
	volumeName := containerName + "-data"

	if err := g.containerMgr.CreateVolume(
		ctx, volumeName, map[string]string{
			"benchmarkoor.managed-by": "benchmarkoor",
			"benchmarkoor.type":       "generate",
		},
	); err != nil {
		return fmt.Errorf("creating volume: %w", err)
	}

	defer func() {
		if rmErr := g.containerMgr.RemoveVolume(
			context.Background(), volumeName,
		); rmErr != nil {
			g.log.WithError(rmErr).Warn("Failed to remove volume")
		}
	}()

	mounts = append(mounts, docker.Mount{
		Type:   "volume",
		Source: volumeName,
		Target: spec.DataDir(),
	})

	// Build environment.
	env := spec.DefaultEnvironment()
	if env == nil {
		env = make(map[string]string, len(g.cfg.Environment))
	}

	for k, v := range g.cfg.Environment {
		env[k] = v
	}

	// Ensure network exists.
	networkName := config.DefaultContainerNetwork

	if err := g.containerMgr.EnsureNetwork(ctx, networkName); err != nil {
		return fmt.Errorf("ensuring network: %w", err)
	}

	// Run init container if required.
	if spec.RequiresInit() && genesisFile != "" {
		g.log.Info("Running init container")

		initSpec := &docker.ContainerSpec{
			Name:        containerName + "-init",
			Image:       image,
			Command:     spec.InitCommand(),
			Mounts:      mounts,
			NetworkName: networkName,
			Labels: map[string]string{
				"benchmarkoor.managed-by": "benchmarkoor",
				"benchmarkoor.type":       "generate-init",
			},
		}

		if err := g.containerMgr.RunInitContainer(
			ctx, initSpec, os.Stdout, os.Stderr,
		); err != nil {
			return fmt.Errorf("running init container: %w", err)
		}
	}

	// Create and start client container.
	containerSpec := &docker.ContainerSpec{
		Name:        containerName,
		Image:       image,
		Command:     cmd,
		Env:         env,
		Mounts:      mounts,
		NetworkName: networkName,
		Labels: map[string]string{
			"benchmarkoor.managed-by": "benchmarkoor",
			"benchmarkoor.type":       "generate",
		},
	}

	containerID, err := g.containerMgr.CreateContainer(ctx, containerSpec)
	if err != nil {
		return fmt.Errorf("creating container: %w", err)
	}

	defer func() {
		stopErr := g.containerMgr.StopContainer(context.Background(), containerID)
		if stopErr != nil {
			g.log.WithError(stopErr).Warn("Failed to stop container")
		}

		rmErr := g.containerMgr.RemoveContainer(context.Background(), containerID)
		if rmErr != nil {
			g.log.WithError(rmErr).Warn("Failed to remove container")
		}
	}()

	if err := g.containerMgr.StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	g.log.Info("Client container started")

	// Stream container logs in the background.
	go func() {
		if streamErr := g.containerMgr.StreamLogs(
			ctx, containerID, os.Stdout, os.Stderr,
		); streamErr != nil {
			g.log.WithError(streamErr).Debug("Container log streaming ended")
		}
	}()

	// Get container IP.
	containerIP, err := g.containerMgr.GetContainerIP(ctx, containerID, networkName)
	if err != nil {
		return fmt.Errorf("getting container IP: %w", err)
	}

	rpcURL := fmt.Sprintf("http://%s:%d", containerIP, spec.RPCPort())
	engineURL := fmt.Sprintf("http://%s:%d", containerIP, spec.EnginePort())

	// Wait for RPC to be ready.
	g.log.Info("Waiting for RPC to be ready")

	if err := g.waitForRPC(ctx, rpcURL); err != nil {
		return fmt.Errorf("waiting for RPC: %w", err)
	}

	g.log.Info("Client RPC is ready")

	// Get genesis block info.
	headHash, err := g.getHeadHash(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("getting head block hash: %w", err)
	}

	g.log.WithField("head_hash", headHash).Info("Got genesis head hash")

	// Send bootstrap FCU.
	engine := NewEngineClient(
		g.log, engineURL, rpcURL, jwt,
		g.cfg.ExecutionSpecs.Fork, headHash, 0,
	)

	bootstrapFCU := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "engine_forkchoiceUpdatedV3",
		Params: []any{
			map[string]string{
				"headBlockHash":      headHash,
				"safeBlockHash":      zeroHash,
				"finalizedBlockHash": zeroHash,
			},
			nil,
		},
		ID: 1,
	}

	resp, err := engine.doRPCCall(ctx, engineURL, bootstrapFCU, true)
	if err != nil {
		return fmt.Errorf("bootstrap FCU: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf(
			"bootstrap FCU error (code %d): %s",
			resp.Error.Code, resp.Error.Message,
		)
	}

	g.log.Info("Bootstrap FCU sent successfully")

	// Create output directory and fixture writer.
	outputDir := g.cfg.OutputDir
	if outputDir == "" {
		outputDir = "./generated-fixtures"
	}

	fixtureWriter := NewFixtureWriter(g.log, outputDir)

	if err := fixtureWriter.Init(); err != nil {
		return fmt.Errorf("initializing fixture writer: %w", err)
	}

	// Run gas bump blocks if enabled.
	if g.cfg.GasBump.Enabled {
		g.log.WithField("count", g.cfg.GasBump.Count).Info("Running gas bump blocks")

		for i := 0; i < g.cfg.GasBump.Count; i++ {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("gas bump interrupted: %w", err)
			}

			if err := engine.BuildEmptyBlock(ctx); err != nil {
				return fmt.Errorf("gas bump block %d: %w", i, err)
			}

			if (i+1)%1000 == 0 {
				g.log.WithField("progress", fmt.Sprintf("%d/%d", i+1, g.cfg.GasBump.Count)).
					Info("Gas bump progress")
			}
		}

		g.log.Info("Gas bump blocks complete")
	}

	// Run funding block if enabled.
	if g.cfg.Funding.Enabled {
		g.log.WithFields(logrus.Fields{
			"address": g.cfg.Funding.Address,
			"amount":  g.cfg.Funding.WithdrawalAmount,
		}).Info("Running funding block")

		if err := engine.BuildFundingBlock(
			ctx, g.cfg.Funding.Address, g.cfg.Funding.WithdrawalAmount,
		); err != nil {
			return fmt.Errorf("funding block: %w", err)
		}

		g.log.Info("Funding block complete")
	}

	// Start proxy.
	proxy := NewProxy(g.log, ":0", rpcURL, engine, fixtureWriter)

	if err := proxy.Start(ctx); err != nil {
		return fmt.Errorf("starting proxy: %w", err)
	}

	defer func() {
		if stopErr := proxy.Stop(context.Background()); stopErr != nil {
			g.log.WithError(stopErr).Warn("Failed to stop proxy")
		}
	}()

	proxyEndpoint := fmt.Sprintf("http://localhost:%d", proxy.Port())

	g.log.WithField("endpoint", proxyEndpoint).Info("Proxy ready")

	// Setup Python environment.
	cacheDir, err := getCacheDir()
	if err != nil {
		return fmt.Errorf("getting cache directory: %w", err)
	}

	pythonEnv := NewPythonEnv(g.log)

	if err := pythonEnv.Setup(
		ctx,
		g.cfg.ExecutionSpecs.Repo,
		g.cfg.ExecutionSpecs.Branch,
		g.cfg.ExecutionSpecs.Commit,
		g.cfg.ExecutionSpecs.LocalPath,
		cacheDir,
	); err != nil {
		return fmt.Errorf("setting up Python environment: %w", err)
	}

	// Run execute remote.
	g.log.Info("Running execute remote")

	opts := &ExecuteRemoteOpts{
		Fork:               g.cfg.ExecutionSpecs.Fork,
		RPCEndpoint:        proxyEndpoint,
		SeedKey:            g.cfg.ExecutionSpecs.SeedKey,
		ChainID:            g.cfg.ExecutionSpecs.ChainID,
		GasBenchmarkValues: g.cfg.ExecutionSpecs.GasBenchmarkValues,
		TestPath:           g.cfg.ExecutionSpecs.TestPath,
		EESTMode:           g.cfg.ExecutionSpecs.EESTMode,
		ParameterFilter:    g.cfg.ExecutionSpecs.ParameterFilter,
		AddressStubs:       g.cfg.ExecutionSpecs.AddressStubs,
		ExtraPytestArgs:    g.cfg.ExecutionSpecs.ExtraPytestArgs,
	}

	if err := pythonEnv.RunExecuteRemote(ctx, opts); err != nil {
		return fmt.Errorf("execute remote: %w", err)
	}

	// Flush proxy (handles remaining buffered txns).
	if err := proxy.Stop(ctx); err != nil {
		g.log.WithError(err).Warn("Error during proxy shutdown")
	}

	g.log.WithField("output_dir", outputDir).Info("Fixture generation complete")

	return nil
}

// waitForRPC polls the RPC endpoint until it responds or context is cancelled.
func (g *Generator) waitForRPC(ctx context.Context, rpcURL string) error {
	ctx, cancel := context.WithTimeout(ctx, rpcHealthCheckTimeout)
	defer cancel()

	ticker := time.NewTicker(rpcHealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for RPC: %w", ctx.Err())
		case <-ticker.C:
			if g.checkRPCHealth(ctx, rpcURL) {
				return nil
			}
		}
	}
}

// checkRPCHealth performs a single web3_clientVersion health check.
func (g *Generator) checkRPCHealth(ctx context.Context, rpcURL string) bool {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "web3_clientVersion",
		Params:  []any{},
		ID:      1,
	}

	// Create a temporary engine client for the health check.
	tmpClient := &EngineClient{log: g.log}

	resp, err := tmpClient.doRPCCall(ctx, rpcURL, req, false)
	if err != nil {
		return false
	}

	return resp.Error == nil
}

// getHeadHash fetches the head block hash from the client.
func (g *Generator) getHeadHash(ctx context.Context, rpcURL string) (string, error) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "eth_getBlockByNumber",
		Params:  []any{"latest", false},
		ID:      1,
	}

	tmpClient := &EngineClient{log: g.log}

	resp, err := tmpClient.doRPCCall(ctx, rpcURL, req, false)
	if err != nil {
		return "", fmt.Errorf("fetching latest block: %w", err)
	}

	if resp.Error != nil {
		return "", fmt.Errorf(
			"eth_getBlockByNumber error (code %d): %s",
			resp.Error.Code, resp.Error.Message,
		)
	}

	var block struct {
		Hash string `json:"hash"`
	}

	if err := decodeJSONResult(resp.Result, &block); err != nil {
		return "", fmt.Errorf("parsing block: %w", err)
	}

	return block.Hash, nil
}

// getCacheDir returns the cache directory for generate operations.
func getCacheDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}

	cacheDir := filepath.Join(homeDir, ".cache", "benchmarkoor", "generate")

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	return cacheDir, nil
}

// decodeJSONResult unmarshals a json.RawMessage result into a target struct.
func decodeJSONResult(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}
