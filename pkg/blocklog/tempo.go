package blocklog

import (
	"encoding/json"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// tempoParser reuses Reth's structured slow-block parser because Tempo is a
// Reth SDK node, while preserving the Tempo client identity in reports.
type tempoParser struct {
	reth Parser
}

// NewTempoParser creates a Tempo log parser.
func NewTempoParser() Parser {
	return &tempoParser{reth: NewRethParser()}
}

var _ Parser = (*tempoParser)(nil)

func (p *tempoParser) ParseLine(line string) (json.RawMessage, bool) {
	return p.reth.ParseLine(line)
}

func (p *tempoParser) ClientType() client.ClientType {
	return client.ClientTempo
}
