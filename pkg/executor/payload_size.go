package executor

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/eest"
	"github.com/golang/snappy"
	"github.com/sirupsen/logrus"
)

// NewPayloadLine represents one engine_newPayloadVN line found in a test step.
type NewPayloadLine struct {
	Version int // the V from "engine_newPayloadV{N}"
	Payload *eest.ExecutionPayload
}

// ExtractNewPayloadLines parses each test-step JSON-RPC line and returns the
// engine_newPayloadV* entries with their parsed executionPayload. Malformed
// lines and non-newPayload methods are skipped silently.
func ExtractNewPayloadLines(lines []string) []NewPayloadLine {
	out := make([]NewPayloadLine, 0, len(lines))
	for _, line := range lines {
		v, ep, ok := parseNewPayloadLine(line)
		if !ok {
			continue
		}
		out = append(out, NewPayloadLine{Version: v, Payload: ep})
	}
	return out
}

type newPayloadRequest struct {
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

func parseNewPayloadLine(line string) (int, *eest.ExecutionPayload, bool) {
	var req newPayloadRequest
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return 0, nil, false
	}
	if !strings.HasPrefix(req.Method, "engine_newPayloadV") {
		return 0, nil, false
	}
	versionStr := strings.TrimPrefix(req.Method, "engine_newPayloadV")
	version, err := strconv.Atoi(versionStr)
	if err != nil || version < 1 {
		return 0, nil, false
	}
	if len(req.Params) == 0 {
		return 0, nil, false
	}
	var ep eest.ExecutionPayload
	if err := json.Unmarshal(req.Params[0], &ep); err != nil {
		return 0, nil, false
	}
	return version, &ep, true
}

// PayloadSizeBuckets carries per-engine_newPayload byte counts for a single
// step. Each slice has one element per engine_newPayload* line in step
// order, so multi-block steps can be inspected block-by-block.
//
// Field semantics (suffix = what the bytes are):
//   - SSZFull: full SSZ-encoded ExecutionPayload (BlockAccessList inline
//     for Gloas+). Uncompressed.
//   - SSZBAL: just the SSZ-encoded BlockAccessList sub-field, decoded from
//     the hex on the wire (a subset of SSZFull). 0 for pre-Gloas payloads.
//   - SSZFullSnappy: snappy(SSZFull) — same SSZ bytes after compression.
//   - JSONFull: canonical JSON encoding of the same ExecutionPayload
//     (json.Marshal output, no envelope, no whitespace).
//   - JSONBAL: byte length of the BAL's hex string as it appears in JSON
//     (e.g. "0xabc..." — chars only, not counting the surrounding JSON
//     quotes). 0 for pre-Gloas payloads.
//
// JSON shape (one of the nested objects under "payload_sizes"):
//
//	{
//	  "ssz_full": [...], "ssz_bal": [...], "ssz_full_snappy": [...],
//	  "json_full": [...], "json_bal": [...]
//	}
type PayloadSizeBuckets struct {
	SSZFull       []uint64 `json:"ssz_full"`
	SSZBAL        []uint64 `json:"ssz_bal"`
	SSZFullSnappy []uint64 `json:"ssz_full_snappy"`
	JSONFull      []uint64 `json:"json_full"`
	JSONBAL       []uint64 `json:"json_bal"`
	RLPFull       []uint64 `json:"rlp_full,omitempty"`
	RLPBAL        []uint64 `json:"rlp_bal,omitempty"`
	RLPFullSnappy []uint64 `json:"rlp_full_snappy,omitempty"`
}

// HasData returns true if any of the slices contain a non-zero entry.
func (b *PayloadSizeBuckets) HasData() bool {
	if b == nil {
		return false
	}
	for _, slice := range [][]uint64{
		b.SSZFull, b.SSZBAL, b.SSZFullSnappy, b.JSONFull, b.JSONBAL,
		b.RLPFull, b.RLPBAL, b.RLPFullSnappy,
	} {
		for _, v := range slice {
			if v > 0 {
				return true
			}
		}
	}
	return false
}

