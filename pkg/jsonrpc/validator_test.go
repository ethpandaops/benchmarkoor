package jsonrpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorValidator_Validate(t *testing.T) {
	validator := &ErrorValidator{}

	tests := []struct {
		name     string
		response string
		wantErr  bool
	}{
		{
			name:     "no error",
			response: `{"jsonrpc":"2.0","id":1,"result":{"status":"VALID"}}`,
			wantErr:  false,
		},
		{
			name:     "has error",
			response: `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := Parse(tt.response)
			require.NoError(t, err)

			err = validator.Validate("engine_newPayloadV3", resp)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "JSON-RPC error")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewPayloadValidator_Validate(t *testing.T) {
	validator := &NewPayloadValidator{}

	tests := []struct {
		name     string
		method   string
		response string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "valid status",
			method:   "engine_newPayloadV3",
			response: `{"jsonrpc":"2.0","id":1,"result":{"status":"VALID","latestValidHash":"0x123"}}`,
			wantErr:  false,
		},
		{
			name:     "syncing status",
			method:   "engine_newPayloadV3",
			response: `{"jsonrpc":"2.0","id":1,"result":{"status":"SYNCING"}}`,
			wantErr:  true,
			errMsg:   "SYNCING",
		},
		{
			name:     "invalid status",
			method:   "engine_newPayloadV3",
			response: `{"jsonrpc":"2.0","id":1,"result":{"status":"INVALID","validationError":"bad block"}}`,
			wantErr:  true,
			errMsg:   "bad block",
		},
		{
			name:     "accepted status",
			method:   "engine_newPayloadV2",
			response: `{"jsonrpc":"2.0","id":1,"result":{"status":"ACCEPTED"}}`,
			wantErr:  true,
			errMsg:   "ACCEPTED",
		},
		{
			name:     "non-newPayload method passes",
			method:   "engine_forkchoiceUpdatedV3",
			response: `{"jsonrpc":"2.0","id":1,"result":{"status":"SYNCING"}}`,
			wantErr:  false,
		},
		{
			name:     "non-engine method passes",
			method:   "eth_blockNumber",
			response: `{"jsonrpc":"2.0","id":1,"result":"0x1234"}`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := Parse(tt.response)
			require.NoError(t, err)

			err = validator.Validate(tt.method, resp)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestForkchoiceUpdatedValidator_Validate(t *testing.T) {
	validator := &ForkchoiceUpdatedValidator{}

	tests := []struct {
		name     string
		method   string
		response string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "success status",
			method:   "engine_forkchoiceUpdatedV3",
			response: `{"jsonrpc":"2.0","id":1,"result":{"payloadStatus":{"status":"SUCCESS","latestValidHash":"0x123"}}}`,
			wantErr:  false,
		},
		{
			name:     "valid status fails",
			method:   "engine_forkchoiceUpdatedV3",
			response: `{"jsonrpc":"2.0","id":1,"result":{"payloadStatus":{"status":"VALID"}}}`,
			wantErr:  true,
			errMsg:   "VALID",
		},
		{
			name:     "syncing status",
			method:   "engine_forkchoiceUpdatedV3",
			response: `{"jsonrpc":"2.0","id":1,"result":{"payloadStatus":{"status":"SYNCING"}}}`,
			wantErr:  true,
			errMsg:   "SYNCING",
		},
		{
			name:     "invalid status with validation error",
			method:   "engine_forkchoiceUpdatedV2",
			response: `{"jsonrpc":"2.0","id":1,"result":{"payloadStatus":{"status":"INVALID","validationError":"unknown ancestor"}}}`,
			wantErr:  true,
			errMsg:   "unknown ancestor",
		},
		{
			name:     "non-forkchoiceUpdated method passes",
			method:   "engine_newPayloadV3",
			response: `{"jsonrpc":"2.0","id":1,"result":{"status":"SYNCING"}}`,
			wantErr:  false,
		},
		{
			name:     "non-engine method passes",
			method:   "eth_blockNumber",
			response: `{"jsonrpc":"2.0","id":1,"result":"0x1234"}`,
			wantErr:  false,
		},
		{
			name:     "with payloadId",
			method:   "engine_forkchoiceUpdatedV3",
			response: `{"jsonrpc":"2.0","id":1,"result":{"payloadStatus":{"status":"SUCCESS"},"payloadId":"0xabc"}}`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := Parse(tt.response)
			require.NoError(t, err)

			err = validator.Validate(tt.method, resp)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestComposedValidator_Validate(t *testing.T) {
	validator := NewComposedValidator(
		&ErrorValidator{},
		&NewPayloadValidator{},
	)

	tests := []struct {
		name     string
		method   string
		response string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "both pass",
			method:   "engine_newPayloadV3",
			response: `{"jsonrpc":"2.0","id":1,"result":{"status":"VALID"}}`,
			wantErr:  false,
		},
		{
			name:     "error validator fails first",
			method:   "engine_newPayloadV3",
			response: `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`,
			wantErr:  true,
			errMsg:   "JSON-RPC error",
		},
		{
			name:     "newPayload validator fails",
			method:   "engine_newPayloadV3",
			response: `{"jsonrpc":"2.0","id":1,"result":{"status":"INVALID"}}`,
			wantErr:  true,
			errMsg:   "INVALID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := Parse(tt.response)
			require.NoError(t, err)

			err = validator.Validate(tt.method, resp)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDefaultValidator(t *testing.T) {
	validator := DefaultValidator()
	require.NotNil(t, validator)

	resp, err := Parse(`{"jsonrpc":"2.0","id":1,"result":{"status":"VALID"}}`)
	require.NoError(t, err)

	err = validator.Validate("engine_newPayloadV3", resp)
	assert.NoError(t, err)
}
