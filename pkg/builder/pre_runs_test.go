package builder

import (
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/docker"

	"github.com/stretchr/testify/require"
)

// A pre-run's datadir is its product, so a filler that had to be killed fails
// the target whether or not it promotes.
func TestRequireCleanStop(t *testing.T) {
	b := &PreRunsBuilder{}
	require.ErrorContains(t, b.requireCleanStop(), "did not stop cleanly")

	b.fillerExit = fillerExitState{detail: "the client was OOM-killed"}
	require.ErrorContains(t, b.requireCleanStop(), "OOM-killed")

	b.fillerExit = fillerExitState{graceful: true}
	require.NoError(t, b.requireCleanStop())
}

// nethermind exits 130 and besu 143 after the signal a stop sends, having logged
// a complete shutdown; only a SIGKILL, an OOM kill or another code is not clean.
func TestClassifyFillerExit(t *testing.T) {
	for _, code := range []int64{0, 130, 143} {
		require.True(t, classifyFillerExit(docker.ContainerExitInfo{ExitCode: code}, 30).graceful, "exit %d", code)
	}

	killed := classifyFillerExit(docker.ContainerExitInfo{ExitCode: 137}, 900)
	require.False(t, killed.graceful)
	require.Contains(t, killed.detail, "900s")

	require.Contains(t, classifyFillerExit(docker.ContainerExitInfo{ExitCode: 0, OOMKilled: true}, 30).detail, "OOM")
	require.Contains(t, classifyFillerExit(docker.ContainerExitInfo{ExitCode: 1}, 30).detail, "code 1")
}
