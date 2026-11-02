package client

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGethMempoolCommand(t *testing.T) {
	cmd, err := MempoolCommandFor(NewGethSpec(), MempoolSettings{
		Limits: PoolLimits{
			AccountSlots: 16, GlobalSlots: 5120, AccountQueue: 64, GlobalQueue: 1024,
			PriceBump: 10, BlobPriceBump: 100,
		},
		GasLimit: 30_000_000,
		P2PPort:  DefaultMempoolP2PPort,
	})
	require.NoError(t, err)

	for _, want := range []string{
		"--port=30303", "--maxpeers=50", "--nodiscover",
		"--http.api=admin,debug,web3,eth,net,txpool",
		"--txpool.accountslots=16", "--txpool.pricebump=10", "--blobpool.pricebump=100",
		"--miner.gaslimit=30000000", "--txpool.nolocals",
	} {
		assert.Contains(t, cmd, want)
	}

	assert.NotContains(t, cmd, "--maxpeers=0")
	assert.NotContains(t, cmd, "--port=0")

	// The replay command does not change.
	assert.Contains(t, NewGethSpec().DefaultCommand(), "--maxpeers=0")
}

func TestMempoolProfileMissing(t *testing.T) {
	assert.False(t, SupportsMempool(NewErigonSpec()))

	_, err := MempoolCommandFor(NewErigonSpec(), MempoolSettings{})
	require.Error(t, err)
}

func TestReplaceFlags(t *testing.T) {
	got := replaceFlags([]string{"--a=1", "--b", "--c=3"}, map[string]string{
		"--a=": "--a=9",
		"--z=": "--z=1",
		"--y=": "--y=2",
	})

	assert.Equal(t, []string{"--a=9", "--b", "--c=3", "--y=2", "--z=1"}, got)
	assert.True(t, slices.IsSorted(got[3:]))
}
