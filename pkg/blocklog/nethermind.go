package blocklog

import (
	"encoding/json"
	"regexp"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// nethermindLogPattern matches Nethermind Slow block log lines (after ANSI stripping).
// Format: <timestamp> | {JSON with "msg":"Slow block"}
var nethermindLogPattern = regexp.MustCompile(
	`^\s*\d+\s+\w+\s+\d+:\d+:\d+\s*\|\s*(\{.+\})\s*$`,
)

// nethermindParser parses JSON payloads from Nethermind client Slow block logs.
type nethermindParser struct{}

// NewNethermindParser creates a new Nethermind log parser.
func NewNethermindParser() Parser {
	return &nethermindParser{}
}

// Ensure interface compliance.
var _ Parser = (*nethermindParser)(nil)

// ParseLine extracts JSON from a Nethermind Slow block log line.
func (p *nethermindParser) ParseLine(line string) (json.RawMessage, bool) {
	// Strip ANSI escape codes — Nethermind logs include color/style sequences.
	line = ansiPattern.ReplaceAllString(line, "")

	matches := nethermindLogPattern.FindStringSubmatch(line)
	if len(matches) < 2 {
		return nil, false
	}

	jsonStr := matches[1]

	// Validate that it's valid JSON and contains the expected "Slow block" message.
	var probe struct {
		Msg string `json:"msg"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &probe); err != nil {
		return nil, false
	}

	if probe.Msg != "Slow block" {
		return nil, false
	}

	return json.RawMessage(jsonStr), true
}

// ClientType returns the client type.
func (p *nethermindParser) ClientType() client.ClientType {
	return client.ClientNethermind
}
