package eest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertFixture_SinglePayload(t *testing.T) {
	fixture := &Fixture{
		Network: "Prague",
		GenesisBlockHeader: &BlockHeader{
			Hash: "0xgenesis",
		},
		EngineNewPayloads: []*EngineNewPayload{
			{
				ExecutionPayload: &ExecutionPayload{
					ParentHash:    "0xparent1",
					FeeRecipient:  "0xfee",
					StateRoot:     "0xstate",
					ReceiptsRoot:  "0xreceipts",
					LogsBloom:     "0xbloom",
					PrevRandao:    "0xrandao",
					BlockNumber:   "0x1",
					GasLimit:      "0x1000000",
					GasUsed:       "0x0",
					Timestamp:     "0x100",
					ExtraData:     "0x",
					BaseFeePerGas: "0x7",
					BlockHash:     "0xblock1",
					Transactions:  []string{},
				},
				NewPayloadVersion:        4,
				ForkchoiceUpdatedVersion: 3,
				BlobVersionedHashes:      []string{},
				ParentBeaconBlockRoot:    "0xbeacon",
				ExecutionRequests:        []string{},
			},
		},
	}

	result, err := ConvertFixture("test_fixture", fixture)
	require.NoError(t, err)

	assert.Equal(t, "test_fixture", result.Name)
	assert.Equal(t, "0xgenesis", result.GenesisHash)
	assert.Equal(t, "0xblock1", result.FinalHash)
	assert.Equal(t, 1, result.PayloadCount)
	assert.Empty(t, result.SetupLines)
	assert.Len(t, result.TestLines, 2) // newPayload + forkchoiceUpdated

	// Verify first line is engine_newPayloadV4.
	var rpcCall map[string]any
	err = json.Unmarshal([]byte(result.TestLines[0]), &rpcCall)
	require.NoError(t, err)
	assert.Equal(t, "engine_newPayloadV4", rpcCall["method"])

	// Verify second line is engine_forkchoiceUpdatedV3.
	err = json.Unmarshal([]byte(result.TestLines[1]), &rpcCall)
	require.NoError(t, err)
	assert.Equal(t, "engine_forkchoiceUpdatedV3", rpcCall["method"])
}

func TestConvertFixture_MultiplePayloads(t *testing.T) {
	fixture := &Fixture{
		Network: "Prague",
		GenesisBlockHeader: &BlockHeader{
			Hash: "0xgenesis",
		},
		EngineNewPayloads: []*EngineNewPayload{
			{
				ExecutionPayload: &ExecutionPayload{
					ParentHash:    "0xgenesis",
					FeeRecipient:  "0xfee",
					StateRoot:     "0xstate1",
					ReceiptsRoot:  "0xreceipts1",
					LogsBloom:     "0xbloom",
					PrevRandao:    "0xrandao",
					BlockNumber:   "0x1",
					GasLimit:      "0x1000000",
					GasUsed:       "0x0",
					Timestamp:     "0x100",
					ExtraData:     "0x",
					BaseFeePerGas: "0x7",
					BlockHash:     "0xblock1",
					Transactions:  []string{},
				},
				NewPayloadVersion:        3,
				ForkchoiceUpdatedVersion: 3,
				BlobVersionedHashes:      []string{},
				ParentBeaconBlockRoot:    "0xbeacon1",
			},
			{
				ExecutionPayload: &ExecutionPayload{
					ParentHash:    "0xblock1",
					FeeRecipient:  "0xfee",
					StateRoot:     "0xstate2",
					ReceiptsRoot:  "0xreceipts2",
					LogsBloom:     "0xbloom",
					PrevRandao:    "0xrandao",
					BlockNumber:   "0x2",
					GasLimit:      "0x1000000",
					GasUsed:       "0x0",
					Timestamp:     "0x200",
					ExtraData:     "0x",
					BaseFeePerGas: "0x7",
					BlockHash:     "0xblock2",
					Transactions:  []string{},
				},
				NewPayloadVersion:        3,
				ForkchoiceUpdatedVersion: 3,
				BlobVersionedHashes:      []string{},
				ParentBeaconBlockRoot:    "0xbeacon2",
			},
		},
	}

	result, err := ConvertFixture("test_fixture", fixture)
	require.NoError(t, err)

	assert.Equal(t, "test_fixture", result.Name)
	assert.Equal(t, 2, result.PayloadCount)
	assert.Equal(t, "0xblock2", result.FinalHash)

	// First payload becomes setup.
	assert.Len(t, result.SetupLines, 2) // newPayload + forkchoiceUpdated

	// Last payload becomes test.
	assert.Len(t, result.TestLines, 2) // newPayload + forkchoiceUpdated

	// Verify setup uses V3 methods.
	var rpcCall map[string]any
	err = json.Unmarshal([]byte(result.SetupLines[0]), &rpcCall)
	require.NoError(t, err)
	assert.Equal(t, "engine_newPayloadV3", rpcCall["method"])
}

