package runner

import (
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/cputopology"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sixCoresSMT is a 6-core host with 2 threads per core, siblings (n, n+6).
func sixCoresSMT() []cputopology.CPU {
	cpus := make([]cputopology.CPU, 0, 12)
	for id := range 12 {
		cpus = append(cpus, cputopology.CPU{ID: id, Core: id % 6})
	}

	return cpus
}

func intPtr(v int) *int { return &v }

func TestBuildContainerResourceLimitsCpusetTopology(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		limits, resolved, err := buildContainerResourceLimits(nil, nil)
		require.NoError(t, err)
		assert.Nil(t, limits)
		assert.Nil(t, resolved)
	})

	t.Run("full_cores count picks sibling pairs", func(t *testing.T) {
		cfg := &config.ResourceLimits{CpusetCount: intPtr(4), CpusetTopology: "full_cores"}

		limits, resolved, err := buildContainerResourceLimits(cfg, sixCoresSMT())
		require.NoError(t, err)
		assert.Equal(t, "full_cores", resolved.CpusetTopology)
		assert.Equal(t, limits.CpusetCpus, resolved.CpusetCpus)

		cpus, err := cputopology.ParseCPUList(resolved.CpusetCpus)
		require.NoError(t, err)
		require.Len(t, cpus, 4)
		require.NoError(t, cputopology.Check(sixCoresSMT(), cpus, cputopology.ModeFullCores))
	})

	t.Run("one_thread_per_core count picks distinct cores", func(t *testing.T) {
		cfg := &config.ResourceLimits{CpusetCount: intPtr(6), CpusetTopology: "one_thread_per_core"}

		_, resolved, err := buildContainerResourceLimits(cfg, sixCoresSMT())
		require.NoError(t, err)
		assert.Equal(t, "0,1,2,3,4,5", resolved.CpusetCpus)
	})

	t.Run("explicit cpuset is checked against the mode", func(t *testing.T) {
		cfg := &config.ResourceLimits{Cpuset: []int{0, 1, 2, 3, 4, 5}, CpusetTopology: "full_cores"}

		_, _, err := buildContainerResourceLimits(cfg, sixCoresSMT())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "uses 1 of 2 threads")

		cfg.Cpuset = []int{0, 6, 1, 7}
		_, resolved, err := buildContainerResourceLimits(cfg, sixCoresSMT())
		require.NoError(t, err)
		assert.Equal(t, "0,6,1,7", resolved.CpusetCpus)
	})

	t.Run("any mode leaves the resolved topology empty", func(t *testing.T) {
		cfg := &config.ResourceLimits{Cpuset: []int{0, 1}}

		_, resolved, err := buildContainerResourceLimits(cfg, nil)
		require.NoError(t, err)
		assert.Empty(t, resolved.CpusetTopology)
		assert.Equal(t, "0,1", resolved.CpusetCpus)
	})

	t.Run("mode without a host topology fails", func(t *testing.T) {
		cfg := &config.ResourceLimits{CpusetCount: intPtr(2), CpusetTopology: "full_cores"}
		_, _, err := buildContainerResourceLimits(cfg, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not expose")

		cfg = &config.ResourceLimits{Cpuset: []int{0, 1}, CpusetTopology: "full_cores"}
		_, _, err = buildContainerResourceLimits(cfg, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not expose")
	})
}
