package blocklog

import (
	"encoding/json"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// nimbusParser is a stub parser for Nimbus client logs.
// Returns nil, false until the log format is known.
type nimbusParser struct{}

// NewNimbusParser creates a new Nimbus log parser (stub).
func NewNimbusParser() Parser {
	return &nimbusParser{}
}

// Ensure interface compliance.
var _ Parser = (*nimbusParser)(nil)

// ParseLine is a stub that always returns nil, false.
func (p *nimbusParser) ParseLine(_ string) (json.RawMessage, bool) {
	return nil, false
}

// ClientType returns the client type.
func (p *nimbusParser) ClientType() client.ClientType {
	return client.ClientNimbus
}
