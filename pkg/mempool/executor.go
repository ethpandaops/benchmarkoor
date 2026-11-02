// Package mempool is the mempool executor mode: it runs mempool (txpool)
// scenarios against a client, with the harness acting as the CL. It plugs
// into the runner through executor.Executor, so it reuses the container
// lifecycle, the rollback strategies, resource stats and results layout.
package mempool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/observer"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/scenario"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
	"github.com/ethpandaops/benchmarkoor/pkg/stats"
)

// SchemaVersion is the version of mempool-result.json.
const SchemaVersion = 1

// RunResultFile is the run-level result file, next to config.json.
const RunResultFile = "mempool-result.json"

// scenarioResultFile is the per-scenario result file in the scenario dir.
const scenarioResultFile = "mempool.result.json"

// RunResult is the content of runs/<run>/mempool-result.json.
type RunResult struct {
	SchemaVersion int                 `json:"schema_version"`
	Mode          string              `json:"mode"`
	Client        string              `json:"client"`
	InstanceID    string              `json:"instance_id"`
	Fork          string              `json:"fork"`
	ChainID       uint64              `json:"chain_id"`
	PoolLimits    scenario.PoolLimits `json:"pool_limits"`
	Scenarios     []*scenario.Result  `json:"scenarios"`
}

// Config configures the mempool executor.
type Config struct {
	Mempool      config.MempoolConfig
	ResultsDir   string
	ResultsOwner *fsutil.OwnerConfig
	// CacheDir holds the generated genesis.
	CacheDir string
	// Filter runs only the scenarios whose name contains it.
	Filter string
	// SystemResourceCollectionEnabled samples the container memory.
	SystemResourceCollectionEnabled bool
	// ScenarioTimeout caps one scenario.
	ScenarioTimeout time.Duration
}

// plannedScenario is one configured scenario with its resolved parameters and
// its account range.
type plannedScenario struct {
	scenario     scenario.Scenario
	params       scenario.Params
	accountBase  int
	accountCount int
}

type mempoolExecutor struct {
	log      logrus.FieldLogger
	cfg      Config
	registry *scenario.Registry

	plan         []*plannedScenario
	tests        []*executor.TestWithSteps
	suiteHash    string
	genesisPaths map[string]string // client type -> genesis file

	progressTotal  atomic.Int64
	progressPassed atomic.Int64
	progressFailed atomic.Int64

	liveMu sync.RWMutex
	live   map[string]executor.LiveTestStats

	// resultMu serialises the read-modify-write of mempool-result.json.
	resultMu sync.Mutex
}

// Ensure interface compliance.
var (
	_ executor.Executor        = (*mempoolExecutor)(nil)
	_ executor.GenesisProvider = (*mempoolExecutor)(nil)
)

// NewExecutor returns the mempool executor.
func NewExecutor(log logrus.FieldLogger, cfg Config) executor.Executor {
	if cfg.ScenarioTimeout == 0 {
		cfg.ScenarioTimeout = 20 * time.Minute
	}

	return &mempoolExecutor{
		log:      log.WithField("component", "mempool-executor"),
		cfg:      cfg,
		registry: scenario.NewRegistry(),
		live:     make(map[string]executor.LiveTestStats, 8),
	}
}

