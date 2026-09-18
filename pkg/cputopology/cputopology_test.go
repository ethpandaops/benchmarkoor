package cputopology

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeSysfs builds a sysfs tree for a host with the given CPUs. Each CPU
// is written under cpu/cpuN/topology, and NUMA nodes under node/nodeN/cpulist.
func writeFakeSysfs(t *testing.T, cpus []CPU, online string) string {
	t.Helper()

	root := t.TempDir()
	cpuDir := filepath.Join(root, "cpu")
	require.NoError(t, os.MkdirAll(cpuDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cpuDir, "online"), []byte(online+"\n"), 0o644))

	nodeCPUs := make(map[int][]int, 2)

	for _, c := range cpus {
		topo := filepath.Join(cpuDir, fmt.Sprintf("cpu%d", c.ID), "topology")
		require.NoError(t, os.MkdirAll(topo, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(topo, "core_id"),
			fmt.Appendf(nil, "%d\n", c.Core), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(topo, "physical_package_id"),
			fmt.Appendf(nil, "%d\n", c.Socket), 0o644))

		nodeCPUs[c.NUMA] = append(nodeCPUs[c.NUMA], c.ID)
	}

	for node, ids := range nodeCPUs {
		dir := filepath.Join(root, "node", fmt.Sprintf("node%d", node))
		require.NoError(t, os.MkdirAll(dir, 0o755))

		list := ""
		for i, id := range ids {
			if i > 0 {
				list += ","
			}

			list += fmt.Sprintf("%d", id)
		}

		require.NoError(t, os.WriteFile(filepath.Join(dir, "cpulist"), []byte(list+"\n"), 0o644))
	}

	return root
}

// sixCoresSMT is a 6-core host with 2 threads per core, siblings (n, n+6).
func sixCoresSMT() []CPU {
	cpus := make([]CPU, 0, 12)
	for id := range 12 {
		cpus = append(cpus, CPU{ID: id, Core: id % 6, Socket: 0, NUMA: 0})
	}

	return cpus
}

func TestRead(t *testing.T) {
	t.Run("reads core, socket and numa for each online cpu", func(t *testing.T) {
		want := []CPU{
			{ID: 0, Core: 0, Socket: 0, NUMA: 0},
			{ID: 1, Core: 0, Socket: 0, NUMA: 0},
			{ID: 2, Core: 1, Socket: 0, NUMA: 0},
			{ID: 3, Core: 1, Socket: 0, NUMA: 0},
			{ID: 4, Core: 0, Socket: 1, NUMA: 1},
			{ID: 5, Core: 0, Socket: 1, NUMA: 1},
		}
		root := writeFakeSysfs(t, want, "0-5")

		got, err := Read(root)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("skips offline cpus", func(t *testing.T) {
		all := sixCoresSMT()
		root := writeFakeSysfs(t, all, "0-5,7-11")

		got, err := Read(root)
		require.NoError(t, err)
		require.Len(t, got, 11)

		for _, c := range got {
			assert.NotEqual(t, 6, c.ID)
		}
	})

	t.Run("defaults numa to 0 without a node tree", func(t *testing.T) {
		root := writeFakeSysfs(t, sixCoresSMT(), "0-11")
		require.NoError(t, os.RemoveAll(filepath.Join(root, "node")))

		got, err := Read(root)
		require.NoError(t, err)

		for _, c := range got {
			assert.Equal(t, 0, c.NUMA)
		}
	})

	t.Run("fails without a cpu tree", func(t *testing.T) {
		_, err := Read(t.TempDir())
		require.Error(t, err)
	})
}

func TestParseCPUList(t *testing.T) {
	tests := []struct {
		name    string
		list    string
		want    []int
		wantErr bool
	}{
		{name: "empty", list: "", want: []int{}},
		{name: "single", list: "3", want: []int{3}},
		{name: "range", list: "0-3", want: []int{0, 1, 2, 3}},
		{name: "mixed unsorted", list: "8,0-2,10-11", want: []int{0, 1, 2, 8, 10, 11}},
		{name: "duplicates", list: "1,1,0-1", want: []int{0, 1}},
		{name: "spaces", list: " 0 , 2 ", want: []int{0, 2}},
		{name: "reversed range", list: "3-1", wantErr: true},
		{name: "garbage", list: "a-b", wantErr: true},
		{name: "trailing comma", list: "0,", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCPUList(tt.list)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSummary(t *testing.T) {
	twoSockets := []CPU{
		{ID: 0, Core: 0, Socket: 0, NUMA: 0},
		{ID: 1, Core: 0, Socket: 0, NUMA: 0},
		{ID: 2, Core: 1, Socket: 0, NUMA: 0},
		{ID: 3, Core: 1, Socket: 0, NUMA: 0},
		{ID: 4, Core: 0, Socket: 1, NUMA: 1},
		{ID: 5, Core: 0, Socket: 1, NUMA: 1},
		{ID: 6, Core: 1, Socket: 1, NUMA: 1},
		{ID: 7, Core: 1, Socket: 1, NUMA: 1},
	}

	tests := []struct {
		name   string
		cpus   []CPU
		cpuset []int
		want   string
	}{
		{
			name: "no topology",
			want: "",
		},
		{
			name: "host only",
			cpus: sixCoresSMT(),
			want: "12 threads on 6 physical cores (2 threads per core)",
		},
		{
			name:   "one thread per core across six cores",
			cpus:   sixCoresSMT(),
			cpuset: []int{0, 1, 2, 3, 4, 5},
			want:   "6 threads on 6 of 6 physical cores (1 of 2 threads per core)",
		},
		{
			name:   "both threads of three cores",
			cpus:   sixCoresSMT(),
			cpuset: []int{0, 6, 1, 7, 2, 8},
			want:   "6 threads on 3 of 6 physical cores (2 of 2 threads per core)",
		},
		{
			name:   "uneven usage shows a range",
			cpus:   sixCoresSMT(),
			cpuset: []int{0, 6, 1},
			want:   "3 threads on 2 of 6 physical cores (1-2 of 2 threads per core)",
		},
		{
			name:   "single thread",
			cpus:   sixCoresSMT(),
			cpuset: []int{4},
			want:   "1 thread on 1 of 6 physical cores (1 of 2 threads per core)",
		},
		{
			name:   "unknown cpu ids are ignored",
			cpus:   sixCoresSMT(),
			cpuset: []int{0, 99},
			want:   "1 thread on 1 of 6 physical cores (1 of 2 threads per core)",
		},
		{
			name:   "cpuset outside the host",
			cpus:   sixCoresSMT(),
			cpuset: []int{99},
			want:   "cpuset matches none of the 12 host threads",
		},
		{
			name: "no smt host",
			cpus: []CPU{{ID: 0, Core: 0}, {ID: 1, Core: 1}, {ID: 2, Core: 2}},
			want: "3 threads on 3 physical cores (1 thread per core)",
		},
		{
			name: "two sockets host",
			cpus: twoSockets,
			want: "8 threads on 4 physical cores (2 threads per core), 2 sockets, 2 NUMA nodes",
		},
		{
			name:   "two sockets pinned to one",
			cpus:   twoSockets,
			cpuset: []int{4, 5, 6, 7},
			want: "4 threads on 2 of 4 physical cores (2 of 2 threads per core), " +
				"1 of 2 sockets, 1 of 2 NUMA nodes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Summary(tt.cpus, tt.cpuset))
		})
	}
}
