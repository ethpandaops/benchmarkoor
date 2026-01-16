package jsonrpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		checkResp func(*testing.T, *Response)
	}{
		{
			name:    "valid response with result",
			input:   `{"jsonrpc":"2.0","id":1,"result":{"status":"VALID"}}`,
			wantErr: false,
			checkResp: func(t *testing.T, resp *Response) {
				assert.Equal(t, "2.0", resp.JSONRPC)
				assert.NotNil(t, resp.Result)
				assert.Nil(t, resp.Error)
			},
		},
		{
			name:    "valid response with error",
			input:   `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`,
			wantErr: false,
			checkResp: func(t *testing.T, resp *Response) {
				assert.Equal(t, "2.0", resp.JSONRPC)
				require.NotNil(t, resp.Error)
				assert.Equal(t, -32600, resp.Error.Code)
				assert.Equal(t, "Invalid Request", resp.Error.Message)
			},
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   ``,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := Parse(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, resp)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)

			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
		})
	}
}

func TestResponse_ParseResult(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(*testing.T, *NewPayloadResult)
	}{
		{
			name:    "valid newPayload result",
			input:   `{"jsonrpc":"2.0","id":1,"result":{"status":"VALID","latestValidHash":"0x123"}}`,
			wantErr: false,
			check: func(t *testing.T, result *NewPayloadResult) {
				assert.Equal(t, "VALID", result.Status)
				assert.Equal(t, "0x123", result.LatestValidHash)
			},
		},
		{
			name:    "no result field",
			input:   `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"error"}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := Parse(tt.input)
			require.NoError(t, err)

			var result NewPayloadResult
			err = resp.ParseResult(&result)

			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)

			if tt.check != nil {
				tt.check(t, &result)
			}
		})
	}
}