func TestConvertFixture_NilFixture(t *testing.T) {
	_, err := ConvertFixture("test", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fixture is nil")
}

func TestConvertFixture_NoPayloads(t *testing.T) {
	fixture := &Fixture{
		Network:           "Prague",
		EngineNewPayloads: []*EngineNewPayload{},
	}

	_, err := ConvertFixture("test", fixture)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no payloads")
}

func TestConvertFixture_PayloadVersions(t *testing.T) {
	tests := []struct {
		npVersion   int
		fcuVersion  int
		expectedNP  string
		expectedFCU string
	}{
		{1, 1, "engine_newPayloadV1", "engine_forkchoiceUpdatedV1"},
		{2, 1, "engine_newPayloadV2", "engine_forkchoiceUpdatedV1"},
		{3, 3, "engine_newPayloadV3", "engine_forkchoiceUpdatedV3"},
		{4, 3, "engine_newPayloadV4", "engine_forkchoiceUpdatedV3"},
	}

	for _, tc := range tests {
		t.Run(tc.expectedNP, func(t *testing.T) {
			fixture := &Fixture{
				Network: "Test",
				GenesisBlockHeader: &BlockHeader{
					Hash: "0xgenesis",
				},
				EngineNewPayloads: []*EngineNewPayload{
					{
						ExecutionPayload: &ExecutionPayload{
							ParentHash:    "0xparent",
							FeeRecipient:  "0xfee",
							StateRoot:     "0xstate",
							ReceiptsRoot:  "0xreceipts",
							LogsBloom:     "0xbloom",
							PrevRandao:    "0xrandao",
							BlockNumber:   "0x1",
							GasLimit:      "0x1000000",
							GasUsed:       "0x0",
							Timestamp:     "0x100",
							ExtraData:     "0x",
							BaseFeePerGas: "0x7",
							BlockHash:     "0xblock",
							Transactions:  []string{},
						},
						NewPayloadVersion:        tc.npVersion,
						ForkchoiceUpdatedVersion: tc.fcuVersion,
						BlobVersionedHashes:      []string{},
						ParentBeaconBlockRoot:    "0xbeacon",
						ExecutionRequests:        []string{},
					},
				},
			}

			result, err := ConvertFixture("test", fixture)
			require.NoError(t, err)
			require.Len(t, result.TestLines, 2)

			// Check newPayload method.
			var rpcCall map[string]any
			err = json.Unmarshal([]byte(result.TestLines[0]), &rpcCall)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedNP, rpcCall["method"])

			// Check forkchoiceUpdated method.
			err = json.Unmarshal([]byte(result.TestLines[1]), &rpcCall)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedFCU, rpcCall["method"])
		})
	}
}

func TestParseFixtureFile(t *testing.T) {
	jsonData := `{
		"test_one": {
			"network": "Prague",
			"genesisBlockHeader": {
				"hash": "0xgenesis"
			},
			"engineNewPayloads": []
		},
		"test_two": {
			"network": "Prague",
			"genesisBlockHeader": {
				"hash": "0xgenesis2"
			},
			"engineNewPayloads": []
		}
	}`

	fixtures, err := ParseFixtureFile([]byte(jsonData))
	require.NoError(t, err)
	assert.Len(t, fixtures, 2)
	assert.Contains(t, fixtures, "test_one")
	assert.Contains(t, fixtures, "test_two")
}

func TestParseFixtureFile_InvalidJSON(t *testing.T) {
	_, err := ParseFixtureFile([]byte("invalid json"))
	assert.Error(t, err)
}

func TestFixture_IsSupportedFormat(t *testing.T) {
	tests := []struct {
		name     string
		fixture  *Fixture
		expected bool
	}{
		{
			name:     "nil info",
			fixture:  &Fixture{},
			expected: false,
		},
		{
			name: "supported format",
			fixture: &Fixture{
				Info: &FixtureInfo{
					FixtureFormat: "blockchain_test_engine_x",
				},
			},
			expected: true,
		},
		{
			name: "unsupported format",
			fixture: &Fixture{
				Info: &FixtureInfo{
					FixtureFormat: "state_test",
				},
			},
			expected: false,
		},
		{
			name: "empty format",
			fixture: &Fixture{
				Info: &FixtureInfo{
					FixtureFormat: "",
				},
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.fixture.IsSupportedFormat())
		})
	}
}

func TestEngineNewPayload_UnmarshalJSON(t *testing.T) {
	// Test with actual EEST fixture format.
	jsonData := `{
		"newPayloadVersion": "4",
		"forkchoiceUpdatedVersion": "3",
		"params": [
			{
				"parentHash": "0xparent",
				"feeRecipient": "0xfee",
				"stateRoot": "0xstate",
				"receiptsRoot": "0xreceipts",
				"logsBloom": "0xbloom",
				"prevRandao": "0xrandao",
				"blockNumber": "0x1",
				"gasLimit": "0x1000000",
				"gasUsed": "0x0",
				"timestamp": "0x100",
				"extraData": "0x",
				"baseFeePerGas": "0x7",
				"blockHash": "0xblock",
				"transactions": []
			},
			[],
			"0xbeacon",
			[]
		]
	}`

	var payload EngineNewPayload
	err := json.Unmarshal([]byte(jsonData), &payload)
	require.NoError(t, err)

	assert.Equal(t, 4, payload.NewPayloadVersion)
	assert.Equal(t, 3, payload.ForkchoiceUpdatedVersion)
	assert.NotNil(t, payload.ExecutionPayload)
	assert.Equal(t, "0xblock", payload.ExecutionPayload.BlockHash)
	assert.Equal(t, "0xbeacon", payload.ParentBeaconBlockRoot)
	assert.Empty(t, payload.BlobVersionedHashes)
	assert.Empty(t, payload.ExecutionRequests)
}
