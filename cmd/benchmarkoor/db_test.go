package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestDBCompactRejectsInvalidInputs(t *testing.T) {
	original := dbCompactFlags
	t.Cleanup(func() { dbCompactFlags = original })

	// Invalid input must be rejected without a reachable container engine.
	t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(t.TempDir(), "missing.sock"))

	for _, tc := range []struct {
		name    string
		datadir string
		timeout string
		want    string
	}{
		{"empty datadir", "", "1m", "--datadir must not be empty"},
		{"zero timeout", t.TempDir(), "0", "--timeout must be positive"},
		{"negative timeout", t.TempDir(), "-1s", "--timeout must be positive"},
		{"malformed timeout", t.TempDir(), "invalid", "--timeout:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbCompactFlags.client = "geth"
			dbCompactFlags.datadir = tc.datadir
			dbCompactFlags.timeout = tc.timeout
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())

			require.ErrorContains(t, runDBCompact(cmd, nil), tc.want)
		})
	}
}
