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

// PayloadSizes aggregates the per-test byte counts the UI surfaces.
type PayloadSizes struct {
	PayloadBytes uint64 // SSZ-encoded executionPayload (BAL inline)
	SnappyBytes  uint64 // snappy compression of the SSZ bytes
	BALBytes     uint64 // hex-decoded BlockAccessList content
}

// ComputePayloadSizes parses each engine_newPayloadV* line in the test step,
// SSZ-encodes the executionPayload using Prysm's fork-specific struct, snappy-
// compresses the result, and decodes the inline BlockAccessList. It returns
// the sum of those three quantities across all newPayload lines in the step.
//
// Single-line errors (unknown fork, malformed hex) are logged with the test
// name and do not abort the computation. The function never returns an error;
// a fully-failing test step yields zero values for all three fields.
func ComputePayloadSizes(log logrus.FieldLogger, testName string, lines []string) PayloadSizes {
	var out PayloadSizes
	for _, np := range ExtractNewPayloadLines(lines) {
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
		out.PayloadBytes += uint64(len(ssz))
		snap := snappy.Encode(nil, ssz)
		out.SnappyBytes += uint64(len(snap))
		out.BALBytes += balByteLen(log, testName, np.Payload.BlockAccessList)
	}
	return out
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
// file from disk and splits on newlines.
func stepLinesForTest(step *StepFile) []string {
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

// MergePayloadSizes populates the three payload-size fields on tests that
// currently have all-zero values. The lineProvider callback returns the
// test-step lines for a given test name (used so callers can fetch from
// disk or from memory as appropriate).
//
// Tests with any non-zero size field are left alone (idempotent re-run).
func MergePayloadSizes(log logrus.FieldLogger, tests []SuiteTest, lineProvider func(name string) []string) {
	for i := range tests {
		t := &tests[i]
		if t.PayloadSizeBytes != 0 || t.PayloadSizeBytesSnappy != 0 || t.BALSizeBytes != 0 {
			continue
		}
		lines := lineProvider(t.Name)
		if len(lines) == 0 {
			continue
		}
		sizes := ComputePayloadSizes(log, t.Name, lines)
		t.PayloadSizeBytes = sizes.PayloadBytes
		t.PayloadSizeBytesSnappy = sizes.SnappyBytes
		t.BALSizeBytes = sizes.BALBytes
	}
}
