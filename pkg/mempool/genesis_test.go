package mempool

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/scenario"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

func TestGenesis(t *testing.T) {
	b := &config.BenchmarkConfig{Mempool: &config.MempoolConfig{Accounts: 64}}
	m := b.GetMempool()

	data, err := genesisJSON(m)
	require.NoError(t, err)

	var g struct {
		Config struct {
			ChainID      uint64          `json:"chainId"`
			OsakaTime    *uint64         `json:"osakaTime"`
			BlobSchedule json.RawMessage `json:"blobSchedule"`
		} `json:"config"`
		GasLimit string                     `json:"gasLimit"`
		Alloc    map[string]json.RawMessage `json:"alloc"`
	}

	require.NoError(t, json.Unmarshal(data, &g))
	assert.Equal(t, uint64(config.DefaultMempoolChainID), g.Config.ChainID)
	require.NotNil(t, g.Config.OsakaTime)
	assert.Contains(t, string(g.Config.BlobSchedule), "osaka")
	assert.Equal(t, "0x1c9c380", g.GasLimit)

	// 64 accounts, 4 system contracts, the burner, the sweeper.
	assert.Len(t, g.Alloc, 64+6)

	lower := make(map[string]bool, len(g.Alloc))
	for k := range g.Alloc {
		lower[k] = true
	}

	for _, addr := range txfactory.Addresses(m.Seed, 64) {
		assert.True(t, lower[addr.Hex()] || lower[lowerHex(addr.Hex())], addr.Hex())
	}

	assert.True(t, lower[lowerHex(scenario.BurnerAddress.Hex())] || lower[scenario.BurnerAddress.Hex()])

	// The same config gives the same bytes (the suite hash depends on it).
	again, err := genesisJSON(m)
	require.NoError(t, err)
	assert.Equal(t, data, again)
}

func lowerHex(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'F' {
			out[i] = c + 'a' - 'A'
		}
	}

	return string(out)
}
