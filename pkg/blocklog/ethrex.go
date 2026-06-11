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
var ethrexLogPattern = regexp.MustCompile(
	`\[METRIC\] BLOCK (\d+)(?:\s+(0x[0-9a-fA-F]+))? \| ` +
		`([0-9.]+) Ggas/s \| ([0-9.]+) ms \| (\d+) txs \| ([0-9.]+) Mgas \((\d+)%\)`,
)

// ethrexPhasePattern matches the per-phase breakdown lines that follow the
// header, e.g. (after ANSI stripping):
//
//	|- exec:      450.00 ms  (80%) << BOTTLENECK
//	|- merkle:     12.34 ms  (20%)  [concurrent: 1.2 ms, drain: 12.3 ms, ...]
//	|- store:       0.82 ms  ( 4%)
//
// validate and warmer lines share the shape but have no destination column, so
// only exec/merkle/store are mapped.
var ethrexPhasePattern = regexp.MustCompile(
	`\|-\s*(exec|merkle|store):\s*([0-9.]+)\s*ms`,
)

// ethrexParser parses metrics from ethrex block execution throughput logs.
//
// ethrex prints one header line followed by separate phase lines per block, so
// the parser is stateful: it buffers the header and folds in the phase timings,
// emitting one complete payload. Because benchmarkoor registers a block hash
// *before* the engine_newPayload RPC, the first payload emitted for a hash is
// the one stored — so it must already be complete. It is emitted on the `store`
// line (the last mapped phase); a block that never reaches `store` (e.g. a
// header-only build) is flushed when the next header arrives.
type ethrexParser struct {
	pending *ethrexBlock
}

type ethrexBlock struct {
	number      int64
	hash        string
	gasUsedMgas float64
	txCount     int64
	totalMs     float64
	mgasPerSec  float64
	execMs      float64
	merkleMs    float64
	storeMs     float64
	haveExec    bool
	haveMerkle  bool
	haveStore   bool
	emitted     bool
}

// NewEthrexParser creates a new ethrex log parser.
func NewEthrexParser() Parser {
	return &ethrexParser{}
}

// Ensure interface compliance.
var _ Parser = (*ethrexParser)(nil)

// ParseLine extracts metrics from ethrex per-block metric logs and returns them
// as a nested JSON structure matching the shape used by the other client
// parsers ({ "block": {...}, "timing": {...}, "throughput": {...} }).
func (p *ethrexParser) ParseLine(line string) (json.RawMessage, bool) {
	// Strip ANSI escape codes — ethrex colorizes stdout logs when on a TTY.
	line = ansiPattern.ReplaceAllString(line, "")

	if m := ethrexLogPattern.FindStringSubmatch(line); m != nil {
		// New block header. Flush the previous block if it never reached its
		// `store` line (header-only build, or a missing phase) so it isn't lost.
		var flushed json.RawMessage
		var flush bool
		if p.pending != nil && !p.pending.emitted {
			flushed, flush = p.pending.payload()
		}
		p.pending = &ethrexBlock{
			number:      parseInt(m[1]),
			hash:        m[2],
			mgasPerSec:  parseFloat(m[3]) * 1000.0, // Ggas/s -> Mgas/s
			totalMs:     parseFloat(m[4]),
			txCount:     parseInt(m[5]),
			gasUsedMgas: parseFloat(m[6]),
		}
		if flush {
			return flushed, true
		}

		return nil, false
	}

	if p.pending != nil {
		if m := ethrexPhasePattern.FindStringSubmatch(line); m != nil {
			ms := parseFloat(m[2])
			switch m[1] {
			case "exec":
				p.pending.execMs, p.pending.haveExec = ms, true
			case "merkle":
				p.pending.merkleMs, p.pending.haveMerkle = ms, true
			case "store":
				p.pending.storeMs, p.pending.haveStore = ms, true
				// `store` is the last mapped phase: the record is complete.
				p.pending.emitted = true

				return p.pending.payload()
			}
		}
	}

	return nil, false
}

// payload renders the buffered block as the nested JSON the indexer expects.
func (b *ethrexBlock) payload() (json.RawMessage, bool) {
	block := map[string]any{
		"number":   b.number,
		"gas_used": int64(b.gasUsedMgas*1e6 + 0.5), // header only carries Mgas
		"tx_count": b.txCount,
	}
	// hash is only present on builds that log it; the collector needs it to
	// associate the log with a test.
	if b.hash != "" {
		block["hash"] = b.hash
	}

	timing := map[string]any{"total_ms": b.totalMs}
	if b.haveExec {
		timing["execution_ms"] = b.execMs
	}
	if b.haveMerkle {
		timing["state_hash_ms"] = b.merkleMs // ethrex "merkle" == state hashing
	}
	if b.haveStore {
		timing["commit_ms"] = b.storeMs // ethrex "store" == commit
	}

	result := map[string]any{
		"level":      "info",
		"msg":        "Block execution throughput",
		"block":      block,
		"timing":     timing,
		"throughput": map[string]any{"mgas_per_sec": b.mgasPerSec},
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
