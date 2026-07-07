package builder

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHumanizeBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2.0 KiB"},
		{5 * 1024 * 1024, "5.0 MiB"},
		{3 * 1024 * 1024 * 1024, "3.0 GiB"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, humanizeBytes(tt.bytes))
	}
}

func TestLogDatadirProgress(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "chaindata"), make([]byte, 4096), 0o644))

	logger, hook := test.NewNullLogger()

	stop := logDatadirProgress(context.Background(), logger, dir, 5*time.Millisecond)

	require.Eventually(t, func() bool {
		return len(hook.AllEntries()) > 0
	}, 2*time.Second, 5*time.Millisecond, "should log at least one progress line")

	stop() // must return promptly, not hang

	entries := hook.AllEntries()
	require.NotEmpty(t, entries)
	assert.Contains(t, entries[0].Message, "datadir")

	size, ok := entries[0].Data["datadir_size"].(string)
	require.True(t, ok, "progress line carries a datadir_size string field")
	assert.Contains(t, size, "KiB", "4096 bytes should render as KiB")
}
