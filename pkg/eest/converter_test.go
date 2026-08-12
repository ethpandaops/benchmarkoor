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
		{5, 4, "engine_newPayloadV5", "engine_forkchoiceUpdatedV4"},
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

// statefulPayload builds a minimal EngineNewPayload for stateful conversion
// tests, with the given block number/hash/parent.
func statefulPayload(number, hash, parent string) *EngineNewPayload {
	return &EngineNewPayload{
		ExecutionPayload: &ExecutionPayload{
			ParentHash:    parent,
			FeeRecipient:  "0xfee",
			StateRoot:     "0xstate",
			ReceiptsRoot:  "0xreceipts",
			LogsBloom:     "0xbloom",
			PrevRandao:    "0xrandao",
			BlockNumber:   number,
			GasLimit:      "0x1000000",
			GasUsed:       "0x0",
			Timestamp:     "0x100",
			ExtraData:     "0x",
			BaseFeePerGas: "0x7",
			BlockHash:     hash,
			Transactions:  []string{},
		},
		NewPayloadVersion:        4,
		ForkchoiceUpdatedVersion: 3,
		BlobVersionedHashes:      []string{},
		ParentBeaconBlockRoot:    "0xbeacon",
		ExecutionRequests:        []string{},
	}
}

func TestConvertStatefulFixture(t *testing.T) {
	preRun := &StatefulPreRun{
		SnapshotBlockHash: "0xsnapshot",
		StartBlockHash:    "0xstart",
		// snapshot (0x0) -> start (0x3): three pre_run blocks.
		EngineNewPayloads: []*EngineNewPayload{
			statefulPayload("0x1", "0xb1", "0xsnapshot"),
			statefulPayload("0x2", "0xb2", "0xb1"),
			statefulPayload("0x3", "0xstart", "0xb2"),
		},
	}

	fixture := &Fixture{
		Info:              &FixtureInfo{FixtureFormat: SupportedStatefulFixtureFormat},
		Network:           "Osaka",
		SnapshotBlockHash: "0xsnapshot",
		StartBlockHash:    "0xstart",
		LastBlockHash:     "0xbench",
		// start (0x3) -> setup (0x4).
		SetupEngineNewPayloads: []*EngineNewPayload{
			statefulPayload("0x4", "0xsetup", "0xstart"),
		},
		// setup (0x4) -> benchmark (0x5): the measured block.
		EngineNewPayloads: []*EngineNewPayload{
			statefulPayload("0x5", "0xbench", "0xsetup"),
		},
	}

	result, err := ConvertStatefulFixture("test_stateful", fixture, preRun)
	require.NoError(t, err)

	assert.Equal(t, "test_stateful", result.Name)
	// GenesisHash carries the snapshot hash for reporting.
	assert.Equal(t, "0xsnapshot", result.GenesisHash)
	assert.Equal(t, "0xbench", result.FinalHash)
	// 3 pre_run + 1 setup + 1 benchmark = 5 payloads.
	assert.Equal(t, 5, result.PayloadCount)
	// Setup = 1 anchor fcU + (3 pre_run + 1 setup) * 2 lines (newPayload + fcU).
	assert.Len(t, result.SetupLines, 9)
	// Test = 1 benchmark * 2 lines.
	assert.Len(t, result.TestLines, 2)

	// The anchor forkchoiceUpdated leads, pointing at the parent of the first
	// payload. With a pre_run prepended that is the snapshot block, not the
	// fixture's startBlockHash.
	assert.Equal(t, "engine_forkchoiceUpdatedV3", rpcMethod(t, result.SetupLines[0]))
	assert.Equal(t, "0xsnapshot", forkchoiceHeadHash(t, result.SetupLines[0]),
		"the anchor must be the parent of the first pre_run block")

	// Ordering by CONTENT (not just method name): the shared pre_run blocks must
	// precede the fixture's own setup block. After the anchor line, SetupLines are
	// (newPayload, fcU) pairs, so newPayload lines sit at odd indices:
	// [1]=pre_run#1, [3]=pre_run#2, [5]=pre_run#3 (start), [7]=setup.
	assert.Equal(t, "engine_newPayloadV4", rpcMethod(t, result.SetupLines[1]))
	assert.Equal(t, "0xb1", newPayloadBlockHash(t, result.SetupLines[1]),
		"first setup line must replay the first pre_run block, not the fixture's setup")
	assert.Equal(t, "0xstart", newPayloadBlockHash(t, result.SetupLines[5]),
		"third newPayload is the last pre_run block (start)")
	assert.Equal(t, "0xsetup", newPayloadBlockHash(t, result.SetupLines[7]),
		"the fixture's own setup block comes AFTER the pre_run blocks")

	// The benchmark newPayload is the test step.
	assert.Equal(t, "engine_newPayloadV4", rpcMethod(t, result.TestLines[0]))
	assert.Equal(t, "0xbench", newPayloadBlockHash(t, result.TestLines[0]))
}

