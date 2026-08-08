package builder

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// readRequestLines reads a newline-delimited .request bundle file, returning its
// non-empty JSON-RPC request lines (an engine_newPayload + forkchoiceUpdated
// pair per block, in order).
func readRequestLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	raw := strings.Split(string(data), "\n")
	lines := make([]string, 0, len(raw))

	for _, line := range raw {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}

	return lines, nil
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

// preRunBundleMetaFile sits beside the bundle and records what it contains, so
// consumers (the build summary, the GitHub action) can describe a bundle without
// re-parsing a multi-hundred-MB request file. Written whenever the bundle is,
// and left in place by a skipped rebuild — so it always describes the bundle
// currently on disk.
const preRunBundleMetaFile = "pre-run.meta.json"

// PreRunBundleInfo describes a recorded replay bundle.
//
// StartBlock is the block the bundle attaches TO — the parent of its first
// payload, i.e. the snapshot head it was recorded against — not its first
// payload. That is the field that matters when deciding whether a bundle can be
// replayed onto a given snapshot: a head mismatch there fails on the first
// payload. EndBlock is the head the chain reaches once the bundle is replayed.
//
// Payloads is counted, not derived from EndBlock-StartBlock: the two agree only
// while the bundle is contiguous, and a disagreement is worth being able to see.
type PreRunBundleInfo struct {
	Payloads         int    `json:"payloads"`
	StartBlockNumber uint64 `json:"start_block_number"`
	StartBlockHash   string `json:"start_block_hash"`
	EndBlockNumber   uint64 `json:"end_block_number"`
	EndBlockHash     string `json:"end_block_hash"`
}

// Contiguous reports whether the bundle covers an unbroken block range, i.e.
// every block from StartBlock+1 to EndBlock has exactly one payload.
func (i *PreRunBundleInfo) Contiguous() bool {
	return uint64(i.Payloads) == i.EndBlockNumber-i.StartBlockNumber
}

// summarizeBundle derives a PreRunBundleInfo from block-ordered payloads.
func summarizeBundle(ordered []recordedPayload) (*PreRunBundleInfo, error) {
	if len(ordered) == 0 {
		return nil, fmt.Errorf("bundle has no payloads")
	}

	first, err := payloadBlockRef(&ordered[0])
	if err != nil {
		return nil, fmt.Errorf("parsing first payload: %w", err)
	}

	last, err := payloadBlockRef(&ordered[len(ordered)-1])
	if err != nil {
		return nil, fmt.Errorf("parsing last payload: %w", err)
	}

	return &PreRunBundleInfo{
		Payloads: len(ordered),
		// The parent of the first payload: the snapshot head, one block below it.
		StartBlockNumber: first.number - 1,
		StartBlockHash:   first.parentHash,
		EndBlockNumber:   last.number,
		EndBlockHash:     last.hash,
	}, nil
}

// blockRef is the identity a bundle payload contributes to PreRunBundleInfo.
type blockRef struct {
	number     uint64
	hash       string
	parentHash string
}

// payloadBlockRef extracts a payload's block number, hash and parent hash.
func payloadBlockRef(p *recordedPayload) (blockRef, error) {
	if len(p.Params) == 0 {
		return blockRef{}, fmt.Errorf("payload has no params")
	}

	var ep struct {
		BlockNumber string `json:"blockNumber"`
		BlockHash   string `json:"blockHash"`
		ParentHash  string `json:"parentHash"`
	}
	if err := json.Unmarshal(p.Params[0], &ep); err != nil {
		return blockRef{}, fmt.Errorf("parsing execution payload: %w", err)
	}

	number, err := strconv.ParseUint(strings.TrimPrefix(ep.BlockNumber, "0x"), 16, 64)
	if err != nil {
		return blockRef{}, fmt.Errorf("parsing blockNumber %q: %w", ep.BlockNumber, err)
	}

	return blockRef{number: number, hash: ep.BlockHash, parentHash: ep.ParentHash}, nil
}

