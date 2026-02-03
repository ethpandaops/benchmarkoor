package blocklog

import (
	"encoding/json"
	"regexp"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// gethLogPattern matches Geth log lines with embedded JSON.
// Format: {LEVEL} [{timestamp}] {JSON payload}
// Example: WARN [02-02|15:03:22.121] {"level":"warn","msg":"Slow block",...}
var gethLogPattern = regexp.MustCompile(`^(?:WARN|INFO|DEBUG|ERROR)\s+\[[^\]]+\]\s+(\{.+\})\s*$`)

// gethParser parses JSON payloads from Geth client logs.
type gethParser struct{}

// NewGethParser creates a new Geth log parser.
func NewGethParser() Parser {
	return &gethParser{}
}

// Ensure interface compliance.
var _ Parser = (*gethParser)(nil)

// ParseLine extracts JSON from a Geth log line.
func (p *gethParser) ParseLine(line string) (json.RawMessage, bool) {
	matches := gethLogPattern.FindStringSubmatch(line)
	if len(matches) < 2 {
		return nil, false
	}

	jsonStr := matches[1]

	// Validate that it's valid JSON.
	if !json.Valid([]byte(jsonStr)) {
		return nil, false
	}

	return json.RawMessage(jsonStr), true
}

// ClientType returns the client type.
func (p *gethParser) ClientType() client.ClientType {
	return client.ClientGeth
}
