package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/sirupsen/logrus"
)

// TestOpcodesFilename is the basename of the per-run aggregated opcodes
// dump written under the run results directory.
const TestOpcodesFilename = "test-opcodes.json"

// opcodeCountTracerJS is the JavaScript tracer passed to
// debug_traceBlockByNumber. It increments a per-opcode counter for
// every executed step and returns the final map. Compatible with
// clients that support `eth.JSTracerKind` style tracers (geth,
// erigon, nethermind via JS-tracer support, etc.).
const opcodeCountTracerJS = `{ counts: {}, ` +
	`step: function(log){ var op = log.op.toString(); this.counts[op] = (this.counts[op]||0)+1 }, ` +
	`fault: function(){}, ` +
	`result: function(){ return this.counts } }`

// debugTraceBlockResponse models the relevant slice of the
// debug_traceBlockByNumber response we care about. Each entry is one
// transaction in the block.
type debugTraceBlockResponse struct {
	Result []debugTraceBlockEntry `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// debugTraceBlockEntry is the per-tx trace entry. The opcode-count
// tracer returns a map[string]int as `result`.
type debugTraceBlockEntry struct {
	TxHash string         `json:"txHash"`
	Result map[string]int `json:"result"`
}

// extractTestOpcodes runs debug_traceBlockByNumber against every
// engine_newPayload* line in the test step and aggregates the per-tx
// opcode counts into a single map per newPayload (uppercased). Each
// newPayload becomes one entry in the per-test array stored on the
// executor. Errors are logged but never abort the test; opcode
// extraction is best-effort.
func (e *executor) extractTestOpcodes(
	ctx context.Context,
	opts *ExecuteOptions,
	test *TestWithSteps,
	log logrus.FieldLogger,
) {
	if test.Test == nil || opts.RPCEndpoint == "" {
		return
	}

	cfg := opts.OpcodeExtraction
	if cfg == nil || !cfg.Enabled {
		return
	}

	lines, err := readStepFileLines(test.Test)
	if err != nil {
		log.WithError(err).Warn("opcode_extraction: failed to read test step lines")

		return
	}

	// Pre-count newPayload* lines so progress logs can show "i/total"
	// instead of just the absolute line number, which is more useful
	// when a step has interleaved FCUs we skip over.
	totalPayloads := 0

	for _, l := range lines {
		if m, err := extractMethod(l); err == nil && strings.HasPrefix(m, "engine_newPayload") {
			totalPayloads++
		}
	}

	if totalPayloads == 0 {
		return
	}

	timeout := cfg.EffectiveTimeout()

	log.WithFields(logrus.Fields{
		"payloads": totalPayloads,
		"timeout":  timeout,
	}).Info("Extracting opcode counts")

	var perPayload []map[string]int

	startedAll := time.Now()
	processed := 0

	for lineNum, line := range lines {
		select {
		case <-ctx.Done():
			log.Warn("opcode_extraction: context cancelled mid-extraction")

			return
		default:
		}

		method, err := extractMethod(line)
		if err != nil {
			continue
		}

		if !strings.HasPrefix(method, "engine_newPayload") {
			continue
		}

		blockNum, ok := extractBlockNumber(line)
		if !ok {
			log.WithField("line", lineNum+1).Debug(
				"opcode_extraction: skipping newPayload without parseable blockNumber",
			)

			continue
		}

		blockNumberHex := fmt.Sprintf("0x%x", blockNum)
		processed++

		callLog := log.WithFields(logrus.Fields{
			"block_number": blockNumberHex,
			"pos":          fmt.Sprintf("%d/%d", processed, totalPayloads),
		})

		traceStart := time.Now()

		counts, err := e.traceBlockOpcodes(ctx, opts.RPCEndpoint, blockNumberHex, timeout)
		if err != nil {
			callLog.WithError(err).Warn("opcode_extraction: trace failed")

			continue
		}

		callLog.WithFields(logrus.Fields{
			"opcodes":  len(counts),
			"duration": time.Since(traceStart),
		}).Info("Traced block opcodes")

		perPayload = append(perPayload, counts)
	}

	log.WithFields(logrus.Fields{
		"payloads_traced": len(perPayload),
		"payloads_total":  totalPayloads,
		"duration":        time.Since(startedAll),
	}).Info("Opcode extraction completed")

	if len(perPayload) == 0 {
		return
	}

	e.opcodesMu.Lock()
	if e.testOpcodes == nil {
		e.testOpcodes = make(map[string][]map[string]int)
	}

	e.testOpcodes[test.Name] = append(e.testOpcodes[test.Name], perPayload...)
	// Snapshot under the lock so the file write below sees a consistent
	// view, then drop the lock before doing I/O.
	snapshot := make(map[string][]map[string]int, len(e.testOpcodes))
	for k, v := range e.testOpcodes {
		snapshot[k] = v
	}
	e.opcodesMu.Unlock()

	// Flush the cumulative map after every successful extraction so a
	// cancelled run still leaves test-opcodes.json on disk with all the
	// tests that finished before the cancel landed. The end-of-run write
	// in ExecuteTests stays as a final safety net.
	if err := WriteTestOpcodes(opts.ResultsDir, snapshot, e.cfg.ResultsOwner); err != nil {
		log.WithError(err).Warn("opcode_extraction: failed to flush test-opcodes.json")
	}
}

// traceBlockOpcodes calls debug_traceBlockByNumber with the JS
// opcode-count tracer and returns the summed (uppercase) opcode counts
// across all transactions in the block.
func (e *executor) traceBlockOpcodes(
	ctx context.Context,
	endpoint, blockNumberHex string,
	timeout time.Duration,
) (map[string]int, error) {
	tracerArg := map[string]any{
		"tracer":          opcodeCountTracerJS,
		"timeout":         timeout.String(),
		"borTraceEnabled": false,
	}

	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"debug_traceBlockByNumber","params":[%q,%s]}`,
		blockNumberHex, mustMarshal(tracerArg),
	)

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rawResp, err := executeSimpleRPC(callCtx, endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("rpc: %w", err)
	}

	return aggregateBlockOpcodes(rawResp)
}