// Start resolves the scenarios, writes the genesis and the suite summary.
func (e *mempoolExecutor) Start(_ context.Context) error {
	m := e.cfg.Mempool

	configured := m.Scenarios
	if len(configured) == 0 {
		for _, name := range e.registry.Names() {
			configured = append(configured, config.MempoolScenarioConfig{Name: name})
		}
	}

	next := 0

	for _, sc := range configured {
		s, err := e.registry.Get(sc.Name)
		if err != nil {
			return err
		}

		params, err := scenario.Params(sc.Params).Merge(s.Defaults())
		if err != nil {
			return fmt.Errorf("scenario %q: %w", sc.Name, err)
		}

		n := s.Accounts(params)
		e.plan = append(e.plan, &plannedScenario{
			scenario: s, params: params, accountBase: next, accountCount: n,
		})
		next += n
	}

	if next > m.Accounts {
		return fmt.Errorf("the scenarios need %d accounts, but runner.benchmark.mempool.accounts is %d",
			next, m.Accounts)
	}

	if e.cfg.Filter != "" {
		kept := e.plan[:0]

		for _, p := range e.plan {
			if strings.Contains(p.scenario.Name(), e.cfg.Filter) {
				kept = append(kept, p)
			}
		}

		e.plan = kept
	}

	if len(e.plan) == 0 {
		return errors.New("no mempool scenario to run")
	}

	e.tests = make([]*executor.TestWithSteps, 0, len(e.plan))
	for _, p := range e.plan {
		e.tests = append(e.tests, &executor.TestWithSteps{Name: p.scenario.Name()})
	}

	gen, err := genesisJSON(m)
	if err != nil {
		return err
	}

	// The suite hash covers everything that decides the results: the
	// genesis, the scenarios with their parameters, and the version.
	resolved := make([]config.MempoolScenarioConfig, 0, len(e.plan))
	for _, p := range e.plan {
		resolved = append(resolved, config.MempoolScenarioConfig{Name: p.scenario.Name(), Params: p.params})
	}

	scenariosJSON, err := json.Marshal(resolved)
	if err != nil {
		return fmt.Errorf("encoding the scenarios: %w", err)
	}

	h := sha256.New()
	h.Write([]byte("mempool/v1\n"))
	h.Write(gen)
	h.Write(scenariosJSON)
	limitsJSON, err := json.Marshal(m.PoolLimits)
	if err != nil {
		return fmt.Errorf("encoding the pool limits: %w", err)
	}

	h.Write(limitsJSON)
	e.suiteHash = hex.EncodeToString(h.Sum(nil))[:16]

	dir := filepath.Join(e.cfg.CacheDir, "mempool", e.suiteHash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	files, err := clientGenesisFiles(m)
	if err != nil {
		return err
	}

	e.genesisPaths = make(map[string]string, len(files))

	for client, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, f.data, 0o644); err != nil {
			return fmt.Errorf("writing the %s genesis: %w", client, err)
		}

		e.genesisPaths[client] = path
	}

	if err := e.writeSuite(resolved); err != nil {
		return err
	}

	e.log.WithFields(logrus.Fields{
		"suite_hash":  e.suiteHash,
		"scenarios":   len(e.plan),
		"accounts":    m.Accounts,
		"genesis_dir": dir,
	}).Info("Mempool executor ready")

	return nil
}

func (e *mempoolExecutor) writeSuite(resolved []config.MempoolScenarioConfig) error {
	m := e.cfg.Mempool
	tests := make([]executor.SuiteTest, 0, len(e.plan))

	for _, p := range e.plan {
		tests = append(tests, executor.SuiteTest{Name: p.scenario.Name()})
	}

	info := &executor.SuiteInfo{
		Hash: e.suiteHash,
		Mode: config.ExecutorModeMempool,
		Source: &executor.SuiteSource{Mempool: &executor.MempoolSourceInfo{
			Fork: m.Fork, ChainID: m.ChainID, Seed: m.Seed, Accounts: m.Accounts,
			GasLimit: m.GasLimit, Scenarios: resolved,
		}},
		Filter: e.cfg.Filter,
		Tests:  tests,
	}

	dir := filepath.Join(e.cfg.ResultsDir, "suites", e.suiteHash)
	if err := fsutil.MkdirAll(dir, 0o755, e.cfg.ResultsOwner); err != nil {
		return fmt.Errorf("creating the suite dir: %w", err)
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the suite summary: %w", err)
	}

	return fsutil.WriteFile(filepath.Join(dir, "summary.json"), data, 0o644, e.cfg.ResultsOwner)
}

func (e *mempoolExecutor) Stop() error { return nil }

func (e *mempoolExecutor) GetSuiteHash() string { return e.suiteHash }

func (e *mempoolExecutor) GetTests() []*executor.TestWithSteps { return e.tests }

// GetGenesisPath returns the generated genesis in the format of the client,
// or "" for a client without one.
func (e *mempoolExecutor) GetGenesisPath(clientType string) string {
	return e.genesisPaths[clientType]
}

