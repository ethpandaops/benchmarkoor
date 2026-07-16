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
)

// preRunBundleFile is the replayable payload bundle a builder pre-run target
// writes into its output_dir: one recordedPayload (engine_newPayload) per line,
// ordered by block number. A replay target reads it from the builder's
// output_dir to advance its own datadir to the same head.
const preRunBundleFile = ".pre-run-payloads.jsonl"

// writePayloadBundle writes payloads as JSONL to <dir>/preRunBundleFile.
func writePayloadBundle(dir string, payloads []recordedPayload) error {
	path := filepath.Join(dir, preRunBundleFile)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating payload bundle: %w", err)
	}

	defer func() { _ = f.Close() }()

	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)

	for i := range payloads {
		if err := enc.Encode(&payloads[i]); err != nil {
			return fmt.Errorf("encoding payload %d: %w", i, err)
		}
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf("flushing payload bundle: %w", err)
	}

	return nil
}

// readPayloadBundle reads the JSONL payload bundle from <dir>/preRunBundleFile.
func readPayloadBundle(dir string) ([]recordedPayload, error) {
	path := filepath.Join(dir, preRunBundleFile)

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening payload bundle %q: %w", path, err)
	}

	defer func() { _ = f.Close() }()

	var payloads []recordedPayload

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024) // large blocks (big BALs)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var p recordedPayload
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			return nil, fmt.Errorf("parsing payload bundle line: %w", err)
		}

		payloads = append(payloads, p)
	}

	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading payload bundle: %w", err)
	}

	return payloads, nil
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