// PayloadSizes is the per-test container persisted under "payload_sizes" in
// summary.json. Each step that actually contains engine_newPayload* lines
// gets a populated nested object; steps with no newPayload activity are
// omitted entirely.
//
// JSON shape:
//
//	"payload_sizes": {
//	  "test":    {"raw": [...], "bal": [...], "snappy": [...]},
//	  "setup":   {"raw": [...], "bal": [...], "snappy": [...]},
//	  "cleanup": {"raw": [...], "bal": [...], "snappy": [...]}
//	}
type PayloadSizes struct {
	Test    *PayloadSizeBuckets `json:"test,omitempty"`
	Setup   *PayloadSizeBuckets `json:"setup,omitempty"`
	Cleanup *PayloadSizeBuckets `json:"cleanup,omitempty"`
}

// HasData returns true if any nested bucket carries non-zero data.
func (p *PayloadSizes) HasData() bool {
	if p == nil {
		return false
	}
	return p.Test.HasData() || p.Setup.HasData() || p.Cleanup.HasData()
}

// ComputePayloadSizeBuckets parses each engine_newPayloadV* line in a step,
// SSZ-encodes the executionPayload using Prysm's fork-specific struct, snappy-
// compresses the result, and decodes the inline BlockAccessList. It returns
// per-newPayload byte counts (one element per engine_newPayloadV* line, in
// step order) so multi-block steps can be inspected block-by-block.
//
// Lines that fail to convert/encode are logged with the test name and
// **skipped** (no entry is appended for that block) — the function never
// returns an error. A step with no recognizable newPayload lines yields
// three empty slices.
func ComputePayloadSizeBuckets(log logrus.FieldLogger, testName string, lines []string) PayloadSizeBuckets {
	nps := ExtractNewPayloadLines(lines)
	out := PayloadSizeBuckets{
		SSZFull:       make([]uint64, 0, len(nps)),
		SSZBAL:        make([]uint64, 0, len(nps)),
		SSZFullSnappy: make([]uint64, 0, len(nps)),
		JSONFull:      make([]uint64, 0, len(nps)),
		JSONBAL:       make([]uint64, 0, len(nps)),
	}
	for _, np := range nps {
		marshaler, err := eest.EestToPrysmPayload(np.Version, np.Payload)
		if err != nil {
			log.WithFields(logrus.Fields{
				"test":    testName,
				"version": np.Version,
			}).WithError(err).Warn("Failed to convert payload to Prysm struct, skipping line")
			continue
		}
		ssz, err := marshaler.MarshalSSZ()
		if err != nil {
			log.WithFields(logrus.Fields{
				"test":    testName,
				"version": np.Version,
			}).WithError(err).Warn("Failed to SSZ-encode payload, skipping line")
			continue
		}
		jsonBytes, err := json.Marshal(np.Payload)
		if err != nil {
			log.WithFields(logrus.Fields{
				"test":    testName,
				"version": np.Version,
			}).WithError(err).Warn("Failed to JSON-encode payload, skipping line")
			continue
		}
		snap := snappy.Encode(nil, ssz)
		out.SSZFull = append(out.SSZFull, uint64(len(ssz)))
		out.SSZFullSnappy = append(out.SSZFullSnappy, uint64(len(snap)))
		out.SSZBAL = append(out.SSZBAL, balByteLen(log, testName, np.Payload.BlockAccessList))
		out.JSONFull = append(out.JSONFull, uint64(len(jsonBytes)))
		// JSONBAL is the hex string's length as it appears in the JSON
		// value (e.g. "0xab..." — chars only, not the surrounding quotes).
		out.JSONBAL = append(out.JSONBAL, uint64(len(np.Payload.BlockAccessList)))
	}

	for _, line := range lines {
		block, bal, ok := extractRethPayloadBytes(line)
		if !ok {
			continue
		}
		out.RLPFull = append(out.RLPFull, uint64(len(block)))
		out.RLPBAL = append(out.RLPBAL, uint64(len(bal)))
		out.RLPFullSnappy = append(out.RLPFullSnappy, uint64(len(snappy.Encode(nil, block))))
	}
	return out
}

