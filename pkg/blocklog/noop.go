package blocklog

import (
	"encoding/json"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// noopParser is a no-op parser for unknown or unsupported client types.
// Always returns nil, false.
type noopParser struct{}

// NewNoopParser creates a new no-op parser.
func NewNoopParser() Parser {
	return &noopParser{}
}

// Ensure interface compliance.
var _ Parser = (*noopParser)(nil)

// ParseLine always returns nil, false.
func (p *noopParser) ParseLine(_ string) (json.RawMessage, bool) {
	return nil, false
}

// ClientType returns an empty client type.
func (p *noopParser) ClientType() client.ClientType {
	return ""
}
