package blocklog

import (
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
)

// ethrexLogPattern matches the summary header that ethrex emits per imported
// block from Blockchain::print_add_block_pipeline_logs (the engine_newPayload
// execution path). After ANSI stripping the header looks like:
//
//	[METRIC] BLOCK 24358000 0x<hash> | 1.234 Ggas/s | 567.00 ms | 150 txs | 700 Mgas (93%)
//
// The block hash is optional: it is only present on ethrex builds that include
// it in the header. benchmarkoor's collector can only associate a log with a
// test when block.hash is present, so a hash-less build parses but never
// matches.
//
// Note: ethrex follows this header with separate "|- validate/exec/merkle/store"
// lines. Those are distinct log lines, so the collector (which matches one
// payload per line) cannot fold them into this payload; only the header fields
// are captured.
var ethrexLogPattern = regexp.MustCompile(
	`\[METRIC\] BLOCK (\d+)(?:\s+(0x[0-9a-fA-F]+))? \| ` +
		`([0-9.]+) Ggas/s \| ([0-9.]+) ms \| (\d+) txs \| ([0-9.]+) Mgas \((\d+)%\)`,
)

// ethrexParser parses metrics from ethrex block execution throughput logs.
type ethrexParser struct{}

// NewEthrexParser creates a new ethrex log parser.
func NewEthrexParser() Parser {
	return &ethrexParser{}
}

// Ensure interface compliance.
var _ Parser = (*ethrexParser)(nil)

// ParseLine extracts metrics from an ethrex per-block metric header and returns
// them as a nested JSON structure matching the shape used by the other client
// parsers ({ "block": { "hash": ... }, ... }).
func (p *ethrexParser) ParseLine(line string) (json.RawMessage, bool) {
	// Strip ANSI escape codes — ethrex colorizes stdout logs when on a TTY.
	line = ansiPattern.ReplaceAllString(line, "")

	matches := ethrexLogPattern.FindStringSubmatch(line)
	if matches == nil {
		return nil, false
	}

	block := map[string]any{
		"number":        parseInt(matches[1]),
		"gas_used_mgas": parseFloat(matches[6]),
		"gas_used_pct":  parseInt(matches[7]),
		"tx_count":      parseInt(matches[5]),
	}
	// matches[2] (hash) is only present on builds that log it; the collector
	// needs block.hash to associate the log with a test.
	if matches[2] != "" {
		block["hash"] = matches[2]
	}

	result := map[string]any{
		"level":      "info",
		"msg":        "Block execution throughput",
		"block":      block,
		"timing":     map[string]any{"total_ms": parseFloat(matches[4])},
		"throughput": map[string]any{"ggas_per_sec": parseFloat(matches[3])},
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, false
	}

	return json.RawMessage(data), true
}

// ClientType returns the client type.
func (p *ethrexParser) ClientType() client.ClientType {
	return client.ClientEthrex
}

// parseInt parses a base-10 integer, returning 0 on failure.
func parseInt(s string) int64 {
	i, _ := strconv.ParseInt(s, 10, 64)

	return i
}

// parseFloat parses a float, returning 0 on failure.
func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)

	return f
}