// writeBundleMeta persists info beside the bundle in dir's PreRunBundleSubdir.
func writeBundleMeta(dir string, info *PreRunBundleInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling bundle meta: %w", err)
	}

	path := filepath.Join(dir, config.PreRunBundleSubdir, preRunBundleMetaFile)
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing bundle meta: %w", err)
	}

	return nil
}

// Derived reports whether the info was recovered from the bundle file rather
// than read from a sidecar. A derived info knows only its end block: the start
// block and payload count are not recoverable without reading the whole file,
// which is the cost the sidecar exists to avoid.
func (i *PreRunBundleInfo) Derived() bool {
	return i.StartBlockHash == ""
}

// ReadPreRunBundleInfo describes the bundle in bundleParentDir (a pre_runs
// target's BundleParentDir). It prefers the sidecar written beside the bundle
// and falls back to reading the end block out of the bundle itself, so a bundle
// produced before the sidecar existed — an older CI artifact, say — still gets
// the "already applied, skip the replay" fast path.
//
// Returns nil without error when there is no bundle at all: a replay-only target
// records none.
func ReadPreRunBundleInfo(bundleParentDir string) (*PreRunBundleInfo, error) {
	if bundleParentDir == "" {
		return nil, nil
	}

	dir := filepath.Join(bundleParentDir, config.PreRunBundleSubdir)
	path := filepath.Join(dir, preRunBundleMetaFile)

	data, err := os.ReadFile(path)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return deriveBundleInfo(filepath.Join(dir, preRunBundleFile))
	case err != nil:
		return nil, fmt.Errorf("reading bundle meta %q: %w", path, err)
	}

	var info PreRunBundleInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parsing bundle meta %q: %w", path, err)
	}

	return &info, nil
}

// bundleTailChunk is how much of the bundle's tail is read looking for its last
// engine_newPayload. A single payload line carries a whole block, so lines run to
// hundreds of KB on a large-gas chain; the window doubles up to bundleTailMax
// when one line does not fit.
const (
	bundleTailChunk = 1 << 23 // 8 MiB
	bundleTailMax   = 1 << 29 // 512 MiB
)

// deriveBundleInfo recovers the bundle's end block by scanning backwards from the
// end of the file for the last engine_newPayload, so no sidecar is needed and a
// multi-GB bundle is never streamed in full. Returns nil when the file is absent.
func deriveBundleInfo(bundlePath string) (*PreRunBundleInfo, error) {
	f, err := os.Open(bundlePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("opening bundle %q: %w", bundlePath, err)
	}

	defer func() { _ = f.Close() }()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, fmt.Errorf("sizing bundle %q: %w", bundlePath, err)
	}

	for window := int64(bundleTailChunk); ; window *= 2 {
		from := size - window
		if from < 0 {
			from = 0
		}

		buf := make([]byte, size-from)
		if _, err := f.ReadAt(buf, from); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("reading bundle tail %q: %w", bundlePath, err)
		}

		lines := strings.Split(string(buf), "\n")

		// The first line is truncated unless the window reached the file start.
		if from > 0 && len(lines) > 0 {
			lines = lines[1:]
		}

		if ref, ok := lastPayloadRef(lines); ok {
			return &PreRunBundleInfo{
				EndBlockNumber: ref.number,
				EndBlockHash:   ref.hash,
			}, nil
		}

		if from == 0 {
			return nil, fmt.Errorf("bundle %q contains no engine_newPayload", bundlePath)
		}

		if window >= bundleTailMax {
			return nil, fmt.Errorf(
				"no engine_newPayload in the last %d bytes of bundle %q", window, bundlePath,
			)
		}
	}
}

// lastPayloadRef returns the block the last engine_newPayload line refers to.
func lastPayloadRef(lines []string) (blockRef, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.Unmarshal([]byte(line), &req) != nil {
			continue
		}

		if !strings.HasPrefix(req.Method, "engine_newPayload") {
			continue
		}

		ref, err := payloadBlockRef(&recordedPayload{Method: req.Method, Params: req.Params})
		if err != nil {
			continue
		}

		return ref, true
	}

	return blockRef{}, false
}
