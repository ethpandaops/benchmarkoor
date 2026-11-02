package config

import (
	"fmt"
)

const (
	// ExecutorModeReplay replays JSON-RPC request files (the default).
	ExecutorModeReplay = "replay"
	// ExecutorModeMempool runs mempool (txpool) scenarios. The harness acts
	// as the CL through the Engine API.
	ExecutorModeMempool = "mempool"
)

// Mempool defaults. They pin the pool to the geth defaults, so the scenarios
// compare implementations, not configured limits.
const (
	DefaultMempoolFork          = "osaka"
	DefaultMempoolChainID       = 1337
	DefaultMempoolSeed          = "benchmarkoor-mempool"
	DefaultMempoolAccounts      = 4096
	DefaultMempoolGasLimit      = 30_000_000
	DefaultMempoolAccountSlots  = 16
	DefaultMempoolGlobalSlots   = 5120
	DefaultMempoolAccountQueue  = 64
	DefaultMempoolGlobalQueue   = 1024
	DefaultMempoolPriceBump     = 10
	DefaultMempoolBlobPriceBump = 100
)

// MempoolConfig configures the mempool executor mode.
type MempoolConfig struct {
	// Fork is the fork that is active from genesis. Only "osaka" for now.
	Fork string `yaml:"fork,omitempty" mapstructure:"fork"`
	// ChainID of the generated genesis.
	ChainID uint64 `yaml:"chain_id,omitempty" mapstructure:"chain_id"`
	// Seed derives the pre-funded keys. The same seed gives the same genesis.
	Seed string `yaml:"seed,omitempty" mapstructure:"seed"`
	// Accounts is the number of pre-funded accounts in the genesis.
	Accounts int `yaml:"accounts,omitempty" mapstructure:"accounts"`
	// GasLimit is the block gas limit of the genesis and of built blocks.
	GasLimit uint64 `yaml:"gas_limit,omitempty" mapstructure:"gas_limit"`
	// PoolLimits are the pool limits the client profile pins.
	PoolLimits MempoolPoolLimits `yaml:"pool_limits,omitempty" mapstructure:"pool_limits"`
	// Scenarios to run, in order. Empty runs every registered scenario.
	Scenarios []MempoolScenarioConfig `yaml:"scenarios,omitempty" mapstructure:"scenarios"`
}

// MempoolPoolLimits are the pool limits the mempool client profile sets.
type MempoolPoolLimits struct {
	AccountSlots  int `yaml:"account_slots,omitempty" mapstructure:"account_slots" json:"account_slots"`
	GlobalSlots   int `yaml:"global_slots,omitempty" mapstructure:"global_slots" json:"global_slots"`
	AccountQueue  int `yaml:"account_queue,omitempty" mapstructure:"account_queue" json:"account_queue"`
	GlobalQueue   int `yaml:"global_queue,omitempty" mapstructure:"global_queue" json:"global_queue"`
	PriceBump     int `yaml:"price_bump,omitempty" mapstructure:"price_bump" json:"price_bump"`
	BlobPriceBump int `yaml:"blob_price_bump,omitempty" mapstructure:"blob_price_bump" json:"blob_price_bump"`
}

// MempoolScenarioConfig selects one scenario and sets its parameters.
type MempoolScenarioConfig struct {
	Name   string         `yaml:"name" mapstructure:"name" json:"name"`
	Params map[string]any `yaml:"params,omitempty" mapstructure:"params" json:"params,omitempty"`
}

// GetExecutorMode returns the executor mode, "replay" when not set.
func (b *BenchmarkConfig) GetExecutorMode() string {
	if b.Mode == "" {
		return ExecutorModeReplay
	}

	return b.Mode
}

// IsMempoolMode returns true when the mempool executor mode is selected.
func (c *Config) IsMempoolMode() bool {
	return c.Runner.Benchmark.GetExecutorMode() == ExecutorModeMempool
}

