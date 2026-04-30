package docker

import (
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostMaxNofile_NonZero(t *testing.T) {
	got := HostMaxNofile()
	assert.NotZero(t, got, "HostMaxNofile must always return a non-zero value")
}

func TestHostMaxNofile_LinuxReadsProc(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only test: /proc/sys/fs/nr_open")
	}

	data, err := os.ReadFile("/proc/sys/fs/nr_open")
	if err != nil {
		t.Skipf("could not read /proc/sys/fs/nr_open: %v", err)
	}

	require.NotEmpty(t, data)

	got := HostMaxNofile()
	assert.GreaterOrEqual(t, got, uint64(1024), "kernel nr_open should be at least 1024")
}
