package executor

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildIndexEntryFromData(t *testing.T) {
	t.Run("includes metadata labels", func(t *testing.T) {
		configJSON := `{
			"timestamp": 1700000000,
			"suite_hash": "abc123",
			"instance": {
				"id": "geth-1",
				"client": "geth",
				"image": "ethereum/client-go:latest"
			},
			"metadata": {
				"labels": {
					"env": "production",
					"team": "platform"
				}
			}
		}`

		entry, err := BuildIndexEntryFromData("run-1", []byte(configJSON), nil)
		require.NoError(t, err)

		require.NotNil(t, entry.Metadata)
		assert.Equal(t, "production", entry.Metadata["env"])
		assert.Equal(t, "platform", entry.Metadata["team"])
	})

	t.Run("no metadata when absent", func(t *testing.T) {
		configJSON := `{
			"timestamp": 1700000000,
			"instance": {
				"id": "geth-1",
				"client": "geth",
				"image": "ethereum/client-go:latest"
			}
		}`

		entry, err := BuildIndexEntryFromData("run-1", []byte(configJSON), nil)
		require.NoError(t, err)

		assert.Nil(t, entry.Metadata)
	})

	t.Run("no metadata when labels empty", func(t *testing.T) {
		configJSON := `{
			"timestamp": 1700000000,
			"instance": {
				"id": "geth-1",
				"client": "geth",
				"image": "ethereum/client-go:latest"
			},
			"metadata": {
				"labels": {}
			}
		}`

		entry, err := BuildIndexEntryFromData("run-1", []byte(configJSON), nil)
		require.NoError(t, err)

		assert.Nil(t, entry.Metadata)
	})

	t.Run("metadata omitted from JSON when nil", func(t *testing.T) {
		configJSON := `{
			"timestamp": 1700000000,
			"instance": {
				"id": "geth-1",
				"client": "geth",
				"image": "ethereum/client-go:latest"
			}
		}`

		entry, err := BuildIndexEntryFromData("run-1", []byte(configJSON), nil)
		require.NoError(t, err)

		data, err := json.Marshal(entry)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "metadata")
	})

	t.Run("parses basic fields", func(t *testing.T) {
		configJSON := `{
			"timestamp": 1700000000,
			"timestamp_end": 1700003600,
			"suite_hash": "abc123",
			"status": "completed",
			"instance": {
				"id": "geth-1",
				"client": "geth",
				"image": "ethereum/client-go:latest",
				"rollback_strategy": "rpc-debug-setHead"
			},
			"test_counts": {
				"total": 10,
				"passed": 8,
				"failed": 2
			}
		}`

		entry, err := BuildIndexEntryFromData("run-1", []byte(configJSON), nil)
		require.NoError(t, err)

		assert.Equal(t, "run-1", entry.RunID)
		assert.Equal(t, int64(1700000000), entry.Timestamp)
		assert.Equal(t, int64(1700003600), entry.TimestampEnd)
		assert.Equal(t, "abc123", entry.SuiteHash)
		assert.Equal(t, "completed", entry.Status)
		assert.Equal(t, "geth-1", entry.Instance.ID)
		assert.Equal(t, "geth", entry.Instance.Client)
		assert.Equal(t, 10, entry.Tests.TestsTotal)
		assert.Equal(t, 8, entry.Tests.TestsPassed)
		assert.Equal(t, 2, entry.Tests.TestsFailed)
	})
}