// GetMempool returns the mempool config with the defaults applied.
func (b *BenchmarkConfig) GetMempool() MempoolConfig {
	var m MempoolConfig
	if b.Mempool != nil {
		m = *b.Mempool
	}

	if m.Fork == "" {
		m.Fork = DefaultMempoolFork
	}

	if m.ChainID == 0 {
		m.ChainID = DefaultMempoolChainID
	}

	if m.Seed == "" {
		m.Seed = DefaultMempoolSeed
	}

	if m.Accounts == 0 {
		m.Accounts = DefaultMempoolAccounts
	}

	if m.GasLimit == 0 {
		m.GasLimit = DefaultMempoolGasLimit
	}

	l := &m.PoolLimits
	setDefault(&l.AccountSlots, DefaultMempoolAccountSlots)
	setDefault(&l.GlobalSlots, DefaultMempoolGlobalSlots)
	setDefault(&l.AccountQueue, DefaultMempoolAccountQueue)
	setDefault(&l.GlobalQueue, DefaultMempoolGlobalQueue)
	setDefault(&l.PriceBump, DefaultMempoolPriceBump)
	setDefault(&l.BlobPriceBump, DefaultMempoolBlobPriceBump)

	return m
}

func setDefault(v *int, def int) {
	if *v == 0 {
		*v = def
	}
}

// validateExecutorMode checks the mode and the settings that depend on it.
func (c *Config) validateExecutorMode() error {
	b := &c.Runner.Benchmark

	switch b.GetExecutorMode() {
	case ExecutorModeReplay:
		if b.Mempool != nil {
			return fmt.Errorf(
				"runner.benchmark.mempool is set but runner.benchmark.mode is %q",
				ExecutorModeReplay,
			)
		}

		return nil
	case ExecutorModeMempool:
	default:
		return fmt.Errorf(
			"runner.benchmark.mode: unknown mode %q (must be %q or %q)",
			b.Mode, ExecutorModeReplay, ExecutorModeMempool,
		)
	}

	// One run has one mode: a replay test source makes no sense here.
	if b.Tests.Source.IsConfigured() {
		return fmt.Errorf("runner.benchmark.tests.source cannot be used with mode %q",
			ExecutorModeMempool)
	}

	m := b.GetMempool()
	if m.Fork != DefaultMempoolFork {
		return fmt.Errorf("runner.benchmark.mempool.fork: only %q is supported, got %q",
			DefaultMempoolFork, m.Fork)
	}

	if m.Accounts < 64 {
		return fmt.Errorf("runner.benchmark.mempool.accounts must be at least 64, got %d",
			m.Accounts)
	}

	seen := make(map[string]struct{}, len(m.Scenarios))

	for i, s := range m.Scenarios {
		if s.Name == "" {
			return fmt.Errorf("runner.benchmark.mempool.scenarios[%d]: name is required", i)
		}

		if _, dup := seen[s.Name]; dup {
			return fmt.Errorf("runner.benchmark.mempool.scenarios: duplicate scenario %q",
				s.Name)
		}

		seen[s.Name] = struct{}{}
	}

	for _, instance := range c.Runner.Instances {
		// debug_setHead puts txs back into the pool and leaves it dirty for
		// the next scenario. A fresh container gives every scenario a clean
		// pool.
		if c.GetRollbackStrategy(&instance) == RollbackStrategyRPCDebugSetHead {
			return fmt.Errorf(
				"instance %q: rollback_strategy %q is not allowed in mode %q "+
					"(use %q, %q or %q)",
				instance.ID, RollbackStrategyRPCDebugSetHead, ExecutorModeMempool,
				RollbackStrategyContainerRecreate, RollbackStrategyCheckpointRestore,
				RollbackStrategyNone,
			)
		}

		if instance.DataDir != nil || c.Runner.Client.DataDirs[instance.Client] != nil {
			return fmt.Errorf(
				"instance %q: mode %q generates its own genesis and does not "+
					"support a datadir yet", instance.ID, ExecutorModeMempool,
			)
		}
	}

	return nil
}
