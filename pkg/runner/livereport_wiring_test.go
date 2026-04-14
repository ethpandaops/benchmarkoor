package runner

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLiveRunState_ConfigSnapshot verifies the liveRunState SetConfig /
// SnapshotConfig round-trip, which is the mechanism the live reporter uses
// to include the full run config in each ingest payload.
func TestLiveRunState_ConfigSnapshot(t *testing.T) {
	s := &liveRunState{}

	assert.Nil(t, s.SnapshotConfig(), "no config set yet")

	cfg := &RunConfig{
		Timestamp: 1234,
		Status:    "running",
		System:    &SystemInfo{Hostname: "testhost"},
		Instance:  &ResolvedInstance{ID: "inst-1", Client: "geth"},
	}
	s.SetConfig(cfg)

	got := s.SnapshotConfig()
	require.NotNil(t, got, "SnapshotConfig should return bytes after SetConfig")

	// Round-trip through JSON so we verify SetConfig actually serialised the
	// struct with its json tags.
	var back map[string]any
	require.NoError(t, json.Unmarshal(got, &back))
	assert.Equal(t, float64(1234), back["timestamp"])
	assert.Equal(t, "running", back["status"])

	sysInfo, ok := back["system"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "testhost", sysInfo["hostname"])
}