func (e *mempoolExecutor) GetProgress() executor.ProgressSnapshot {
	return executor.ProgressSnapshot{
		TestsTotal:  int(e.progressTotal.Load()),
		TestsPassed: int(e.progressPassed.Load()),
		TestsFailed: int(e.progressFailed.Load()),
	}
}

func (e *mempoolExecutor) ResetProgress(total int) {
	e.progressTotal.Store(int64(total))
	e.progressPassed.Store(0)
	e.progressFailed.Store(0)

	e.liveMu.Lock()
	e.live = make(map[string]executor.LiveTestStats, total)
	e.liveMu.Unlock()
}

func (e *mempoolExecutor) GetLiveTests() map[string]executor.LiveTestStats {
	e.liveMu.RLock()
	defer e.liveMu.RUnlock()

	out := make(map[string]executor.LiveTestStats, len(e.live))
	for k, v := range e.live {
		out[k] = v
	}

	return out
}

// ExecuteTests runs the scenarios (all, or opts.Tests) against one client.
func (e *mempoolExecutor) ExecuteTests(ctx context.Context, opts *executor.ExecuteOptions,
) (*executor.ExecutionResult, error) {
	start := time.Now()

	plan := e.plan
	if opts.Tests != nil {
		want := make(map[string]bool, len(opts.Tests))
		for _, t := range opts.Tests {
			want[t.Name] = true
		}

		plan = make([]*plannedScenario, 0, len(opts.Tests))

		for _, p := range e.plan {
			if want[p.scenario.Name()] {
				plan = append(plan, p)
			}
		}
	}

	result := &executor.ExecutionResult{TotalTests: len(plan)}

	m := e.cfg.Mempool
	log := e.log.WithField("engine", opts.EngineEndpoint)

	memory, stopMemory := e.memorySampler(ctx, log, opts)
	defer stopMemory()

	driver := cldriver.New(log, cldriver.Config{
		EngineURL:    opts.EngineEndpoint,
		JWTSecret:    opts.JWT,
		Forks:        []cldriver.Fork{{Name: m.Fork, Time: 0}},
		FeeRecipient: common.HexToAddress("0x000000000000000000000000000000000000fee5"),
		PayloadDelay: 250 * time.Millisecond,
	})
	if err := driver.Start(ctx); err != nil {
		return result, fmt.Errorf("starting the CL driver: %w", err)
	}

	defer func() { _ = driver.Stop() }()

	obs := observer.New(log, observer.Config{RPCURL: opts.RPCEndpoint, Memory: memory})
	if err := obs.Start(ctx); err != nil {
		return result, fmt.Errorf("starting the pool observer: %w", err)
	}

	defer func() { _ = obs.Stop() }()

	factory := txfactory.New(m.ChainID, m.Seed, m.Accounts)
	limits := scenario.PoolLimits(m.PoolLimits)
	blobCfg := osakaBlobConfig()

	instanceID, client := readInstance(opts.ResultsDir)

	for _, p := range plan {
		name := p.scenario.Name()
		slog := log.WithField("scenario", name)
		slog.Info("Running mempool scenario")

		rec := scenario.NewRecorder(name, p.scenario.Description(), p.params)
		env := scenario.NewEnv(scenario.Env{
			Log:     slog,
			Driver:  driver,
			Pool:    obs,
			Factory: factory,
			Rec:     rec,
			Params:  p.params,
			Limits:  limits,
			Chain: scenario.Chain{
				ChainID:          m.ChainID,
				GasLimit:         m.GasLimit,
				MaxTxGas:         params.MaxTxGas,
				MaxBlobsPerBlock: blobCfg.Max,
			},
		}, p.accountBase, p.accountCount)

		scenarioCtx, cancel := context.WithTimeout(ctx, e.cfg.ScenarioTimeout)
		runErr := p.scenario.Run(scenarioCtx, env)

		cancel()

		res := rec.Finish(runErr, obs.Samples())

		if err := e.writeScenario(opts.ResultsDir, res, instanceID, client); err != nil {
			slog.WithError(err).Warn("Failed to write the scenario result")
		}

		passed := res.Status == scenario.StatusPassed
		if passed {
			result.Passed++

			e.progressPassed.Add(1)
		} else {
			result.Failed++

			e.progressFailed.Add(1)
		}

		e.liveMu.Lock()
		e.live[name] = executor.LiveTestStats{Passed: passed}
		e.liveMu.Unlock()

		slog.WithFields(logrus.Fields{
			"status":   res.Status,
			"duration": time.Duration(res.DurationMs * float64(time.Millisecond)).Round(time.Millisecond),
			"error":    res.Error,
		}).Info("Mempool scenario done")

		if ctx.Err() != nil {
			result.TerminationReason = "context cancelled"

			break
		}
	}

	result.TotalDuration = time.Since(start)

	return result, nil
}

