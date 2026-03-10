package blocklog

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// rethLogPattern matches reth slow_block log lines (after ANSI stripping).
// Format: <timestamp> WARN reth::slow_block: Slow block <key=value pairs>
var rethLogPattern = regexp.MustCompile(
	`^\S+\s+WARN\s+reth::slow_block:\s+Slow block\s+(.+)$`,
)

// ansiPattern matches ANSI escape sequences (colors, styles, etc.).
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// rethParser parses key=value pairs from Reth client slow_block logs.
type rethParser struct{}

// NewRethParser creates a new Reth log parser.
func NewRethParser() Parser {
	return &rethParser{}
}

// Ensure interface compliance.
var _ Parser = (*rethParser)(nil)

// ParseLine extracts metrics from a Reth slow_block log line and
// returns them as a nested JSON structure.
func (p *rethParser) ParseLine(line string) (json.RawMessage, bool) {
	// Strip ANSI escape codes — reth logs include color/style sequences.
	line = ansiPattern.ReplaceAllString(line, "")

	matches := rethLogPattern.FindStringSubmatch(line)
	if len(matches) < 2 {
		return nil, false
	}

	kvPart := matches[1]
	result := map[string]any{
		"level": "warn",
		"msg":   "Slow block",
	}

	for _, token := range parseKVTokens(kvPart) {
		key, value, ok := parseKVPair(token)
		if !ok {
			continue
		}

		setNested(result, strings.Split(key, "."), value)
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, false
	}

	return json.RawMessage(data), true
}

// ClientType returns the client type.
func (p *rethParser) ClientType() client.ClientType {
	return client.ClientReth
}

// parseKVTokens splits a key=value string into individual tokens,
// handling quoted values that may contain spaces.
func parseKVTokens(s string) []string {
	var tokens []string

	var current strings.Builder

	inQuote := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		switch {
		case ch == '"':
			inQuote = !inQuote
			current.WriteByte(ch)
		case ch == ' ' && !inQuote:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// parseKVPair splits a "key=value" token and parses the value into
// the appropriate Go type (int64, float64, or string).
func parseKVPair(token string) (string, any, bool) {
	key, raw, ok := strings.Cut(token, "=")
	if !ok {
		return "", nil, false
	}

	// Strip surrounding quotes.
	raw = strings.Trim(raw, "\"")

	return key, parseValue(raw), true
}

// parseValue attempts to parse a string as int64, then float64,
// falling back to string.
func parseValue(s string) any {
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	return s
}

// setNested inserts a value into a nested map structure following
// the given key path (e.g. ["block", "number"] → {"block": {"number": v}}).
func setNested(m map[string]any, keys []string, value any) {
	for i, key := range keys {
		if i == len(keys)-1 {
			m[key] = value

			return
		}

		sub, ok := m[key]
		if !ok {
			child := make(map[string]any, 4)
			m[key] = child
			m = child

			continue
		}

		child, ok := sub.(map[string]any)
		if !ok {
			// Key already has a non-map value; overwrite with map.
			child = make(map[string]any, 4)
			m[key] = child
		}

		m = child
	}
}
