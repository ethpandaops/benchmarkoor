package blocklog

import (
	"encoding/json"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// ethrexParser is a stub parser for ethrex client logs.
// Returns nil, false until the log format is known.
type ethrexParser struct{}

// NewEthrexParser creates a new ethrex log parser (stub).
func NewEthrexParser() Parser {
	return &ethrexParser{}
}

// Ensure interface compliance.
var _ Parser = (*ethrexParser)(nil)

// ParseLine is a stub that always returns nil, false.
func (p *ethrexParser) ParseLine(_ string) (json.RawMessage, bool) {
	return nil, false
}

// ClientType returns the client type.
func (p *ethrexParser) ClientType() client.ClientType {
	return client.ClientEthrex
}