// memorySampler reads the container memory in the background. Reading stats
// can take a second (the Docker stats API), so the pool sampler only reads
// the last value.
func (e *mempoolExecutor) memorySampler(ctx context.Context, log logrus.FieldLogger,
	opts *executor.ExecuteOptions,
) (observer.MemoryFunc, func()) {
	if !e.cfg.SystemResourceCollectionEnabled || opts.ContainerID == "" {
		return nil, func() {}
	}

	reader, err := stats.NewReader(log, opts.DockerClient, opts.ContainerID)
	if err != nil {
		log.WithError(err).Warn("No stats reader; the timeline has no memory")

		return nil, func() {}
	}

	var (
		last atomic.Uint64
		wg   sync.WaitGroup
		done = make(chan struct{})
	)

	wg.Add(1)

	go func() {
		defer wg.Done()

		for {
			if s, err := reader.ReadStats(); err == nil {
				last.Store(s.Memory)
			}

			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
	}()

	return last.Load, func() {
		close(done)
		wg.Wait()

		_ = reader.Close()
	}
}

// readInstance reads the instance id and client from the run config.json.
func readInstance(runDir string) (id, client string) {
	data, err := os.ReadFile(filepath.Join(runDir, "config.json"))
	if err != nil {
		return "", ""
	}

	var cfg struct {
		Instance struct {
			ID     string `json:"id"`
			Client string `json:"client"`
		} `json:"instance"`
	}

	if json.Unmarshal(data, &cfg) != nil {
		return "", ""
	}

	return cfg.Instance.ID, cfg.Instance.Client
}

// writeScenario writes the scenario result and merges it into the run-level
// mempool-result.json, in plan order.
func (e *mempoolExecutor) writeScenario(runDir string, res *scenario.Result, instanceID, client string) error {
	owner := e.cfg.ResultsOwner

	dir := filepath.Join(runDir, res.Name)
	if err := fsutil.MkdirAll(dir, 0o755, owner); err != nil {
		return err
	}

	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the scenario result: %w", err)
	}

	if err := fsutil.WriteFile(filepath.Join(dir, scenarioResultFile), data, 0o644, owner); err != nil {
		return err
	}

	e.resultMu.Lock()
	defer e.resultMu.Unlock()

	m := e.cfg.Mempool
	run := &RunResult{
		SchemaVersion: SchemaVersion,
		Mode:          config.ExecutorModeMempool,
		Client:        client,
		InstanceID:    instanceID,
		Fork:          m.Fork,
		ChainID:       m.ChainID,
		PoolLimits:    scenario.PoolLimits(m.PoolLimits),
	}

	byName := make(map[string]*scenario.Result, len(e.plan))

	for _, p := range e.plan {
		path := filepath.Join(runDir, p.scenario.Name(), scenarioResultFile)

		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var r scenario.Result
		if err := json.Unmarshal(raw, &r); err != nil {
			return fmt.Errorf("decoding %s: %w", path, err)
		}

		byName[r.Name] = &r
	}

	for _, p := range e.plan {
		if r, ok := byName[p.scenario.Name()]; ok {
			run.Scenarios = append(run.Scenarios, r)
		}
	}

	data, err = json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", RunResultFile, err)
	}

	return fsutil.WriteFile(filepath.Join(runDir, RunResultFile), data, 0o644, owner)
}
