package executor

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvalidateStateRoot(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		errContains string
		checkOutput func(t *testing.T, output string)
	}{
		{
			name: "valid engine_newPayloadV3 request",
			input: `{
				"jsonrpc": "2.0",
				"method": "engine_newPayloadV3",
				"params": [
					{
						"parentHash": "0x1234",
						"feeRecipient": "0xabcd",
						"stateRoot": "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
						"receiptsRoot": "0x5678",
						"logsBloom": "0x",
						"prevRandao": "0x",
						"blockNumber": "0x1",
						"gasLimit": "0x989680",
						"gasUsed": "0x0",
						"timestamp": "0x60000000",
						"extraData": "0x",
						"baseFeePerGas": "0x7",
						"blockHash": "0xabcdef",
						"transactions": [],
						"withdrawals": [],
						"blobGasUsed": "0x0",
						"excessBlobGas": "0x0"
					},
					["0x01"],
					"0xparentBeaconBlockRoot"
				],
				"id": 1
			}`,
			checkOutput: func(t *testing.T, output string) {
				var req struct {
					Params []json.RawMessage `json:"params"`
				}
				require.NoError(t, json.Unmarshal([]byte(output), &req))
				require.Len(t, req.Params, 3)

				var payload struct {
					StateRoot string `json:"stateRoot"`
				}
				require.NoError(t, json.Unmarshal(req.Params[0], &payload))
				assert.Equal(t, InvalidStateRoot, payload.StateRoot)
			},
		},
		{
			name: "valid engine_newPayloadV2 request",
			input: `{
				"jsonrpc": "2.0",
				"method": "engine_newPayloadV2",
				"params": [{
					"parentHash": "0x1234",
					"stateRoot": "0xoriginalstateroot",
					"blockHash": "0xabcdef"
				}],
				"id": 1
			}`,
			checkOutput: func(t *testing.T, output string) {
				var req struct {
					Params []json.RawMessage `json:"params"`
				}
				require.NoError(t, json.Unmarshal([]byte(output), &req))
				require.Len(t, req.Params, 1)

				var payload struct {
					StateRoot  string `json:"stateRoot"`
					ParentHash string `json:"parentHash"`
				}
				require.NoError(t, json.Unmarshal(req.Params[0], &payload))
				assert.Equal(t, InvalidStateRoot, payload.StateRoot)
				assert.Equal(t, "0x1234", payload.ParentHash)
			},
		},
		{
			name:        "missing params field",
			input:       `{"jsonrpc": "2.0", "method": "engine_newPayloadV3", "id": 1}`,
			wantErr:     true,
			errContains: "missing params field",
		},
		{
			name:        "empty params array",
			input:       `{"jsonrpc": "2.0", "method": "engine_newPayloadV3", "params": [], "id": 1}`,
			wantErr:     true,
			errContains: "empty params array",
		},
		{
			name:        "invalid JSON",
			input:       `{not valid json}`,
			wantErr:     true,
			errContains: "parsing request",
		},
		{
			name:        "params is not an array",
			input:       `{"jsonrpc": "2.0", "method": "engine_newPayloadV3", "params": "not an array", "id": 1}`,
			wantErr:     true,
			errContains: "parsing params",
		},
		{
			name:        "first param is not an object",
			input:       `{"jsonrpc": "2.0", "method": "engine_newPayloadV3", "params": ["not an object"], "id": 1}`,
			wantErr:     true,
			errContains: "parsing execution payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := invalidateStateRoot(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotEmpty(t, output)

			if tt.checkOutput != nil {
				tt.checkOutput(t, output)
			}
		})
	}
}

func TestInvalidStateRootConstant(t *testing.T) {
	// Ensure the constant is the correct length (32 bytes hex encoded with 0x prefix).
	assert.Equal(t, 66, len(InvalidStateRoot))
	assert.Equal(t, "0x", InvalidStateRoot[:2])

	// Ensure it's all zeros.
	expected := "0x" + "0000000000000000000000000000000000000000000000000000000000000000"
	assert.Equal(t, expected, InvalidStateRoot)
}
