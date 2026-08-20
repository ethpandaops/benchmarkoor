package runner

import (
	"encoding/json"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForkchoicePayloadUsesTempoDialect(t *testing.T) {
	dialect := client.EngineAPIDialectFor(client.NewTempoSpec())
	payload := forkchoicePayload(dialect, "0xhead", "0xanchor")

	var request struct {
		Method string `json:"method"`
		Params []any  `json:"params"`
	}
	require.NoError(t, json.Unmarshal([]byte(payload), &request))
	assert.Equal(t, "reth_forkchoiceUpdated", request.Method)
	assert.Len(t, request.Params, 1)
}
