package blocklog

import (
	"encoding/json"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// nethermindParser is a stub parser for Nethermind client logs.
// Returns nil, false until the log format is known.
type nethermindParser struct{}

// NewNethermindParser creates a new Nethermind log parser (stub).
func NewNethermindParser() Parser {
	return &nethermindParser{}
}

// Ensure interface compliance.
var _ Parser = (*nethermindParser)(nil)

// ParseLine is a stub that always returns nil, false.
func (p *nethermindParser) ParseLine(_ string) (json.RawMessage, bool) {
	return nil, false
}

// ClientType returns the client type.
func (p *nethermindParser) ClientType() client.ClientType {
	return client.ClientNethermind
}
