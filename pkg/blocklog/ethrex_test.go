package blocklog

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// feed runs every line through the parser and returns the emitted payloads, in
// order, decoded to maps.
func feed(t *testing.T, p Parser, lines []string) []map[string]any {
	t.Helper()

	var out []map[string]any
	for _, line := range lines {
		if payload, ok := p.ParseLine(line); ok {
			require.NotNil(t, payload)
			var m map[string]any
			require.NoError(t, json.Unmarshal(payload, &m))
			out = append(out, m)
		}
	}

	return out
}

// A full per-block report: header followed by the phase breakdown lines.
var fullBlock = []string{
	`2026-06-11T04:49:18.910Z  INFO [METRIC] BLOCK 24358000 0xc957abc123 | 1.234 Ggas/s | 567.00 ms | 150 txs | 700 Mgas (93%)`,
	`2026-06-11T04:49:18.910Z  INFO   |- validate:   1.50 ms  ( 0%)`,
	`2026-06-11T04:49:18.910Z  INFO   |- exec:     450.00 ms  (79%) << BOTTLENECK`,
	`2026-06-11T04:49:18.910Z  INFO   |- merkle:    12.34 ms  ( 2%)  [concurrent: 1.20 ms, drain: 12.34 ms, overlap: 9%, queue: 0, start_delay: 1.10 ms]`,
	`2026-06-11T04:49:18.910Z  INFO   |- store:      0.82 ms  ( 0%)`,
	"2026-06-11T04:49:18.910Z  INFO   `- warmer:    3.00 ms         [finished: 3.00 ms before exec]",
}

func TestEthrexParser_FullBlock(t *testing.T) {
	out := feed(t, NewEthrexParser(), fullBlock)
	require.Len(t, out, 1, "exactly one complete payload, emitted on the store line")

	data := out[0]
	assert.Equal(t, "info", data["level"])
	assert.Equal(t, "Block execution throughput", data["msg"])

	block := data["block"].(map[string]any)
	assert.Equal(t, float64(24358000), block["number"])
	assert.Equal(t, "0xc957abc123", block["hash"])
	assert.Equal(t, float64(150), block["tx_count"])
	assert.Equal(t, float64(700_000_000), block["gas_used"], "700 Mgas -> absolute gas")

	timing := data["timing"].(map[string]any)
	assert.Equal(t, float64(567), timing["total_ms"])
	assert.Equal(t, float64(450), timing["execution_ms"], "exec")
	assert.Equal(t, 12.34, timing["state_hash_ms"], "merkle -> state_hash_ms")
	assert.Equal(t, 0.82, timing["commit_ms"], "store -> commit_ms")

	throughput := data["throughput"].(map[string]any)
	assert.InDelta(t, 1234.0, throughput["mgas_per_sec"], 0.001, "1.234 Ggas/s -> 1234 Mgas/s")
}

func TestEthrexParser_EmitsOnStoreNotHeader(t *testing.T) {
	p := NewEthrexParser()
	// Header alone must not emit — the runner stores the first payload per hash,
	// so it must be the complete one (emitted on the store line).
	_, ok := p.ParseLine(fullBlock[0])
	assert.False(t, ok, "header line should not emit on its own")
}

func TestEthrexParser_HeaderOnlyFlushedOnNextHeader(t *testing.T) {
	// A build that logs only headers (no phase lines): each header flushes the
	// previous block so its total_ms/throughput are still captured.
	lines := []string{
		`INFO [METRIC] BLOCK 100 0xaaa | 1.000 Ggas/s | 10.00 ms | 5 txs | 50 Mgas (50%)`,
		`INFO [METRIC] BLOCK 101 0xbbb | 2.000 Ggas/s | 20.00 ms | 6 txs | 60 Mgas (50%)`,
	}
	out := feed(t, NewEthrexParser(), lines)
	require.Len(t, out, 1, "first block flushed when the second header arrives")

	block := out[0]["block"].(map[string]any)
	assert.Equal(t, float64(100), block["number"])
	assert.Equal(t, "0xaaa", block["hash"])

	timing := out[0]["timing"].(map[string]any)
	assert.Equal(t, float64(10), timing["total_ms"])
	_, hasMerkle := timing["state_hash_ms"]
	assert.False(t, hasMerkle, "header-only block has no phase timings")
}

func TestEthrexParser_HeaderWithoutHash(t *testing.T) {
	lines := append([]string{
		`INFO [METRIC] BLOCK 24358000 | 1.234 Ggas/s | 567.00 ms | 150 txs | 700 Mgas (93%)`,
	}, fullBlock[1:]...)
	out := feed(t, NewEthrexParser(), lines)
	require.Len(t, out, 1)

	block := out[0]["block"].(map[string]any)
	_, hasHash := block["hash"]
	assert.False(t, hasHash, "no hash should be set when the log omits it")
}

func TestEthrexParser_ANSIStripped(t *testing.T) {
	lines := []string{
		"\x1b[2m2026-06-11T04:49:18Z\x1b[0m \x1b[32m INFO\x1b[0m [METRIC] BLOCK 24358001 0x9f56 | 2.500 Ggas/s | 400.00 ms | 200 txs | 1000 Mgas (50%)",
		"\x1b[32m INFO\x1b[0m   |- exec:     100.00 ms  (25%)",
		"\x1b[32m INFO\x1b[0m   |- store:      5.00 ms  ( 1%)",
	}
	out := feed(t, NewEthrexParser(), lines)
	require.Len(t, out, 1)

	block := out[0]["block"].(map[string]any)
	assert.Equal(t, float64(24358001), block["number"])
	assert.Equal(t, "0x9f56", block["hash"])
	timing := out[0]["timing"].(map[string]any)
	assert.Equal(t, float64(100), timing["execution_ms"])
}

func TestEthrexParser_NonMatchingLines(t *testing.T) {
	p := NewEthrexParser()
	for _, line := range []string{
		"",
		"some random log output",
		`INFO Initiating blockchain with levm`,
		`INFO [METRIC] BLOCK BUILDING THROUGHPUT: 1.234 Gigagas/s TIME SPENT: 567 msecs`,
		// phase line with no pending header must not emit
		`INFO   |- exec:     450.00 ms  (80%) << BOTTLENECK`,
	} {
		_, ok := p.ParseLine(line)
		assert.False(t, ok, "line should not emit: %q", line)
	}
}

func TestEthrexParser_ClientType(t *testing.T) {
	parser := NewEthrexParser()
	assert.Equal(t, "ethrex", string(parser.ClientType()))
}
