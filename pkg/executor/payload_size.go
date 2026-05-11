package executor

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/eest"
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
