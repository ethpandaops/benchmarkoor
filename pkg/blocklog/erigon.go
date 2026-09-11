package blocklog

import (
	"encoding/json"
	"regexp"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// erigonLogPattern matches the console form (after ANSI stripping): an
// optionally bracketed level, an optional bracketed timestamp, then the JSON
// payload. Requiring a token before the brace is what routes --log.json lines
// to erigonJSONPayload instead.
var erigonLogPattern = regexp.MustCompile(
	`^\[?\w+\s*\]?\s*(?:\[[^\]]+\]\s*)?(\{.+\})\s*$`,
)

// erigonParser parses JSON payloads from Erigon client slow block logs.
type erigonParser struct{}

// NewErigonParser creates a new Erigon log parser.
func NewErigonParser() Parser {
	return &erigonParser{}
}

// Ensure interface compliance.
var _ Parser = (*erigonParser)(nil)

// ParseLine extracts JSON from an Erigon slow block log line.
func (p *erigonParser) ParseLine(line string) (json.RawMessage, bool) {
	// Strip ANSI escape codes — Erigon colorises the level when on a TTY.
	line = ansiPattern.ReplaceAllString(line, "")

	payload, ok := erigonConsolePayload(line)
	if !ok {
		payload, ok = erigonJSONPayload(line)
	}

	if !ok {
		return nil, false
	}

	return erigonSlowBlock(payload)
}

func erigonConsolePayload(line string) (string, bool) {
	matches := erigonLogPattern.FindStringSubmatch(line)
	if len(matches) < 2 {
		return "", false
	}

	return matches[1], true
}

// erigonJSONPayload unwraps the record from --log.json output, where the whole
// object arrives escaped inside the log line's own msg field.
func erigonJSONPayload(line string) (string, bool) {
	var envelope struct {
		Msg string `json:"msg"`
	}

	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		return "", false
	}

	return envelope.Msg, envelope.Msg != ""
}

// erigonSlowBlock keeps the payload only when it is a slow block record.
// Erigon has no slow block logger to key on, so the payload is probed.
func erigonSlowBlock(payload string) (json.RawMessage, bool) {
	var probe struct {
		Msg    string           `json:"msg"`
		Timing *json.RawMessage `json:"timing"`
	}

	if err := json.Unmarshal([]byte(payload), &probe); err != nil {
		return nil, false
	}

	if probe.Msg != "Slow block" || probe.Timing == nil {
		return nil, false
	}

	return json.RawMessage(payload), true
}

// ClientType returns the client type.
func (p *erigonParser) ClientType() client.ClientType {
	return client.ClientErigon
}
