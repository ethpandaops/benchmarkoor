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

// buildForkchoiceUpdatedCall builds an engine_forkchoiceUpdatedVX JSON-RPC call.
func buildForkchoiceUpdatedCall(payload *EngineNewPayload, id int) (string, error) {
	// Use the forkchoiceUpdated version from the fixture.
	method := fmt.Sprintf("engine_forkchoiceUpdatedV%d", payload.ForkchoiceUpdatedVersion)

	blockHash := payload.ExecutionPayload.BlockHash

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

	return result
}
