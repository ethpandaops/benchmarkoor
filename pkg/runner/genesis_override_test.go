package runner

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

func TestApplyGenesisForkOverrides(t *testing.T) {
	t.Run("no overrides returns input unchanged", func(t *testing.T) {
		in := []byte(`{"config":{"osakaTime":0}}`)

		out, err := applyGenesisForkOverrides(in, nil)

		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("non-geth genesis errors", func(t *testing.T) {
		// Parity-format chainspec has params, not config.
		in := []byte(`{"params":{"eip7825TransitionTimestamp":"0x0"}}`)

		_, err := applyGenesisForkOverrides(in, map[string]uint64{"amsterdam": 1})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "geth-format")
	})

	t.Run("sets fork time and inherits blob schedule", func(t *testing.T) {
		in := []byte(`{
			"config": {
				"chainId": 1337,
				"osakaTime": 0,
				"blobSchedule": {
					"osaka": {"baseFeeUpdateFraction": 5007716, "max": 12, "target": 9}
				}
			},
			"gasLimit": "0x11e1a300"
		}`)

		out, err := applyGenesisForkOverrides(in, map[string]uint64{"amsterdam": 1})
		require.NoError(t, err)

		cfg := decodeConfig(t, out)
		assert.EqualValues(t, 1, cfg["amsterdamTime"])

		bs, ok := cfg["blobSchedule"].(map[string]any)
		require.True(t, ok)

		amsterdam, ok := bs["amsterdam"].(map[string]any)
		require.True(t, ok, "amsterdam should inherit osaka's schedule")
		assert.EqualValues(t, 12, amsterdam["max"])
		assert.EqualValues(t, 9, amsterdam["target"])

		// Untouched top-level fields survive.
		var top map[string]any
		require.NoError(t, json.Unmarshal(out, &top))
		assert.Equal(t, "0x11e1a300", top["gasLimit"])
	})

	t.Run("inherits latest preceding fork when several exist", func(t *testing.T) {
		in := []byte(`{"config":{"blobSchedule":{
			"cancun":{"max":6},
			"prague":{"max":9},
			"osaka":{"max":12}
		}}}`)

		out, err := applyGenesisForkOverrides(in, map[string]uint64{"amsterdam": 1})
		require.NoError(t, err)

		bs := decodeConfig(t, out)["blobSchedule"].(map[string]any)
		amsterdam := bs["amsterdam"].(map[string]any)
		assert.EqualValues(t, 12, amsterdam["max"], "should inherit osaka, the latest")
	})

	t.Run("does not overwrite an existing fork schedule", func(t *testing.T) {
		in := []byte(`{"config":{"blobSchedule":{
			"osaka":{"max":12},
			"amsterdam":{"max":99}
		}}}`)

		out, err := applyGenesisForkOverrides(in, map[string]uint64{"amsterdam": 1})
		require.NoError(t, err)

		bs := decodeConfig(t, out)["blobSchedule"].(map[string]any)
		amsterdam := bs["amsterdam"].(map[string]any)
		assert.EqualValues(t, 99, amsterdam["max"])
	})

	t.Run("no blob schedule only sets the time", func(t *testing.T) {
		in := []byte(`{"config":{"osakaTime":0}}`)

		out, err := applyGenesisForkOverrides(in, map[string]uint64{"amsterdam": 1})
		require.NoError(t, err)

		cfg := decodeConfig(t, out)
		assert.EqualValues(t, 1, cfg["amsterdamTime"])
		_, hasBlob := cfg["blobSchedule"]
		assert.False(t, hasBlob)
	})

	t.Run("preserves large integers without float corruption", func(t *testing.T) {
		// A value beyond float64's exact-integer range must round-trip exactly.
		in := []byte(`{"config":{"terminalTotalDifficulty":115792089237316195423570985008687907853269984665640564039457584007913129639936}}`)

		out, err := applyGenesisForkOverrides(in, map[string]uint64{"amsterdam": 1})
		require.NoError(t, err)

		assert.Contains(t, string(out),
			"115792089237316195423570985008687907853269984665640564039457584007913129639936")
	})
}

func TestApplyGenesisEIPOverrides(t *testing.T) {
	t.Run("nil or empty override returns input unchanged", func(t *testing.T) {
		in := []byte(`{"params":{"eip7825TransitionTimestamp":"0x0"}}`)

		out, err := applyGenesisEIPOverrides(in, nil)
		require.NoError(t, err)
		assert.Equal(t, in, out)

		out, err = applyGenesisEIPOverrides(in, &config.GenesisEIPOverride{Timestamp: 1})
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("non-parity genesis errors", func(t *testing.T) {
		in := []byte(`{"config":{"osakaTime":0}}`)

		_, err := applyGenesisEIPOverrides(in, &config.GenesisEIPOverride{
			Timestamp: 1, EIPs: []uint64{7928},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "parity")
	})

	t.Run("sets eip transition timestamps as hex", func(t *testing.T) {
		in := []byte(`{"params":{"eip7825TransitionTimestamp":"0x0"},"name":"x"}`)

		out, err := applyGenesisEIPOverrides(in, &config.GenesisEIPOverride{
			Timestamp: 1, EIPs: []uint64{7928, 8037},
		})
		require.NoError(t, err)

		params := decodeParams(t, out)
		assert.Equal(t, "0x1", params["eip7928TransitionTimestamp"])
		assert.Equal(t, "0x1", params["eip8037TransitionTimestamp"])
		// Pre-existing params survive.
		assert.Equal(t, "0x0", params["eip7825TransitionTimestamp"])

		var top map[string]any
		require.NoError(t, json.Unmarshal(out, &top))
		assert.Equal(t, "x", top["name"])
	})

	t.Run("encodes larger timestamps as hex", func(t *testing.T) {
		in := []byte(`{"params":{}}`)

		out, err := applyGenesisEIPOverrides(in, &config.GenesisEIPOverride{
			Timestamp: 1769856767, EIPs: []uint64{7928},
		})
		require.NoError(t, err)

		params := decodeParams(t, out)
		assert.Equal(t, "0x697ddeff", params["eip7928TransitionTimestamp"])
	})
}

func decodeParams(t *testing.T, genesis []byte) map[string]any {
	t.Helper()

	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(genesis, &top))

	var params map[string]any
	require.NoError(t, json.Unmarshal(top["params"], &params))

	return params
}

func decodeConfig(t *testing.T, genesis []byte) map[string]any {
	t.Helper()

	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(genesis, &top))

	var cfg map[string]any
	require.NoError(t, json.Unmarshal(top["config"], &cfg))

	return cfg
}
