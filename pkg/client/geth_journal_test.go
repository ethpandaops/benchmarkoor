package client

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The types below mirror what geth v1.17.7's triedb/pathdb journal() writes,
// so the tests build journals the way geth does.

type testJournalNode struct {
	Path []byte
	Blob []byte
}

type testJournalNodes struct {
	Owner common.Hash
	Nodes []testJournalNode
}

type testAccounts struct {
	AddrHashes []common.Hash
	Accounts   [][]byte
}

type testStorage struct {
	AddrHash common.Hash
	Keys     []common.Hash
	Vals     [][]byte
}

type testOriginAccounts struct {
	Addresses []common.Address
	Accounts  [][]byte
}

type testOriginStorage struct {
	Address common.Address
	Keys    []common.Hash
	Vals    [][]byte
}

type testLayerState struct {
	nodes    []testJournalNodes
	accounts testAccounts
	storages []testStorage
}

func dirtyState(i byte) testLayerState {
	return testLayerState{
		nodes:    []testJournalNodes{{Nodes: []testJournalNode{{Path: []byte{i}, Blob: []byte{0xc0}}}}},
		accounts: testAccounts{AddrHashes: []common.Hash{{i}}, Accounts: [][]byte{{i}}},
		storages: []testStorage{{AddrHash: common.Hash{i}, Keys: []common.Hash{{1}}, Vals: [][]byte{{2}}}},
	}
}

func emptyState() testLayerState {
	return testLayerState{nodes: []testJournalNodes{}, accounts: testAccounts{}, storages: []testStorage{}}
}

type testJournal struct {
	version  uint64
	buffer   testLayerState
	diffs    int
	noOrigin bool // pre-origin layout: no node origins after the nodes
}

func (j testJournal) encode(t *testing.T) []byte {
	t.Helper()

	var b bytes.Buffer

	enc := func(v any) { require.NoError(t, rlp.Encode(&b, v)) }

	state := func(s testLayerState) {
		enc(false)
		enc(s.accounts)
		enc(s.storages)
	}

	enc(j.version)
	enc(common.Hash{0xd1}) // disk root
	enc(common.Hash{0xd1}) // disk layer root
	enc(uint64(1000))      // disk layer state id
	enc(j.buffer.nodes)
	state(j.buffer)

	for i := range j.diffs {
		s := dirtyState(byte(i))

		enc(common.Hash{byte(i), 0xff})
		enc(uint64(500 + i))
		enc(s.nodes)

		if !j.noOrigin {
			enc(s.nodes)
		}

		state(s)
		enc(testOriginAccounts{Addresses: []common.Address{{byte(i)}}, Accounts: [][]byte{{}}})
		enc([]testOriginStorage{{Address: common.Address{byte(i)}, Keys: []common.Hash{{1}}, Vals: [][]byte{{}}}})
	}

	return b.Bytes()
}

func writeJournal(t *testing.T, data []byte) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, GethJournalFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, data, 0o644))

	return dir
}

func TestCheckGethJournal(t *testing.T) {
	t.Run("the last 128 blocks over an empty buffer pass", func(t *testing.T) {
		dir := writeJournal(t, testJournal{version: 3, buffer: emptyState(), diffs: 128}.encode(t))

		j, err := CheckGethJournal(dir)
		require.NoError(t, err)
		assert.Equal(t, &GethJournal{DiffLayers: 128, FirstBlock: 500, LastBlock: 627}, j)
	})

	t.Run("fewer blocks than that pass", func(t *testing.T) {
		dir := writeJournal(t, testJournal{version: 3, buffer: emptyState(), diffs: 3}.encode(t))

		j, err := CheckGethJournal(dir)
		require.NoError(t, err)
		assert.Equal(t, 3, j.DiffLayers)
	})

	t.Run("a disk layer alone passes", func(t *testing.T) {
		dir := writeJournal(t, testJournal{version: 3, buffer: emptyState()}.encode(t))

		j, err := CheckGethJournal(dir)
		require.NoError(t, err)
		assert.Zero(t, j.DiffLayers)
	})

	t.Run("an unflushed write buffer fails", func(t *testing.T) {
		dir := writeJournal(t, testJournal{version: 3, buffer: dirtyState(7), diffs: 128}.encode(t))

		j, err := CheckGethJournal(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "128 diff layers (blocks 500-627) over a write buffer of 1 tries, 1 accounts and 1 storages")
		assert.Equal(t, 1, j.BufferTries)
	})

	t.Run("a buffer holding only accounts fails", func(t *testing.T) {
		buffer := emptyState()
		buffer.accounts = dirtyState(1).accounts
		dir := writeJournal(t, testJournal{version: 3, buffer: buffer, diffs: 1}.encode(t))

		_, err := CheckGethJournal(dir)
		require.Error(t, err)
	})

	t.Run("more than 128 diff layers fail", func(t *testing.T) {
		dir := writeJournal(t, testJournal{version: 3, buffer: emptyState(), diffs: 129}.encode(t))

		_, err := CheckGethJournal(dir)
		require.ErrorContains(t, err, "129 diff layers")
	})

	t.Run("diff layers without node origins parse", func(t *testing.T) {
		dir := writeJournal(t, testJournal{version: 3, buffer: emptyState(), diffs: 5, noOrigin: true}.encode(t))

		j, err := CheckGethJournal(dir)
		require.NoError(t, err)
		assert.Equal(t, 5, j.DiffLayers)
	})

	t.Run("no journal passes", func(t *testing.T) {
		j, err := CheckGethJournal(t.TempDir())
		require.NoError(t, err)
		assert.Nil(t, j)
	})

	t.Run("another journal version is not guessed at", func(t *testing.T) {
		dir := writeJournal(t, testJournal{version: 2, buffer: emptyState()}.encode(t))

		_, err := CheckGethJournal(dir)
		require.ErrorContains(t, err, "journal version 2, only 3 can be checked")
	})

	t.Run("a truncated journal fails", func(t *testing.T) {
		data := testJournal{version: 3, buffer: emptyState(), diffs: 2}.encode(t)
		dir := writeJournal(t, data[:len(data)-5])

		_, err := CheckGethJournal(dir)
		require.ErrorContains(t, err, "diff layer 1")
	})
}

func TestGethVerifiesItsJournal(t *testing.T) {
	spec, err := NewRegistry().Get(ClientGeth)
	require.NoError(t, err)

	verify := spec.DBMaintenanceCommands("/data").Verify
	require.NotNil(t, verify)

	summary, err := verify(t.TempDir())
	require.NoError(t, err)
	assert.Contains(t, summary, "no geth/triedb/merkle.journal")

	dir := writeJournal(t, testJournal{version: 3, buffer: emptyState(), diffs: 128}.encode(t))
	summary, err = verify(dir)
	require.NoError(t, err)
	assert.Equal(t, "geth/triedb/merkle.journal: 128 diff layers (blocks 500-627), write buffer empty", summary)
}
