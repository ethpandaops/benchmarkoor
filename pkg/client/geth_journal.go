package client

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/rlp"
)

const (
	// GethJournalFile is where geth's path-scheme state database writes its
	// in-memory layers on a clean stop, relative to the datadir. The next start
	// loads them back into memory.
	GethJournalFile = "geth/triedb/merkle.journal"

	// gethJournalVersion is the only journal layout ReadGethJournal parses
	// (triedb/pathdb journalVersion, written by geth v1.17.7).
	gethJournalVersion = 3

	// GethMaxDiffLayers is the number of recent blocks geth keeps as diff
	// layers (triedb/pathdb maxDiffLayers).
	GethMaxDiffLayers = 128
)

// GethJournal summarises a path-scheme journal: the diff layers, one per
// block, and the disk layer's write buffer, which holds the state of every
// older block that has not been flushed to the database yet.
type GethJournal struct {
	DiffLayers int
	FirstBlock uint64
	LastBlock  uint64

	// What the write buffer holds: tries with dirty nodes, accounts, and
	// accounts with dirty storage slots. All zero when it is empty.
	BufferTries    int
	BufferAccounts int
	BufferStorages int
}

// BufferEmpty reports whether everything below the diff layers is on disk.
func (j *GethJournal) BufferEmpty() bool {
	return j.BufferTries == 0 && j.BufferAccounts == 0 && j.BufferStorages == 0
}

// CheckGethJournal fails when the journal in dataDir holds the state of more
// than the last GethMaxDiffLayers blocks, i.e. when the write buffer below the
// diff layers is not empty: geth would serve that state from memory, so a
// compaction never reaches it. A datadir without a journal passes, and the
// returned journal is nil.
func CheckGethJournal(dataDir string) (*GethJournal, error) {
	path := filepath.Join(dataDir, GethJournalFile)

	j, err := ReadGethJournal(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	if j.DiffLayers > GethMaxDiffLayers || !j.BufferEmpty() {
		return j, fmt.Errorf(
			"%s holds the state of more than the last %d blocks, which geth loads back into memory"+
				" on start: %d diff layers (blocks %d-%d) over a write buffer of %d tries,"+
				" %d accounts and %d storages; flush it by running geth with --cache.gc=0 for"+
				" at least %d blocks before stopping it",
			path, GethMaxDiffLayers, j.DiffLayers, j.FirstBlock, j.LastBlock,
			j.BufferTries, j.BufferAccounts, j.BufferStorages, GethMaxDiffLayers,
		)
	}

	return j, nil
}

// ReadGethJournal parses a path-scheme journal (triedb/pathdb/journal.go):
// the version, the disk root, the disk layer (root, state id, nodes, states)
// and then one entry per diff layer, oldest first.
func ReadGethJournal(path string) (*GethJournal, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	s := rlp.NewStream(bufio.NewReaderSize(f, 1<<20), 0)

	version, err := s.Uint64()
	if err != nil {
		return nil, fmt.Errorf("%s: reading version: %w", path, err)
	}

	if version != gethJournalVersion {
		return nil, fmt.Errorf(
			"%s: journal version %d, only %d can be checked", path, version, gethJournalVersion,
		)
	}

	j := &GethJournal{}

	// Disk root, then the disk layer's root and state id.
	for _, what := range []string{"disk root", "disk layer root"} {
		if _, err := s.Bytes(); err != nil {
			return nil, fmt.Errorf("%s: reading %s: %w", path, what, err)
		}
	}

	if _, err := s.Uint64(); err != nil {
		return nil, fmt.Errorf("%s: reading disk layer id: %w", path, err)
	}

	if j.BufferTries, err = listLen(s); err != nil {
		return nil, fmt.Errorf("%s: reading buffered nodes: %w", path, err)
	}

	if j.BufferAccounts, j.BufferStorages, err = readStateSet(s); err != nil {
		return nil, fmt.Errorf("%s: reading buffered states: %w", path, err)
	}

	for {
		if _, err := s.Bytes(); errors.Is(err, io.EOF) {
			return j, nil
		} else if err != nil {
			return nil, fmt.Errorf("%s: reading diff layer %d root: %w", path, j.DiffLayers, err)
		}

		block, err := s.Uint64()
		if err != nil {
			return nil, fmt.Errorf("%s: reading diff layer %d block: %w", path, j.DiffLayers, err)
		}

		if err := skipDiffLayerBody(s); err != nil {
			return nil, fmt.Errorf("%s: reading diff layer %d (block %d): %w", path, j.DiffLayers, block, err)
		}

		if j.DiffLayers == 0 {
			j.FirstBlock = block
		}

		j.LastBlock = block
		j.DiffLayers++
	}
}

// skipDiffLayerBody consumes a diff layer's nodes, their optional origins
// (a list where the state set's leading bool would be), its state set and
// the state set's origins.
func skipDiffLayerBody(s *rlp.Stream) error {
	if _, err := s.Raw(); err != nil {
		return fmt.Errorf("nodes: %w", err)
	}

	kind, _, err := s.Kind()
	if err != nil {
		return err
	}

	if kind == rlp.List {
		if _, err := s.Raw(); err != nil {
			return fmt.Errorf("node origins: %w", err)
		}
	}

	if _, _, err := readStateSet(s); err != nil {
		return err
	}

	for _, what := range []string{"account origins", "storage origins"} {
		if _, err := s.Raw(); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
	}

	return nil
}

// readStateSet consumes a stateSet (raw-key flag, accounts, storages) and
// returns how many accounts and storages it holds.
func readStateSet(s *rlp.Stream) (accounts, storages int, err error) {
	if _, err := s.Bool(); err != nil {
		return 0, 0, fmt.Errorf("raw storage key flag: %w", err)
	}

	// struct{ AddrHashes []common.Hash; Accounts [][]byte }
	raw, err := s.Raw()
	if err != nil {
		return 0, 0, fmt.Errorf("accounts: %w", err)
	}

	fields, _, err := rlp.SplitList(raw)
	if err != nil {
		return 0, 0, fmt.Errorf("accounts: %w", err)
	}

	_, hashes, _, err := rlp.Split(fields)
	if err != nil {
		return 0, 0, fmt.Errorf("account hashes: %w", err)
	}

	if accounts, err = rlp.CountValues(hashes); err != nil {
		return 0, 0, fmt.Errorf("account hashes: %w", err)
	}

	if storages, err = listLen(s); err != nil {
		return 0, 0, fmt.Errorf("storages: %w", err)
	}

	return accounts, storages, nil
}

// listLen consumes a list and returns its number of elements.
func listLen(s *rlp.Stream) (int, error) {
	raw, err := s.Raw()
	if err != nil {
		return 0, err
	}

	content, _, err := rlp.SplitList(raw)
	if err != nil {
		return 0, err
	}

	return rlp.CountValues(content)
}
