package builder

import (
	"testing"

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
