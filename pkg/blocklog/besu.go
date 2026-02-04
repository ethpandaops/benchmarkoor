package blocklog

import (
	"encoding/json"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// besuParser is a stub parser for Besu client logs.
// Returns nil, false until the log format is known.
type besuParser struct{}

// NewBesuParser creates a new Besu log parser (stub).
func NewBesuParser() Parser {
	return &besuParser{}
}

// Ensure interface compliance.
var _ Parser = (*besuParser)(nil)

// ParseLine is a stub that always returns nil, false.
func (p *besuParser) ParseLine(_ string) (json.RawMessage, bool) {
	return nil, false
}

// ClientType returns the client type.
func (p *besuParser) ClientType() client.ClientType {
	return client.ClientBesu
}
