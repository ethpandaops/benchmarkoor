package cputopology

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hybridHost has two 2-thread cores (0/4, 1/5) and two 1-thread cores (2, 3).
var hybridHost = []CPU{
	{ID: 0, Core: 0}, {ID: 4, Core: 0},
	{ID: 1, Core: 1}, {ID: 5, Core: 1},
	{ID: 2, Core: 2},
	{ID: 3, Core: 3},
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		{in: "", want: ModeAny},
		{in: "any", want: ModeAny},
		{in: " full_cores ", want: ModeFullCores},
		{in: "one_thread_per_core", want: ModeOneThreadPerCore},
		{in: "cores", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseMode(tt.in)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSelect(t *testing.T) {
	t.Run("any picks distinct known cpus", func(t *testing.T) {
		for range 20 {
			got, err := Select(sixCoresSMT(), 5, ModeAny)
			require.NoError(t, err)
			require.Len(t, got, 5)
			assert.IsIncreasing(t, got)

			for _, id := range got {
				assert.Less(t, id, 12)
			}
		}
	})

	t.Run("full cores picks sibling pairs", func(t *testing.T) {
		for range 20 {
			got, err := Select(sixCoresSMT(), 6, ModeFullCores)
			require.NoError(t, err)
			require.Len(t, got, 6)
			require.NoError(t, Check(sixCoresSMT(), got, ModeFullCores))
			assert.Equal(t, "6 threads on 3 of 6 physical cores (2 of 2 threads per core)",
				Summary(sixCoresSMT(), got))
		}
	})

	t.Run("full cores rejects an odd count on a uniform smt host", func(t *testing.T) {
		_, err := Select(sixCoresSMT(), 5, ModeFullCores)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be filled with full cores")
	})

	t.Run("full cores fills an odd count on a hybrid host", func(t *testing.T) {
		for range 20 {
			got, err := Select(hybridHost, 3, ModeFullCores)
			require.NoError(t, err)
			require.Len(t, got, 3)
			require.NoError(t, Check(hybridHost, got, ModeFullCores))
		}
	})

	t.Run("one thread per core picks the lowest thread of distinct cores", func(t *testing.T) {
		for range 20 {
			got, err := Select(sixCoresSMT(), 6, ModeOneThreadPerCore)
			require.NoError(t, err)
			assert.Equal(t, []int{0, 1, 2, 3, 4, 5}, got)
		}

		got, err := Select(sixCoresSMT(), 2, ModeOneThreadPerCore)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.NoError(t, Check(sixCoresSMT(), got, ModeOneThreadPerCore))
	})

	t.Run("one thread per core rejects more threads than cores", func(t *testing.T) {
		_, err := Select(sixCoresSMT(), 7, ModeOneThreadPerCore)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds the 6 physical cores")
	})

	t.Run("rejects bad counts", func(t *testing.T) {
		_, err := Select(sixCoresSMT(), 0, ModeAny)
		require.Error(t, err)

		_, err = Select(sixCoresSMT(), 13, ModeAny)
		require.Error(t, err)
	})

	t.Run("rejects an unknown mode", func(t *testing.T) {
		_, err := Select(sixCoresSMT(), 1, Mode("cores"))
		require.Error(t, err)
	})
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		cpuset  []int
		mode    Mode
		wantErr string
	}{
		{name: "any accepts anything", cpuset: []int{0, 1, 2}, mode: ModeAny},
		{name: "full cores accepts sibling pairs", cpuset: []int{0, 6, 1, 7}, mode: ModeFullCores},
		{
			name:    "full cores rejects a half core",
			cpuset:  []int{0, 6, 1},
			mode:    ModeFullCores,
			wantErr: "core 1 on socket 0 uses 1 of 2 threads",
		},
		{name: "one thread per core accepts distinct cores", cpuset: []int{0, 1, 2}, mode: ModeOneThreadPerCore},
		{
			name:    "one thread per core rejects siblings",
			cpuset:  []int{0, 6},
			mode:    ModeOneThreadPerCore,
			wantErr: "core 0 on socket 0 uses 2 threads",
		},
		{
			name:    "unknown cpu fails",
			cpuset:  []int{99},
			mode:    ModeFullCores,
			wantErr: "not in the host topology",
		},
		{name: "unknown cpu passes with any", cpuset: []int{99}, mode: ModeAny},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Check(sixCoresSMT(), tt.cpuset, tt.mode)
			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
