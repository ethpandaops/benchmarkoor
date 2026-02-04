package blocklog

import (
	"encoding/json"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// rethParser is a stub parser for Reth client logs.
// Returns nil, false until the log format is known.
type rethParser struct{}

// NewRethParser creates a new Reth log parser (stub).
func NewRethParser() Parser {
	return &rethParser{}
}

// Ensure interface compliance.
var _ Parser = (*rethParser)(nil)

// ParseLine is a stub that always returns nil, false.
func (p *rethParser) ParseLine(_ string) (json.RawMessage, bool) {
	return nil, false
}

// ClientType returns the client type.
func (p *rethParser) ClientType() client.ClientType {
	return client.ClientReth
}
