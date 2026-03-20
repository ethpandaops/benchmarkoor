package blocklog

import (
	"encoding/json"
	"regexp"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// besuLogPattern matches Besu SlowBlock log lines (after ANSI stripping).
// Format: <timestamp> | <thread> | WARN  | SlowBlock | {JSON}
var besuLogPattern = regexp.MustCompile(
	`^.+\|\s*(?:WARN|INFO)\s*\|\s*SlowBlock\s*\|\s*(\{.+\})\s*$`,
)

// besuParser parses JSON payloads from Besu client SlowBlock logs.
type besuParser struct{}

// NewBesuParser creates a new Besu log parser.
func NewBesuParser() Parser {
	return &besuParser{}
}

// Ensure interface compliance.
var _ Parser = (*besuParser)(nil)

// ParseLine extracts JSON from a Besu SlowBlock log line.
func (p *besuParser) ParseLine(line string) (json.RawMessage, bool) {
	// Strip ANSI escape codes — Besu logs include color/style sequences.
	line = ansiPattern.ReplaceAllString(line, "")

	matches := besuLogPattern.FindStringSubmatch(line)
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
func (p *besuParser) ClientType() client.ClientType {
	return client.ClientBesu
}
