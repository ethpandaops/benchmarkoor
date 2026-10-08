package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// tarEntryOverhead bounds what tar adds per member: a 512-byte header plus up
// to 511 bytes of padding.
const tarEntryOverhead = 1024

// client describes how one client's datadir is packed.
type client struct {
	// sub is the directory under the datadir whose contents are packed as the
	// archive root "./". Empty packs the datadir itself.
	sub string
	// exclude lists slash-separated globs, relative to the packed root, of
	// node identity, peer tables, locks and logs a published image must not
	// carry.
	exclude []string
}

// common is excluded for every client: node identity, the instance lock,
// peer tables, logs, a previous publish's _snapshot_* files and the snapshot
// downloader's .download-cache.
var common = []string{"nodekey", "LOCK", "nodes", "logs", "_snapshot_*", ".download-cache"}

var clients = map[string]client{
	// geth packs its instance dir <datadir>/geth (chaindata/, triedb/) with no
	// prefix, as snapshots.ethpandaops.io does: consumers extract it into
	// <datadir>/geth, where geth reads triedb/merkle.journal. Extracted flat, geth
	// still opens chaindata/ but misses the journal and rewinds its head.
	"geth":   {sub: "geth"},
	"erigon": {},
	"reth":   {exclude: []string{"discovery-secret", "known-peers.json"}},
	"besu":   {exclude: []string{"key"}},
	"ethrex": {exclude: []string{"node.key", "node_config.json"}},
	// nethermind: <datadir>/nethermind_db/<chain>/<store>; the archive root is
	// nethermind_db, so it holds mainnet/ as jochemnet's does and extracts into
	// <datadir>/nethermind_db (--Init.BaseDbPath=nethermind_db/mainnet).
	"nethermind": {sub: "nethermind_db", exclude: []string{"*/peers", "*/discoveryNodes"}},
}

// Clients lists the clients Pack knows how to pack.
func Clients() []string {
	return []string{"besu", "erigon", "ethrex", "geth", "nethermind", "reth"}
}

func lookup(name string) (client, error) {
	c, ok := clients[name]
	if !ok {
		return client{}, fmt.Errorf("unknown client %q (known: %s)", name, strings.Join(Clients(), ", "))
	}

	c.exclude = append(append([]string{}, common...), c.exclude...)

	return c, nil
}

// excluded reports whether rel, a slash-separated path relative to the packed
// root, matches one of c's exclude globs.
func (c client) excluded(rel string) bool {
	for _, g := range c.exclude {
		if ok, _ := path.Match(g, rel); ok {
			return true
		}
	}

	return false
}

// Manifest is the ordered list of archive members for a datadir.
type Manifest struct {
	// Dir is the directory tar runs in (-C).
	Dir string
	// Members are the archive member names in tar --sort=name order:
	// depth-first, each directory's entries sorted bytewise.
	Members []string
	// Bytes is the members' total regular-file size: the image's data size.
	Bytes int64
}

// MaxArchiveBytes bounds the uncompressed tar stream, and so the zstd stream
// within a few bytes per block.
func (m *Manifest) MaxArchiveBytes() int64 {
	return m.Bytes + int64(len(m.Members)+2)*tarEntryOverhead
}

// BuildManifest walks a client's datadir and returns what Pack archives.
func BuildManifest(clientName, datadir string) (*Manifest, error) {
	c, err := lookup(clientName)
	if err != nil {
		return nil, err
	}

	root := filepath.Join(datadir, c.sub)

	st, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("%s datadir: %w", clientName, err)
	}

	if !st.IsDir() {
		return nil, fmt.Errorf("%s datadir %s is not a directory", clientName, root)
	}

	m := &Manifest{Dir: root}

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}

		rel = filepath.ToSlash(rel)
		if rel != "." && c.excluded(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if rel == "." {
			m.Members = append(m.Members, ".")
		} else {
			m.Members = append(m.Members, "./"+rel)
		}

		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}

			m.Bytes += info.Size()
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}

	return m, nil
}

// Pack streams the manifest as tar | zstd. Read the stream to EOF, then call
// wait: it fails if either process did, including tar reporting a file that
// changed while it was read (the datadir was not stopped).
func Pack(ctx context.Context, m *Manifest, zstdLevel int) (io.Reader, func() error, error) {
	if zstdLevel < 1 || zstdLevel > 19 {
		return nil, nil, fmt.Errorf("zstd level %d outside 1-19", zstdLevel)
	}

	var list bytes.Buffer
	for _, name := range m.Members {
		list.WriteString(name)
		list.WriteByte(0)
	}

	tar := exec.CommandContext(ctx, "tar", "-C", m.Dir, "--null", "--no-recursion", "-T", "-", "-cf", "-")
	zstd := exec.CommandContext(ctx, "zstd", fmt.Sprintf("-%d", zstdLevel), "-T0", "-q", "-c")

	var tarErr, zstdErr bytes.Buffer

	tar.Stdin, tar.Stderr, zstd.Stderr = &list, &tarErr, &zstdErr

	tarOut, err := tar.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	zstd.Stdin = tarOut

	out, err := zstd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	if err := tar.Start(); err != nil {
		return nil, nil, fmt.Errorf("starting tar: %w", err)
	}

	if err := zstd.Start(); err != nil {
		_ = tar.Process.Kill()
		_ = tar.Wait()

		return nil, nil, fmt.Errorf("starting zstd: %w", err)
	}

	// zstd holds the pipe now; closing ours lets tar see EPIPE if zstd dies.
	_ = tarOut.Close()

	wait := func() error {
		errZstd := zstd.Wait()
		errTar := tar.Wait()

		if errTar != nil {
			return fmt.Errorf("tar: %w: %s", errTar, strings.TrimSpace(tarErr.String()))
		}

		if errZstd != nil {
			return fmt.Errorf("zstd: %w: %s", errZstd, strings.TrimSpace(zstdErr.String()))
		}

		return nil
	}

	return out, wait, nil
}