// aggregateBlockOpcodes parses a debug_traceBlockByNumber response,
// sums per-tx opcode counts into a single map, and uppercases the
// opcode names. Returns an error when the response carries a JSON-RPC
// error, can't be parsed, or contains no traces.
func aggregateBlockOpcodes(rawResp string) (map[string]int, error) {
	var resp debugTraceBlockResponse
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		return nil, fmt.Errorf("parse trace response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	if len(resp.Result) == 0 {
		return nil, fmt.Errorf("trace response had no entries")
	}

	out := make(map[string]int, 32)

	for _, entry := range resp.Result {
		for op, count := range entry.Result {
			out[strings.ToUpper(op)] += count
		}
	}

	return out, nil
}

// WriteTestOpcodes writes the aggregated per-test opcode map to
// resultsDir/test-opcodes.json. A nil/empty map is a no-op.
func WriteTestOpcodes(
	resultsDir string,
	opcodes map[string][]map[string]int,
	owner *fsutil.OwnerConfig,
) error {
	if len(opcodes) == 0 {
		return nil
	}

	data, err := json.MarshalIndent(opcodes, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling test opcodes: %w", err)
	}

	path := filepath.Join(resultsDir, TestOpcodesFilename)
	if err := fsutil.WriteFile(path, data, 0644, owner); err != nil {
		return fmt.Errorf("writing %s: %w", TestOpcodesFilename, err)
	}

	return nil
}

// readStepFileLines reads non-empty trimmed lines from a step file or
// provider. Mirrors the loop in runStepFromFile but exposed as a
// standalone helper so opcode extraction can scan the test step
// without re-running it.
func readStepFileLines(step *StepFile) ([]string, error) {
	if step.Provider != nil {
		return step.Provider.Lines(), nil
	}

	file, err := os.Open(step.Path)
	if err != nil {
		return nil, fmt.Errorf("opening step file: %w", err)
	}

	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)

	var lines []string

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				lines = append(lines, trimmed)
			}
		}

		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, fmt.Errorf("reading step file: %w", err)
		}
	}

	return lines, nil
}

// mustMarshal is a tiny helper for embedding a small Go value as JSON
// inside a literal string. Panics only on programmer error (the input
// shape is fully under our control).
func mustMarshal(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("opcode_extraction: marshal: %v", err))
	}

	return string(data)
}
