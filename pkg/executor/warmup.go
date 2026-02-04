package executor

import (
	"encoding/json"
	"fmt"
)

// InvalidStateRoot is the zeroed state root used to invalidate engine_newPayload calls
// during warmup execution. This causes the client to execute the block but reject it.
const InvalidStateRoot = "0x0000000000000000000000000000000000000000000000000000000000000000"

// invalidateStateRoot modifies an engine_newPayload request by replacing the stateRoot
// in the execution payload with an invalid (zeroed) value. This causes the client to
// execute the block (priming caches) but return INVALID status.
func invalidateStateRoot(payload string) (string, error) {
	// Parse the JSON-RPC request.
	var req map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return "", fmt.Errorf("parsing request: %w", err)
	}

	// Extract params array.
	paramsRaw, ok := req["params"]
	if !ok {
		return "", fmt.Errorf("missing params field")
	}

	var params []json.RawMessage
	if err := json.Unmarshal(paramsRaw, &params); err != nil {
		return "", fmt.Errorf("parsing params: %w", err)
	}

	if len(params) == 0 {
		return "", fmt.Errorf("empty params array")
	}

	// Parse the execution payload (first param).
	var execPayload map[string]json.RawMessage
	if err := json.Unmarshal(params[0], &execPayload); err != nil {
		return "", fmt.Errorf("parsing execution payload: %w", err)
	}

	// Replace stateRoot with invalid value.
	invalidRoot, err := json.Marshal(InvalidStateRoot)
	if err != nil {
		return "", fmt.Errorf("marshaling invalid state root: %w", err)
	}

	execPayload["stateRoot"] = invalidRoot

	// Marshal modified execution payload.
	modifiedPayload, err := json.Marshal(execPayload)
	if err != nil {
		return "", fmt.Errorf("marshaling modified payload: %w", err)
	}

	// Replace first param with modified payload.
	params[0] = modifiedPayload

	// Marshal params array.
	modifiedParams, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("marshaling modified params: %w", err)
	}

	// Replace params in request.
	req["params"] = modifiedParams

	// Marshal final request.
	result, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshaling final request: %w", err)
	}

	return string(result), nil
}
