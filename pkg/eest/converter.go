package eest

import (
	"encoding/json"
	"fmt"
)

// ConvertedTest represents a test converted from EEST fixture format.
type ConvertedTest struct {
	Name         string
	SetupLines   []string // JSON-RPC calls for setup (all payloads except last)
	TestLines    []string // JSON-RPC calls for test (last payload only)
	GenesisHash  string   // Genesis block hash for forkchoiceUpdated calls
	FinalHash    string   // Final block hash after all payloads
	PayloadCount int      // Total number of payloads in the fixture
}

// ConvertFixture converts an EEST fixture to JSON-RPC calls.
// For each payload:
//  1. engine_newPayloadV{version}(params)
//  2. engine_forkchoiceUpdatedV{version}({headBlockHash, safeBlockHash, finalizedBlockHash}, null)
//
// All payloads except the last become setup steps.
// The last payload becomes the test step.
func ConvertFixture(name string, fixture *Fixture) (*ConvertedTest, error) {
	if fixture == nil {
		return nil, fmt.Errorf("fixture is nil")
	}

	if len(fixture.EngineNewPayloads) == 0 {
		return nil, fmt.Errorf("fixture has no payloads")
	}

	result := &ConvertedTest{
		Name:         name,
		SetupLines:   make([]string, 0),
		TestLines:    make([]string, 0),
		PayloadCount: len(fixture.EngineNewPayloads),
	}

	// Get genesis hash for reference.
	if fixture.GenesisBlockHeader != nil {
		result.GenesisHash = fixture.GenesisBlockHeader.Hash
	}

	// Process payloads.
	for i, payload := range fixture.EngineNewPayloads {
		isLastPayload := i == len(fixture.EngineNewPayloads)-1

		lines, err := convertPayload(payload, i+1)
		if err != nil {
			return nil, fmt.Errorf("converting payload %d: %w", i, err)
		}

		if isLastPayload {
			result.TestLines = append(result.TestLines, lines...)
			result.FinalHash = payload.ExecutionPayload.BlockHash
		} else {
			result.SetupLines = append(result.SetupLines, lines...)
		}
	}

	return result, nil
}

// ConvertStatefulFixture converts a stateful-engine fixture to JSON-RPC calls.
// Replay boots from a snapshot datadir rather than a genesis, so the setup
// phase is the shared pre_run payloads (snapshot → start block, preRun may be
// nil) followed by the fixture's own setupEngineNewPayloads (start block →
// per-test pre-state). The fixture's engineNewPayloads (the benchmark block)
// become the measured test step. Each payload emits an engine_newPayload +
// engine_forkchoiceUpdated pair, preceded by one forkchoiceUpdated returning
// the head to the block the fixture starts from.
func ConvertStatefulFixture(name string, fixture *Fixture, preRun *StatefulPreRun) (*ConvertedTest, error) {
	if fixture == nil {
		return nil, fmt.Errorf("fixture is nil")
	}

	if len(fixture.EngineNewPayloads) == 0 {
		return nil, fmt.Errorf("fixture has no benchmark payloads")
	}

	// Setup = shared pre_run payloads, then the fixture's own setup payloads.
	setupPayloads := make([]*EngineNewPayload, 0,
		len(fixture.SetupEngineNewPayloads)+preRunPayloadCount(preRun))

	if preRun != nil {
		setupPayloads = append(setupPayloads, preRun.EngineNewPayloads...)
	}

	setupPayloads = append(setupPayloads, fixture.SetupEngineNewPayloads...)

	result := &ConvertedTest{
		Name:         name,
		SetupLines:   make([]string, 0, len(setupPayloads)*2),
		TestLines:    make([]string, 0, len(fixture.EngineNewPayloads)*2),
		GenesisHash:  fixture.SnapshotBlockHash,
		PayloadCount: len(setupPayloads) + len(fixture.EngineNewPayloads),
	}

	// Every fixture replays from the same anchor, but the previous test leaves
	// the head wherever its last payload landed and nothing puts it back. Ask
	// for the anchor rather than assuming we are on it: otherwise the first
	// newPayload names a parent whose state the client may no longer hold and
	// is answered ACCEPTED, orphaning every payload after it.
	//
	// Sent unconditionally — when the head already matches, the client says so
	// for free, which beats tracking the head across tests.
	anchorLine, err := buildAnchorForkchoiceCall(setupPayloads, fixture.EngineNewPayloads)
	if err != nil {
		return nil, fmt.Errorf("building anchor forkchoiceUpdated call: %w", err)
	}

	result.SetupLines = append(result.SetupLines, anchorLine)

	for i, payload := range setupPayloads {
		lines, err := convertPayload(payload, i+1)
		if err != nil {
			return nil, fmt.Errorf("converting setup payload %d: %w", i, err)
		}

		result.SetupLines = append(result.SetupLines, lines...)
	}

	for i, payload := range fixture.EngineNewPayloads {
		lines, err := convertPayload(payload, len(setupPayloads)+i+1)
		if err != nil {
			return nil, fmt.Errorf("converting benchmark payload %d: %w", i, err)
		}

		result.TestLines = append(result.TestLines, lines...)
		result.FinalHash = payload.ExecutionPayload.BlockHash
	}

	return result, nil
}

