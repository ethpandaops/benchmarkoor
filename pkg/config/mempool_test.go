package config

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mempoolTestConfig() *Config {
	return &Config{
		Runner: RunnerConfig{
			Benchmark: BenchmarkConfig{Mode: ExecutorModeMempool},
			Instances: []ClientInstance{{ID: "geth", Client: "geth"}},
		},
	}
}

func TestExecutorModeDefaults(t *testing.T) {
	var b BenchmarkConfig
	assert.Equal(t, ExecutorModeReplay, b.GetExecutorMode())

	m := b.GetMempool()
	assert.Equal(t, DefaultMempoolFork, m.Fork)
	assert.Equal(t, DefaultMempoolAccountSlots, m.PoolLimits.AccountSlots)
	assert.Equal(t, DefaultMempoolBlobPriceBump, m.PoolLimits.BlobPriceBump)
}

func TestValidateExecutorMode(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr string
	}{
		{"mempool ok", func(*Config) {}, ""},
		{"unknown mode", func(c *Config) { c.Runner.Benchmark.Mode = "x" }, "unknown mode"},
		{"replay with mempool block", func(c *Config) {
			c.Runner.Benchmark.Mode = ""
			c.Runner.Benchmark.Mempool = &MempoolConfig{}
		}, "runner.benchmark.mempool is set"},
		{"setHead refused", func(c *Config) {
			c.Runner.Client.Config.RollbackStrategy = RollbackStrategyRPCDebugSetHead
		}, "not allowed"},
		{"bad fork", func(c *Config) {
			c.Runner.Benchmark.Mempool = &MempoolConfig{Fork: "prague"}
		}, "only \"osaka\""},
		{"duplicate scenario", func(c *Config) {
			c.Runner.Benchmark.Mempool = &MempoolConfig{Scenarios: []MempoolScenarioConfig{
				{Name: "a"}, {Name: "a"},
			}}
		}, "duplicate scenario"},
		{"replay source refused", func(c *Config) {
			c.Runner.Benchmark.Tests.Source.Local = &LocalSourceV2{BaseDir: "/x"}
		}, "cannot be used"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := mempoolTestConfig()
			tc.mutate(c)

			err := c.validateExecutorMode()
			if tc.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestMempoolRollbackDefault(t *testing.T) {
	c := mempoolTestConfig()
	assert.Equal(t, RollbackStrategyContainerRecreate, c.GetRollbackStrategy(&c.Runner.Instances[0]))

	c.Runner.Benchmark.Mode = ""
	assert.Equal(t, RollbackStrategyRPCDebugSetHead, c.GetRollbackStrategy(&c.Runner.Instances[0]))
}

func TestUseLoopbackClientPorts(t *testing.T) {
	on, off := true, false

	c := &Config{}
	assert.Equal(t, runtime.GOOS == "darwin", c.UseLoopbackClientPorts(), "default follows the OS")

	c.Runner.LoopbackClientPorts = &off
	assert.False(t, c.UseLoopbackClientPorts())

	c.Runner.LoopbackClientPorts = &on
	assert.True(t, c.UseLoopbackClientPorts())

	c.Runner.ContainerRuntime = "podman"
	require.Error(t, c.validateContainerRuntime())

	c.Runner.LoopbackClientPorts = nil
	assert.False(t, c.UseLoopbackClientPorts(), "podman never defaults to loopback")
}
