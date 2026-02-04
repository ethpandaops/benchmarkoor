package blocklog

import (
	"encoding/json"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// Parser extracts JSON payloads from client log lines.
type Parser interface {
	// ParseLine attempts to extract a JSON payload from a log line.
	// Returns the raw JSON message and true if successful, or nil and false if the line
	// doesn't contain a parseable JSON payload.
	ParseLine(line string) (json.RawMessage, bool)

	// ClientType returns the client type this parser is for.
	ClientType() client.ClientType
}

// NewParser returns the appropriate parser for the given client type.
// Returns nil if the client type is not supported or unknown.
func NewParser(clientType client.ClientType) Parser {
	switch clientType {
	case client.ClientGeth:
		return NewGethParser()
	case client.ClientNethermind:
		return NewNethermindParser()
	case client.ClientBesu:
		return NewBesuParser()
	case client.ClientErigon:
		return NewErigonParser()
	case client.ClientNimbus:
		return NewNimbusParser()
	case client.ClientReth:
		return NewRethParser()
	default:
		return NewNoopParser()
	}
}
