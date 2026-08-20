package client

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTempoDefaultCommandUsesIdleDevMode(t *testing.T) {
	command := NewTempoSpec().DefaultCommand()

	require.Contains(t, command, "--dev")
	require.Contains(t, command, "--dev.block-time=1h")
	require.Contains(t, command, "--debug.startup-sync-state-idle")
}