// preRunPayloadCount returns the number of pre_run payloads, tolerating a nil
// pre_run (capacity hint only).
func preRunPayloadCount(preRun *StatefulPreRun) int {
	if preRun == nil {
		return 0
	}

	return len(preRun.EngineNewPayloads)
}

// convertPayload generates JSON-RPC lines for a single payload.
func convertPayload(payload *EngineNewPayload, id int) ([]string, error) {
	if payload.ExecutionPayload == nil {
		return nil, fmt.Errorf("execution payload is nil")
	}

	var lines []string

	// Generate engine_newPayloadVX call.
	newPayloadLine, err := buildNewPayloadCall(payload, id)
	if err != nil {
		return nil, fmt.Errorf("building newPayload call: %w", err)
	}

	lines = append(lines, newPayloadLine)

	// Generate engine_forkchoiceUpdatedVX call.
	fcuLine, err := buildForkchoiceUpdatedCall(payload, id)
	if err != nil {
		return nil, fmt.Errorf("building forkchoiceUpdated call: %w", err)
	}

	lines = append(lines, fcuLine)

	return lines, nil
}

// buildNewPayloadCall builds an engine_newPayloadVX JSON-RPC call.
func buildNewPayloadCall(payload *EngineNewPayload, id int) (string, error) {
	method := fmt.Sprintf("engine_newPayloadV%d", payload.NewPayloadVersion)

	// Build execution payload for JSON-RPC (convert field names to match spec).
	execPayload := buildExecutionPayloadJSON(payload.ExecutionPayload, payload.NewPayloadVersion)

	// Build params based on version.
	var params []any

	switch payload.NewPayloadVersion {
	case 1:
		params = []any{execPayload}
	case 2:
		params = []any{execPayload}
	case 3:
		// V3: executionPayload, expectedBlobVersionedHashes, parentBeaconBlockRoot
		params = []any{
			execPayload,
			payload.BlobVersionedHashes,
			payload.ParentBeaconBlockRoot,
		}
	case 4:
		// V4: executionPayload, expectedBlobVersionedHashes, parentBeaconBlockRoot, executionRequests
		params = []any{
			execPayload,
			payload.BlobVersionedHashes,
			payload.ParentBeaconBlockRoot,
			payload.ExecutionRequests,
		}
	case 5:
		// V5 (Amsterdam): executionPayload, expectedBlobVersionedHashes, parentBeaconBlockRoot, executionRequests
		params = []any{
			execPayload,
			payload.BlobVersionedHashes,
			payload.ParentBeaconBlockRoot,
			payload.ExecutionRequests,
		}
	default:
		return "", fmt.Errorf("unsupported payload version: %d", payload.NewPayloadVersion)
	}

	rpcCall := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      id,
	}

	data, err := json.Marshal(rpcCall)
	if err != nil {
		return "", fmt.Errorf("marshaling JSON-RPC call: %w", err)
	}

	return string(data), nil
}

// ZeroHash is the zero hash used for forkchoice state.
const ZeroHash = "0x0000000000000000000000000000000000000000000000000000000000000000"