// rpcMethod decodes a JSON-RPC line and returns its "method".
func rpcMethod(t *testing.T, line string) string {
	t.Helper()

	var call map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &call))

	method, _ := call["method"].(string)

	return method
}

// newPayloadBlockHash decodes an engine_newPayloadVX line and returns the
// execution payload's blockHash (params[0].blockHash).
func newPayloadBlockHash(t *testing.T, line string) string {
	t.Helper()

	var call map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &call))

	params, ok := call["params"].([]any)
	require.True(t, ok && len(params) > 0, "newPayload line must carry params")

	payload, ok := params[0].(map[string]any)
	require.True(t, ok, "first param must be the execution payload object")

	hash, _ := payload["blockHash"].(string)

	return hash
}

// forkchoiceHeadHash decodes a forkchoiceUpdated line and returns its
// headBlockHash.
func forkchoiceHeadHash(t *testing.T, line string) string {
	t.Helper()

	var call map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &call))

	params, ok := call["params"].([]any)
	require.True(t, ok && len(params) > 0, "forkchoiceUpdated line must carry params")

	state, ok := params[0].(map[string]any)
	require.True(t, ok, "first param must be the forkchoice state object")

	hash, _ := state["headBlockHash"].(string)

	return hash
}

func TestConvertStatefulFixture_NilPreRun(t *testing.T) {
	fixture := &Fixture{
		Info:                   &FixtureInfo{FixtureFormat: SupportedStatefulFixtureFormat},
		SnapshotBlockHash:      "0xsnapshot",
		SetupEngineNewPayloads: []*EngineNewPayload{statefulPayload("0x4", "0xsetup", "0xstart")},
		EngineNewPayloads:      []*EngineNewPayload{statefulPayload("0x5", "0xbench", "0xsetup")},
	}

	result, err := ConvertStatefulFixture("test_stateful", fixture, nil)
	require.NoError(t, err)

	// Without pre_run: 1 anchor fcU + the fixture's own setup payload's 2 lines.
	assert.Len(t, result.SetupLines, 3)
	assert.Len(t, result.TestLines, 2)
	assert.Equal(t, 2, result.PayloadCount)

	// With no pre_run the anchor is the fixture's start block.
	assert.Equal(t, "0xstart", forkchoiceHeadHash(t, result.SetupLines[0]))
}

// Every fixture replays from the same anchor, but nothing rewinds the client
// between tests, so the replay has to ask for the anchor itself. Without this
// line the first newPayload names a parent that is not the head, the client
// answers ACCEPTED, and every later payload is orphaned.
func TestConvertStatefulFixture_AnchorForkchoicePrecedesReplay(t *testing.T) {
	fixture := &Fixture{
		Info:                   &FixtureInfo{FixtureFormat: SupportedStatefulFixtureFormat},
		SnapshotBlockHash:      "0xsnapshot",
		StartBlockHash:         "0xstart",
		SetupEngineNewPayloads: []*EngineNewPayload{statefulPayload("0x4", "0xsetup", "0xstart")},
		EngineNewPayloads:      []*EngineNewPayload{statefulPayload("0x5", "0xbench", "0xsetup")},
	}

	result, err := ConvertStatefulFixture("test_stateful", fixture, nil)
	require.NoError(t, err)

	require.NotEmpty(t, result.SetupLines)

	assert.Equal(t, "engine_forkchoiceUpdatedV3", rpcMethod(t, result.SetupLines[0]),
		"the replay must open with a forkchoiceUpdated, not a newPayload")
	assert.Equal(t, "0xstart", forkchoiceHeadHash(t, result.SetupLines[0]),
		"it must point at the parent of the first payload")

	// safe and finalized stay zero: replaying a payload must not move either
	// marker.
	var call map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.SetupLines[0]), &call))
	state, _ := call["params"].([]any)[0].(map[string]any)
	assert.Equal(t, ZeroHash, state["safeBlockHash"])
	assert.Equal(t, ZeroHash, state["finalizedBlockHash"])
}

