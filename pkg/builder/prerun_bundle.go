package builder

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

const (
	// preRunBundleFile is the bundle file itself: newline-delimited JSON-RPC
	// request lines (an engine_newPayload + engine_forkchoiceUpdated pair per
	// block, ordered by block number). This is the same ".request" line format
	// the runner replays for fixture steps, so the runner consumes it as a
	// session-level pre-run step with no conversion.
	preRunBundleFile = "pre-run.request"
	// preRunBundleFCUMethod is the forkchoiceUpdated version emitted after each
	// newPayload. V3 (no payload attributes) is accepted across forks and was
	// validated advancing amsterdam datadirs.
	preRunBundleFCUMethod = "engine_forkchoiceUpdatedV3"
)

// writeRequestBundle writes payloads as JSON-RPC ".request" lines to
// <dir>/PreRunBundleSubdir/preRunBundleFile: for each block a newPayload request
// followed by a forkchoiceUpdated to that block's hash.
func writeRequestBundle(dir string, payloads []recordedPayload) (string, error) {
	bundleDir := filepath.Join(dir, config.PreRunBundleSubdir)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return "", fmt.Errorf("creating bundle dir: %w", err)
	}

	path := filepath.Join(bundleDir, preRunBundleFile)

	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("creating bundle file: %w", err)
	}

	defer func() { _ = f.Close() }()

	w := bufio.NewWriter(f)

	for i := range payloads {
		npLine, fcuLine, lerr := payloadRequestLines(&payloads[i], i+1)
		if lerr != nil {
			return "", fmt.Errorf("building request lines for payload %d: %w", i, lerr)
		}

		if _, err := fmt.Fprintln(w, npLine); err != nil {
			return "", fmt.Errorf("writing newPayload line: %w", err)
		}

		if _, err := fmt.Fprintln(w, fcuLine); err != nil {
			return "", fmt.Errorf("writing forkchoiceUpdated line: %w", err)
		}
	}

	if err := w.Flush(); err != nil {
		return "", fmt.Errorf("flushing bundle: %w", err)
	}

	return path, nil
}

// payloadRequestLines returns the engine_newPayload + engine_forkchoiceUpdated
// JSON-RPC request lines for one recorded payload (id used for both).
func payloadRequestLines(p *recordedPayload, id int) (npLine, fcuLine string, err error) {
	np, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": p.Method, "params": p.Params,
	})
	if err != nil {
		return "", "", fmt.Errorf("marshaling newPayload: %w", err)
	}

	if len(p.Params) == 0 {
		return "", "", fmt.Errorf("payload has no params")
	}

	hash, err := payloadBlockHash(p.Params[0])
	if err != nil {
		return "", "", err
	}

	fcu, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": preRunBundleFCUMethod,
		"params": []any{
			map[string]string{"headBlockHash": hash, "safeBlockHash": hash, "finalizedBlockHash": hash},
			nil,
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("marshaling forkchoiceUpdated: %w", err)
	}

	return string(np), string(fcu), nil
}

// rawFixtureEnginePayload is one engine_newPayload entry in a stateful fixture.
// newPayloadVersion is a decimal string in the fixture (e.g. "5"), matching
// eest.EngineNewPayloadRaw.
type rawFixtureEnginePayload struct {
	Params            []json.RawMessage `json:"params"`
	NewPayloadVersion string            `json:"newPayloadVersion"`
}

// rawFixtureFile is the minimal shape of a stateful fixture file: a map of test
// name to its setup + benchmark engine payloads.
type rawFixtureFile map[string]struct {
	SetupEngineNewPayloads []rawFixtureEnginePayload `json:"setupEngineNewPayloads"`
	EngineNewPayloads      []rawFixtureEnginePayload `json:"engineNewPayloads"`
}

// extractFixturePayloads walks the stateful fixtures under fixturesDir and
// collects every engine_newPayload (setup + benchmark) as a recordedPayload.
// With --no-reset-between-tests these are the blocks the fill applied on top of
// the funding head, so together with the recorded bump/funding payloads they
// form the linear chain to the pre-run head.
func extractFixturePayloads(fixturesDir string) ([]recordedPayload, error) {
	var out []recordedPayload

	err := filepath.WalkDir(fixturesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("reading fixture %q: %w", path, readErr)
		}

		var ff rawFixtureFile
		if json.Unmarshal(data, &ff) != nil {
			// Not a fixture file (e.g. a .meta report); skip.
			return nil
		}

		for _, fx := range ff {
			for _, e := range fx.SetupEngineNewPayloads {
				out = appendFixturePayload(out, e)
			}

			for _, e := range fx.EngineNewPayloads {
				out = appendFixturePayload(out, e)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

// appendFixturePayload converts a fixture engine payload to a recordedPayload
// and appends it, skipping malformed entries (no params / version).
func appendFixturePayload(out []recordedPayload, e rawFixtureEnginePayload) []recordedPayload {
	if len(e.Params) == 0 || e.NewPayloadVersion == "" {
		return out
	}

	return append(out, recordedPayload{
		Method: "engine_newPayloadV" + e.NewPayloadVersion,
		Params: e.Params,
	})
}

// sortAndDedupPayloads orders payloads by their block number and drops duplicate
// blocks (same block hash), yielding the linear chain to replay.
func sortAndDedupPayloads(payloads []recordedPayload) ([]recordedPayload, error) {
	type keyed struct {
		p      recordedPayload
		number uint64
		hash   string
	}

	keyedPayloads := make([]keyed, 0, len(payloads))

	for i := range payloads {
		if len(payloads[i].Params) == 0 {
			return nil, fmt.Errorf("payload %d has no params", i)
		}

		var ep struct {
			BlockNumber string `json:"blockNumber"`
			BlockHash   string `json:"blockHash"`
		}
		if err := json.Unmarshal(payloads[i].Params[0], &ep); err != nil {
			return nil, fmt.Errorf("parsing payload %d execution payload: %w", i, err)
		}

		number, err := strconv.ParseUint(strings.TrimPrefix(ep.BlockNumber, "0x"), 16, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing payload %d blockNumber %q: %w", i, ep.BlockNumber, err)
		}

		keyedPayloads = append(keyedPayloads, keyed{p: payloads[i], number: number, hash: ep.BlockHash})
	}

	sort.SliceStable(keyedPayloads, func(i, j int) bool {
		return keyedPayloads[i].number < keyedPayloads[j].number
	})

	out := make([]recordedPayload, 0, len(keyedPayloads))
	seen := make(map[string]bool, len(keyedPayloads))

	for _, k := range keyedPayloads {
		if k.hash != "" && seen[k.hash] {
			continue
		}

		seen[k.hash] = true

		out = append(out, k.p)
	}

	return out, nil
}
