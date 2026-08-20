package blocklog

import (
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/stretchr/testify/assert"
)

func TestTempoParserUsesRethLogFormat(t *testing.T) {
	parser := NewParser(client.ClientTempo)
	assert.Equal(t, client.ClientTempo, parser.ClientType())

	payload, ok := parser.ParseLine(
		"2026-08-19T00:00:00Z WARN reth::slow_block: Slow block block.hash=0x01 execution_ms=12",
	)
	assert.True(t, ok)
	assert.JSONEq(t, `{"level":"warn","msg":"Slow block","block":{"hash":"0x01"},"execution_ms":12}`, string(payload))
}