// With a pre_run prepended the anchor must come from the first payload actually
// replayed, which descends from the snapshot block, not startBlockHash.
func TestConvertStatefulFixture_AnchorIsFirstPayloadParent(t *testing.T) {
	preRun := &StatefulPreRun{
		EngineNewPayloads: []*EngineNewPayload{
			statefulPayload("0x1", "0xb1", "0xsnapshot"),
			statefulPayload("0x2", "0xstart", "0xb1"),
		},
	}

	fixture := &Fixture{
		Info:                   &FixtureInfo{FixtureFormat: SupportedStatefulFixtureFormat},
		SnapshotBlockHash:      "0xsnapshot",
		StartBlockHash:         "0xstart",
		SetupEngineNewPayloads: []*EngineNewPayload{statefulPayload("0x4", "0xsetup", "0xstart")},
		EngineNewPayloads:      []*EngineNewPayload{statefulPayload("0x5", "0xbench", "0xsetup")},
	}

	result, err := ConvertStatefulFixture("test_stateful", fixture, preRun)
	require.NoError(t, err)

	assert.Equal(t, "0xsnapshot", forkchoiceHeadHash(t, result.SetupLines[0]),
		"with a pre_run the anchor is the snapshot block, not startBlockHash")
}

func TestConvertStatefulFixture_NoBenchmarkPayloads(t *testing.T) {
	fixture := &Fixture{
		Info:                   &FixtureInfo{FixtureFormat: SupportedStatefulFixtureFormat},
		SetupEngineNewPayloads: []*EngineNewPayload{statefulPayload("0x4", "0xsetup", "0xstart")},
		EngineNewPayloads:      []*EngineNewPayload{},
	}

	_, err := ConvertStatefulFixture("test", fixture, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no benchmark payloads")
}

func TestConvertStatefulFixture_NilFixture(t *testing.T) {
	_, err := ConvertStatefulFixture("test", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fixture is nil")
}

func TestParsePreRunFile(t *testing.T) {
	jsonData := `{
		"network": "Osaka",
		"snapshotBlockHash": "0xsnapshot",
		"startBlockHash": "0xstart",
		"engineNewPayloads": [
			{
				"newPayloadVersion": "4",
				"forkchoiceUpdatedVersion": "3",
				"params": [
					{"parentHash":"0xsnapshot","feeRecipient":"0xfee","stateRoot":"0xs",
					 "receiptsRoot":"0xr","logsBloom":"0xb","prevRandao":"0xrd",
					 "blockNumber":"0x1","gasLimit":"0x1000000","gasUsed":"0x0",
					 "timestamp":"0x100","extraData":"0x","baseFeePerGas":"0x7",
					 "blockHash":"0xstart","transactions":[]},
					[], "0xbeacon", []
				]
			}
		]
	}`

	preRun, err := ParsePreRunFile([]byte(jsonData))
	require.NoError(t, err)
	assert.Equal(t, "0xstart", preRun.StartBlockHash)
	assert.Equal(t, "0xsnapshot", preRun.SnapshotBlockHash)
	require.Len(t, preRun.EngineNewPayloads, 1)
	assert.Equal(t, 4, preRun.EngineNewPayloads[0].NewPayloadVersion)
}

func TestFixture_IsStateful(t *testing.T) {
	stateful := &Fixture{Info: &FixtureInfo{FixtureFormat: SupportedStatefulFixtureFormat}}
	assert.True(t, stateful.IsStateful())
	assert.True(t, stateful.IsSupportedFormat())

	genesisBased := &Fixture{Info: &FixtureInfo{FixtureFormat: SupportedFixtureFormat}}
	assert.False(t, genesisBased.IsStateful())
	assert.True(t, genesisBased.IsSupportedFormat())
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