func extractRethPayloadBytes(line string) ([]byte, []byte, bool) {
	var req newPayloadRequest
	if json.Unmarshal([]byte(line), &req) != nil || req.Method != "reth_newPayload" ||
		len(req.Params) == 0 {
		return nil, nil, false
	}

	var legacy string
	if json.Unmarshal(req.Params[0], &legacy) == nil {
		block, err := hex.DecodeString(strings.TrimPrefix(legacy, "0x"))
		return block, nil, err == nil
	}

	var input struct {
		Block string `json:"block"`
		BAL   string `json:"bal"`
	}
	if json.Unmarshal(req.Params[0], &input) != nil || input.Block == "" {
		return nil, nil, false
	}
	block, err := hex.DecodeString(strings.TrimPrefix(input.Block, "0x"))
	if err != nil {
		return nil, nil, false
	}
	bal, err := hex.DecodeString(strings.TrimPrefix(input.BAL, "0x"))
	if err != nil {
		return nil, nil, false
	}
	return block, bal, true
}

// balByteLen returns the byte length of a hex-encoded BlockAccessList. An
// empty or absent value returns 0; malformed hex logs a warning and returns 0.
func balByteLen(log logrus.FieldLogger, testName, balHex string) uint64 {
	if balHex == "" {
		return 0
	}
	if len(balHex) < 2 || balHex[:2] != "0x" {
		log.WithField("test", testName).Warn("BlockAccessList missing 0x prefix, treating as 0 bytes")
		return 0
	}
	raw, err := hex.DecodeString(balHex[2:])
	if err != nil {
		log.WithField("test", testName).WithError(err).Warn("Failed to hex-decode BlockAccessList, treating as 0 bytes")
		return 0
	}
	return uint64(len(raw))
}

// stepLinesForTest returns the JSON-RPC lines of a test step. For provider-
// backed steps it uses Provider.Lines(); for file-backed steps it reads the
// file from disk and splits on newlines. A read failure on a file-backed step
// is logged and returns an empty slice (zero payload-size fields).
func stepLinesForTest(log logrus.FieldLogger, step *StepFile) []string {
	if step == nil {
		return nil
	}
	if step.Provider != nil {
		return step.Provider.Lines()
	}
	if step.Path == "" {
		return nil
	}
	data, err := os.ReadFile(step.Path)
	if err != nil {
		log.WithError(err).WithField("path", step.Path).Warn("Failed to read step file for payload-size computation")
		return nil
	}
	return splitNonEmptyLines(string(data))
}

func splitNonEmptyLines(s string) []string {
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// StepKind names a step inside a test for the per-step line provider.
type StepKind string

const (
	StepKindSetup   StepKind = "setup"
	StepKindTest    StepKind = "test"
	StepKindCleanup StepKind = "cleanup"
)

// MergePayloadSizes attaches a PayloadSizes container to tests that don't
// already carry one. The lineProvider callback returns the JSON-RPC lines
// for a given (test, step) pair — callers can fetch from disk or from
// memory as appropriate. Returning nil/empty for a step skips that step.
//
// Tests that already carry any payload-size data are left alone
// (idempotent re-run).
func MergePayloadSizes(
	log logrus.FieldLogger,
	tests []SuiteTest,
	lineProvider func(testName string, step StepKind) []string,
) {
	for i := range tests {
		t := &tests[i]
		if t.PayloadSizes.HasData() {
			continue
		}
		var (
			merged  PayloadSizes
			anyData bool
		)
		for _, step := range []StepKind{StepKindSetup, StepKindTest, StepKindCleanup} {
			lines := lineProvider(t.Name, step)
			if len(lines) == 0 {
				continue
			}
			buckets := ComputePayloadSizeBuckets(log, t.Name, lines)
			if !buckets.HasData() {
				continue
			}
			b := buckets
			switch step {
			case StepKindSetup:
				merged.Setup = &b
			case StepKindTest:
				merged.Test = &b
			case StepKindCleanup:
				merged.Cleanup = &b
			}
			anyData = true
		}
		if anyData {
			m := merged
			t.PayloadSizes = &m
		}
	}
}
