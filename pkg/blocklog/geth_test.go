package blocklog

import (
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGethParser_ParseLine(t *testing.T) {
	parser := NewGethParser()

	tests := []struct {
		name     string
		line     string
		wantOK   bool
		wantJSON string
	}{
		{
			name:     "valid WARN with JSON",
			line:     `WARN [02-02|15:03:22.121] {"level":"warn","msg":"Slow block","block":{"number":123}}`,
			wantOK:   true,
			wantJSON: `{"level":"warn","msg":"Slow block","block":{"number":123}}`,
		},
		{
			name:     "valid INFO with JSON",
			line:     `INFO [01-15|10:30:00.000] {"msg":"test","value":42}`,
			wantOK:   true,
			wantJSON: `{"msg":"test","value":42}`,
		},
		{
			name:     "valid DEBUG with JSON",
			line:     `DEBUG [12-31|23:59:59.999] {"debug":true}`,
			wantOK:   true,
			wantJSON: `{"debug":true}`,
		},
		{
			name:     "valid ERROR with JSON",
			line:     `ERROR [06-15|12:00:00.123] {"error":"something went wrong"}`,
			wantOK:   true,
			wantJSON: `{"error":"something went wrong"}`,
		},
		{
			name:   "no JSON payload",
			line:   `WARN [02-02|15:03:22.121] Some regular log message`,
			wantOK: false,
		},
		{
			name:   "invalid JSON",
			line:   `WARN [02-02|15:03:22.121] {invalid json}`,
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOK: false,
		},
		{
			name:   "no log level",
			line:   `[02-02|15:03:22.121] {"msg":"test"}`,
			wantOK: false,
		},
		{
			name:   "missing timestamp",
			line:   `WARN {"msg":"test"}`,
			wantOK: false,
		},
		{
			name:     "JSON with trailing whitespace",
			line:     `WARN [02-02|15:03:22.121] {"msg":"test"}   `,
			wantOK:   true,
			wantJSON: `{"msg":"test"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := parser.ParseLine(tt.line)

			assert.Equal(t, tt.wantOK, ok)

			if tt.wantOK {
				require.NotNil(t, result)
				assert.JSONEq(t, tt.wantJSON, string(result))
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestGethParser_ClientType(t *testing.T) {
	parser := NewGethParser()
	assert.Equal(t, "geth", string(parser.ClientType()))
}

func TestNewParser(t *testing.T) {
	tests := []struct {
		clientType client.ClientType
		wantType   string
	}{
		{client.ClientGeth, "geth"},
		{client.ClientNethermind, "nethermind"},
		{client.ClientBesu, "besu"},
		{client.ClientErigon, "erigon"},
		{client.ClientNimbus, "nimbus"},
		{client.ClientReth, "reth"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.clientType), func(t *testing.T) {
			parser := NewParser(tt.clientType)
			require.NotNil(t, parser)
			assert.Equal(t, tt.wantType, string(parser.ClientType()))
		})
	}
}
