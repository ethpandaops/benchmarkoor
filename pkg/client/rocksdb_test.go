package client

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRocksDBClientsCompactWithLdb(t *testing.T) {
	for _, ct := range []ClientType{ClientNethermind, ClientBesu} {
		spec, err := NewRegistry().Get(ct)
		require.NoError(t, err)

		cmds := spec.DBMaintenanceCommands("/data")
		require.NotNil(t, cmds, ct)

		assert.Equal(t, RocksDBLdbImage, cmds.CompactImage, ct)
		assert.Equal(t, []string{"sh", "-c", rocksDBCompactScript, "rocksdb-compact", "/data"}, cmds.Compact, ct)
		assert.Empty(t, cmds.Prepare, ct)
		assert.Empty(t, cmds.Inspect, ct)
		assert.Nil(t, cmds.Verify, ct)
		assert.True(t, SupportsDBCompaction(ct), ct)
	}
}

// stubLdb answers list_column_families with `listing` (raw bytes, as ldb
// prints them), get_property levelstats with $LDB_LEVEL0 files at level 0 and
// two at level 6, and records every compact argv,
// NUL-separated with one line per call, in $LDB_CALLS.
const stubLdb = `#!/bin/sh
for a in "$@"; do
	case $a in
	list_column_families) printf '%s' "$LDB_LISTING"; exit 0 ;;
	get_property)
		case "$*" in
		*levelstats*) printf 'rocksdb.levelstats: Level Files Size(MB)\n--------------------\n  0        %s        0\n  6        2       10\n' "$LDB_LEVEL0" ;;
		*total-sst-files-size*) echo "rocksdb.total-sst-files-size: 4096" ;;
		esac
		exit 0 ;;
	compact) for b in "$@"; do printf '%s\0' "$b"; done >>"$LDB_CALLS"; echo >>"$LDB_CALLS"; exit 0 ;;
	esac
done
exit 3
`

type ldbStub struct {
	datadir string
	calls   string
	env     []string
}

func newLdbStub(t *testing.T, listing string, dbs ...string) *ldbStub {
	t.Helper()

	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "ldb"), []byte(stubLdb), 0o755))

	s := &ldbStub{datadir: t.TempDir(), calls: filepath.Join(t.TempDir(), "calls")}

	for _, db := range dbs {
		dir := filepath.Join(s.datadir, db)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "OPTIONS-000007"), []byte("[CFOptions \"default\"]\n  merge_operator=nullptr\n"), 0o644))
	}

	s.env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"LDB_LISTING="+listing,
		"LDB_LEVEL0=0",
		"LDB_CALLS="+s.calls,
	)

	return s
}

func (s *ldbStub) run(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := exec.Command("sh", append([]string{"-c", rocksDBCompactScript, "rocksdb-compact", s.datadir}, args...)...)
	cmd.Env = s.env

	out, err := cmd.CombinedOutput()

	return string(out), err
}

// compacted returns the --column_family of every compact call, in order.
func (s *ldbStub) compacted(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(s.calls)
	require.NoError(t, err)

	var cfs []string

	for _, call := range bytes.Split(bytes.TrimSuffix(data, []byte("\x00\n")), []byte("\x00\n")) {
		for _, arg := range strings.Split(string(call), "\x00") {
			if cf, ok := strings.CutPrefix(arg, "--column_family="); ok {
				cfs = append(cfs, cf)
			}
		}
	}

	return cfs
}

// TestRocksDBCompactScript_ParsesColumnFamilies feeds the script ldb's real
// output shape: a header line, then `{a, b, c}` with names printed raw. Besu
// names its families by single bytes, so one of them is a newline and another
// a tab.
func TestRocksDBCompactScript_ParsesColumnFamilies(t *testing.T) {
	besu := []string{"default", "\x01", "\x09", "\x0a", "\x12"}
	s := newLdbStub(t, "Column families in /data/database: \n{"+strings.Join(besu, ", ")+"}\n", "database")

	out, err := s.run(t)
	require.NoError(t, err, out)

	assert.Equal(t, besu, s.compacted(t))
	assert.Contains(t, out, "column_family=0x0a")
	assert.Contains(t, out, "column_family=default L6=2/10MB sst_bytes=4096\n", "levels before")
	assert.Regexp(t, `compacted \S+ column_family=default L6=2/10MB sst_bytes=4096 duration=\d+s`, out, "levels and duration after")
}

func TestRocksDBCompactScript_EveryDatabase(t *testing.T) {
	s := newLdbStub(t, "Column families in x: \n{default, Blocks, with space}\n",
		"nethermind_db/mainnet/state/0", "nethermind_db/mainnet/receipts")

	out, err := s.run(t, "--max_open_files=-1")
	require.NoError(t, err, out)

	assert.Equal(t, []string{
		"default", "Blocks", "with space", // receipts
		"default", "Blocks", "with space", // state/0
	}, s.compacted(t))

	data, err := os.ReadFile(s.calls)
	require.NoError(t, err)
	assert.Contains(t, string(data), "--max_open_files=-1\x00compact\x00", "extra args reach ldb")
	assert.Contains(t, string(data), "--try_load_options\x00")
}

func TestRocksDBCompactScript_Failures(t *testing.T) {
	t.Run("no database", func(t *testing.T) {
		s := newLdbStub(t, "")

		out, err := s.run(t)
		require.Error(t, err)
		assert.Contains(t, out, "no RocksDB database")
	})

	t.Run("unreadable database", func(t *testing.T) {
		// ldb prints the error to stderr and exits 0.
		s := newLdbStub(t, "", "db")

		out, err := s.run(t)
		require.Error(t, err)
		assert.Contains(t, out, "cannot list column families")
	})

	t.Run("level 0 not empty after compaction", func(t *testing.T) {
		s := newLdbStub(t, "Column families in db: \n{default}\n", "db")
		s.env = append(s.env, "LDB_LEVEL0=2")

		out, err := s.run(t)
		require.Error(t, err)
		assert.Contains(t, out, "level 0 not empty after compaction: L0=2/0MB L6=2/10MB sst_bytes=4096")
	})

	t.Run("a merge operator ldb does not have", func(t *testing.T) {
		s := newLdbStub(t, "Column families in db: \n{default}\n", "db")
		opts := filepath.Join(s.datadir, "db", "OPTIONS-000012")
		require.NoError(t, os.WriteFile(opts, []byte("  merge_operator=nullptr\n  merge_operator=LogIndexMergeOperator\n"), 0o644))

		out, err := s.run(t)
		require.Error(t, err)
		assert.Contains(t, out, "merge_operator=LogIndexMergeOperator")
		assert.NoFileExists(t, s.calls, "nothing is compacted")
	})
}

// ldb itself records its string-append operator in OPTIONS, so a database it
// has compacted before compacts again.
func TestRocksDBCompactScript_AcceptsLdbsOwnMergeOperator(t *testing.T) {
	s := newLdbStub(t, "Column families in db: \n{default}\n", "db")
	opts := filepath.Join(s.datadir, "db", "OPTIONS-000012")
	require.NoError(t, os.WriteFile(opts, []byte("  merge_operator={id=StringAppendOperator;delimiter=\\:;}\n"), 0o644))

	out, err := s.run(t)
	require.NoError(t, err, out)
	assert.Equal(t, []string{"default"}, s.compacted(t))
}