// buildAnchorForkchoiceCall returns the chain head to the block this fixture
// replays from: the parent of its first payload. Taken from the payload rather
// than startBlockHash so it stays correct when a pre_run is prepended, whose
// first payload descends from the snapshot block instead.
func buildAnchorForkchoiceCall(setupPayloads, benchmarkPayloads []*EngineNewPayload) (string, error) {
	payloads := setupPayloads
	if len(payloads) == 0 {
		payloads = benchmarkPayloads
	}

	if len(payloads) == 0 || payloads[0].ExecutionPayload == nil {
		return "", fmt.Errorf("no payload to derive the anchor from")
	}

	anchor := payloads[0].ExecutionPayload.ParentHash
	if anchor == "" {
		return "", fmt.Errorf("first payload has no parentHash")
	}

	// id 0 keeps the payload calls numbered from 1 as before.
	return buildForkchoiceUpdatedCallForHash(anchor, payloads[0].ForkchoiceUpdatedVersion, 0)
}

// buildForkchoiceUpdatedCall builds an engine_forkchoiceUpdatedVX JSON-RPC call
// setting the head to the payload's own block.
func buildForkchoiceUpdatedCall(payload *EngineNewPayload, id int) (string, error) {
	return buildForkchoiceUpdatedCallForHash(
		payload.ExecutionPayload.BlockHash, payload.ForkchoiceUpdatedVersion, id)
}

// buildForkchoiceUpdatedCallForHash builds an engine_forkchoiceUpdatedVX call
// setting the head to blockHash.
//
// safe and finalized stay zero, which clients read as "no update" — replaying a
// payload should not move either marker. Note the engine API only permits the
// zero hash "unless transition block is finalized", which these mainnet-fork
// datadirs do not satisfy; sending a real ancestor here is worth revisiting.
func buildForkchoiceUpdatedCallForHash(blockHash string, version, id int) (string, error) {
	method := fmt.Sprintf("engine_forkchoiceUpdatedV%d", version)

	forkchoiceState := map[string]string{
		"headBlockHash":      blockHash,
		"safeBlockHash":      ZeroHash,
		"finalizedBlockHash": ZeroHash,
	}

	// Second param is null (no payload attributes).
	rpcCall := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  []any{forkchoiceState, nil},
		"id":      id,
	}

	data, err := json.Marshal(rpcCall)
	if err != nil {
		return "", fmt.Errorf("marshaling JSON-RPC call: %w", err)
	}

	return string(data), nil
}

// buildExecutionPayloadJSON converts ExecutionPayload to the JSON-RPC format.
func buildExecutionPayloadJSON(ep *ExecutionPayload, version int) map[string]any {
	result := map[string]any{
		"parentHash":    ep.ParentHash,
		"feeRecipient":  ep.FeeRecipient,
		"stateRoot":     ep.StateRoot,
		"receiptsRoot":  ep.ReceiptsRoot,
		"logsBloom":     ep.LogsBloom,
		"prevRandao":    ep.PrevRandao,
		"blockNumber":   ep.BlockNumber,
		"gasLimit":      ep.GasLimit,
		"gasUsed":       ep.GasUsed,
		"timestamp":     ep.Timestamp,
		"extraData":     ep.ExtraData,
		"baseFeePerGas": ep.BaseFeePerGas,
		"blockHash":     ep.BlockHash,
		"transactions":  ep.Transactions,
	}

	// Add withdrawals for V2+.
	if version >= 2 && ep.Withdrawals != nil {
		withdrawals := make([]map[string]string, len(ep.Withdrawals))
		for i, w := range ep.Withdrawals {
			withdrawals[i] = map[string]string{
				"index":          w.Index,
				"validatorIndex": w.ValidatorIndex,
				"address":        w.Address,
				"amount":         w.Amount,
			}
		}

		result["withdrawals"] = withdrawals
	}

	// Add blob gas fields for V3+.
	if version >= 3 {
		if ep.BlobGasUsed != "" {
			result["blobGasUsed"] = ep.BlobGasUsed
		}

		if ep.ExcessBlobGas != "" {
			result["excessBlobGas"] = ep.ExcessBlobGas
		}
	}

	// Note: V4 deposit/withdrawal/consolidation requests are passed as executionRequests
	// parameter, not in the payload itself.

	// Add Amsterdam fields for V5+.
	if version >= 5 {
		if ep.BlockAccessList != "" {
			result["blockAccessList"] = ep.BlockAccessList
		}

		if ep.SlotNumber != "" {
			result["slotNumber"] = ep.SlotNumber
		}
	}

	return result
}
