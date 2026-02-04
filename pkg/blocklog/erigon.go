package blocklog

import (
	"encoding/json"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// erigonParser is a stub parser for Erigon client logs.
// Returns nil, false until the log format is known.
type erigonParser struct{}

// NewErigonParser creates a new Erigon log parser (stub).
func NewErigonParser() Parser {
	return &erigonParser{}
}

// Ensure interface compliance.
var _ Parser = (*erigonParser)(nil)

// ParseLine is a stub that always returns nil, false.
func (p *erigonParser) ParseLine(_ string) (json.RawMessage, bool) {
	return nil, false
}

// ClientType returns the client type.
func (p *erigonParser) ClientType() client.ClientType {
	return client.ClientErigon
}
