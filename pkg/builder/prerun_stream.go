package builder

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// bundleWriter streams pre-run payloads straight to the .request bundle,
// holding one payload at a time instead of the whole set.
//
// The accumulate-then-sort-then-write path (extractFixturePayloads ->
// sortAndDedupPayloads -> writeRequestBundle) keeps every payload resident
// simultaneously, which is fine for a smoke-sized pre-run and fatal for a
// production one: a 10 GB fixture peaked at 35.5 GB RSS and was OOM-killed
// mid-write, truncating the bundle. See writeBundle for the details.
//
// Ordering: payloads must arrive in ascending block order. That already holds
// for a pre-run — the recorded gas-bump/funding/predeploy blocks precede the
// fill, and a --no-reset-between-tests fill emits its blocks in order — so
// rather than buffering everything to sort it, this enforces the invariant and
// fails loudly if it is ever violated.
// errPayloadOutOfOrder marks the streaming writer's ordering invariant being
// broken. It is distinct from a read failure because the two deserve opposite
// treatment: a fixture that could not be read is salvageable (bundle what was
// readable), whereas payloads arriving out of order mean the bundle cannot be
// assembled correctly at all, and continuing would write a plausible-looking
// prefix that silently omits everything after the break.
var errPayloadOutOfOrder = errors.New("pre-run payloads out of order")

type bundleWriter struct {
	path   string
	file   *os.File
	w      *bufio.Writer
	nextID int
	last   uint64
	seen   map[string]struct{}
	count  int

	// first/latest carry everything summarizeBundle needs, accumulated as
	// payloads go past so PreRunBundleInfo can be produced without keeping them.
	first  blockRef
	latest blockRef
}

func newBundleWriter(dir string) (*bundleWriter, error) {
	bundleDir := filepath.Join(dir, config.PreRunBundleSubdir)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating bundle dir: %w", err)
	}

	path := filepath.Join(bundleDir, preRunBundleFile)

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("creating bundle file: %w", err)
	}

	return &bundleWriter{
		path:   path,
		file:   f,
		w:      bufio.NewWriterSize(f, 1<<20),
		nextID: 1,
		seen:   make(map[string]struct{}),
	}, nil
}

// emit writes one payload's newPayload + forkchoiceUpdated pair. Duplicate
// blocks (same hash) are skipped, matching sortAndDedupPayloads.
func (b *bundleWriter) emit(p recordedPayload) error {
	ref, err := payloadBlockRef(&p)
	if err != nil {
		return fmt.Errorf("payload %d: %w", b.count, err)
	}

	number, ep := ref.number, ref

	if ep.hash != "" {
		if _, dup := b.seen[ep.hash]; dup {
			return nil
		}
	}

	if b.count > 0 && number <= b.last {
		return fmt.Errorf(
			"%w: block %d arrived after %d; the bundle writer streams in order and "+
				"cannot reorder (fixtures are walked in lexical path order, so a fill "+
				"whose files do not sort by block number needs the buffered path)",
			errPayloadOutOfOrder, number, b.last,
		)
	}

	npLine, fcuLine, err := payloadRequestLines(&p, b.nextID)
	if err != nil {
		return fmt.Errorf("building request lines for block %d: %w", number, err)
	}

	if _, err := fmt.Fprintln(b.w, npLine); err != nil {
		return fmt.Errorf("writing newPayload line: %w", err)
	}

	if _, err := fmt.Fprintln(b.w, fcuLine); err != nil {
		return fmt.Errorf("writing forkchoiceUpdated line: %w", err)
	}

	if ep.hash != "" {
		b.seen[ep.hash] = struct{}{}
	}

	if b.count == 0 {
		b.first = ref
	}

	b.latest = ref
	b.last = number
	b.nextID++
	b.count++

	return nil
}

// info builds the same PreRunBundleInfo summarizeBundle would, from the first
// and last payloads seen rather than from a retained slice.
func (b *bundleWriter) info() (*PreRunBundleInfo, error) {
	if b.count == 0 {
		return nil, fmt.Errorf("bundle has no payloads")
	}

	return &PreRunBundleInfo{
		Payloads: b.count,
		// The parent of the first payload: the snapshot head, one block below it.
		StartBlockNumber: b.first.number - 1,
		StartBlockHash:   b.first.parentHash,
		EndBlockNumber:   b.latest.number,
		EndBlockHash:     b.latest.hash,
	}, nil
}

// discard closes and removes the bundle file. Used when nothing was written: an
// empty bundle would only be mistaken for a usable prefix.
func (b *bundleWriter) discard() error {
	_ = b.close()

	return os.Remove(b.path)
}

// close flushes and closes the bundle file.
func (b *bundleWriter) close() error {
	if err := b.w.Flush(); err != nil {
		_ = b.file.Close()

		return fmt.Errorf("flushing bundle: %w", err)
	}

	return b.file.Close()
}

// streamFixturePayloads decodes a stateful fixture file incrementally, invoking
// fn once per engine payload. Only one payload is materialised at a time, so
// peak memory is independent of the fixture's size — the whole point of this
// path. A file that is not a fixture (e.g. a .meta report) is skipped silently,
// matching extractFixturePayloads.
func streamFixturePayloads(path string, fn func(recordedPayload) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening fixture %q: %w", path, err)
	}

	defer func() { _ = f.Close() }()

	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))

	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil // not a fixture object
	}

	for dec.More() {
		if _, err := dec.Token(); err != nil { // the test id
			return nil
		}

		tok, err := dec.Token()
		if err != nil || tok != json.Delim('{') {
			return nil
		}

		if err := streamFixtureCase(dec, fn); err != nil {
			return fmt.Errorf("fixture %q: %w", path, err)
		}

		if _, err := dec.Token(); err != nil { // closing '}' of the case
			return nil
		}
	}

	return nil
}

// streamFixtureCase walks one fixture case's fields, streaming the two payload
// arrays and skipping everything else.
func streamFixtureCase(dec *json.Decoder, fn func(recordedPayload) error) error {
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}

		name, _ := key.(string)

		switch name {
		case "setupEngineNewPayloads", "engineNewPayloads":
			if err := streamPayloadArray(dec, fn); err != nil {
				return err
			}
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return err
			}
		}
	}

	return nil
}

// streamPayloadArray decodes a payload array one element at a time.
func streamPayloadArray(dec *json.Decoder, fn func(recordedPayload) error) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}

	if tok != json.Delim('[') {
		return fmt.Errorf("expected array, got %v", tok)
	}

	for dec.More() {
		var e rawFixtureEnginePayload
		if err := dec.Decode(&e); err != nil {
			return err
		}

		if len(e.Params) == 0 || e.NewPayloadVersion == "" {
			continue // malformed entry, as appendFixturePayload skips
		}

		if err := fn(recordedPayload{
			Method: "engine_newPayloadV" + e.NewPayloadVersion,
			Params: e.Params,
		}); err != nil {
			return err
		}
	}

	_, err = dec.Token() // closing ']'

	return err
}

// streamFixtureDir streams every fixture under dir, in path order so the walk is
// deterministic.
func streamFixtureDir(dir string, fn func(recordedPayload) error) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}

		return streamFixturePayloads(path, fn)
	})
}
