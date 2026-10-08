package snapshot

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLayoutKeys(t *testing.T) {
	l := Layout{Network: "mainnet", Client: "geth", Block: 26188082}
	require.NoError(t, l.Validate())

	assert.Equal(t, "mainnet/geth/26188082/snapshot.tar.zst", l.ArchiveKey())
	assert.Equal(t, "mainnet/geth/26188082/_snapshot_eth_getBlockByNumber.json", l.HeadBlockKey())
	assert.Equal(t, "mainnet/geth/26188082/_snapshot_metadata.json", l.MetadataKey())
	assert.Equal(t, "mainnet/geth/latest", l.LatestKey())

	l.Prefix, l.Client = "test/", "reth"
	assert.Equal(t, "test/mainnet/reth/26188082/snapshot-v2.tar.zst", l.ArchiveKey())
	assert.Equal(t, "test/mainnet/reth/latest", l.LatestKey())

	l.Client = "erigon"
	assert.Equal(t, "test/mainnet/erigon/26188082/snapshot-pruned.tar.zst", l.ArchiveKey())

	for _, c := range []string{"besu", "ethrex", "nethermind"} {
		l.Client = c
		assert.Equal(t, "test/mainnet/"+c+"/26188082/snapshot.tar.zst", l.ArchiveKey())
	}

	l.Client, l.Archive = "reth", "snapshot.tar.zst"
	assert.Equal(t, "test/mainnet/reth/26188082/snapshot.tar.zst", l.ArchiveKey(), "--archive-name overrides")
}

func TestLayoutValidate(t *testing.T) {
	for name, l := range map[string]Layout{
		"no network":       {Client: "geth"},
		"no client":        {Network: "mainnet"},
		"network with /":   {Network: "main/net", Client: "geth"},
		"client is ..":     {Network: "mainnet", Client: ".."},
		"archive with dir": {Network: "mainnet", Client: "geth", Archive: "x/snapshot.tar.zst"},
	} {
		assert.Error(t, l.Validate(), name)
	}
}
