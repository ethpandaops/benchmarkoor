package snapshot

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTree creates files (and their parent directories) under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
}

func TestBuildManifestExcludes(t *testing.T) {
	cases := []struct {
		client string
		files  map[string]string
		want   []string
	}{
		{
			client: "geth",
			files: map[string]string{
				"geth/nodekey": "k", "geth/LOCK": "", "geth/nodes/x": "", "geth/_snapshot_metadata.json": "",
				"geth/chaindata/LOCK": "", "geth/chaindata/000001.sst": "s", "geth/triedb/merkle.journal": "j",
				"keystore/UTC--x": "outside geth/, never packed", "geth/logs/geth.log": "",
			},
			want: []string{".", "./chaindata", "./chaindata/000001.sst", "./chaindata/LOCK",
				"./triedb", "./triedb/merkle.journal"},
		},
		{
			client: "reth",
			files: map[string]string{
				"discovery-secret": "", "known-peers.json": "", "reth.toml": "", "db/mdbx.dat": "d",
				"_snapshot_eth_getBlockByNumber.json": "", "logs/reth.log": "", ".download-cache/part-0": "", "static_files/known-peers.json": "kept",
			},
			want: []string{".", "./db", "./db/mdbx.dat", "./reth.toml", "./static_files", "./static_files/known-peers.json"},
		},
		{
			client: "besu",
			files:  map[string]string{"key": "", "database/key": "kept", "DATABASE_METADATA.json": ""},
			want:   []string{".", "./DATABASE_METADATA.json", "./database", "./database/key"},
		},
		{
			client: "ethrex",
			files:  map[string]string{"node.key": "", "node_config.json": "", "store/data": ""},
			want:   []string{".", "./store", "./store/data"},
		},
		{
			client: "nethermind",
			files: map[string]string{
				"nethermind_db/mainnet/peers/SimpleFileDb.db": "", "nethermind_db/mainnet/discoveryNodes/SimpleFileDb.db": "",
				"nethermind_db/mainnet/state/0/CURRENT": "", "nethermind_db/_snapshot_web3_clientVersion.json": "",
				"keystore/key": "outside nethermind_db, never packed", "logs/nethermind.log": "",
			},
			want: []string{".", "./mainnet", "./mainnet/state", "./mainnet/state/0", "./mainnet/state/0/CURRENT"},
		},
		{
			client: "erigon",
			files:  map[string]string{"LOCK": "", "nodekey": "", "nodes/eth68/mdbx.dat": "", "chaindata/mdbx.dat": "c"},
			want:   []string{".", "./chaindata", "./chaindata/mdbx.dat"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.client, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, tc.files)

			m, err := BuildManifest(tc.client, dir)
			require.NoError(t, err)
			assert.Equal(t, tc.want, m.Members)
			want := filepath.Join(dir, map[string]string{"geth": "geth", "nethermind": "nethermind_db"}[tc.client])
			assert.Equal(t, want, m.Dir, "tar runs in the packed root")
		})
	}
}

func TestBuildManifestErrors(t *testing.T) {
	_, err := BuildManifest("lighthouse", t.TempDir())
	require.ErrorContains(t, err, "unknown client")

	_, err = BuildManifest("geth", t.TempDir())
	require.Error(t, err, "a geth datadir must contain geth/")
}

func TestBuildManifestBytes(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a": "12345", "b/c": "123", "nodekey": "excluded"})

	m, err := BuildManifest("erigon", dir)
	require.NoError(t, err)
	assert.Equal(t, int64(8), m.Bytes)
	assert.Equal(t, int64(8+6*tarEntryOverhead), m.MaxArchiveBytes())
}

func packToTar(t *testing.T, m *Manifest) []byte {
	t.Helper()

	stream, wait, err := Pack(context.Background(), m, 3)
	require.NoError(t, err)

	compressed, err := io.ReadAll(stream)
	require.NoError(t, err)
	require.NoError(t, wait())

	cmd := exec.Command("zstd", "-dc")
	cmd.Stdin = bytes.NewReader(compressed)
	out, err := cmd.Output()
	require.NoError(t, err)

	return out
}

func TestPackListsManifestInOrder(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"geth/b": "b", "geth/a/z": "z", "geth/a/y": "y", "geth/nodekey": "secret", "geth/LOCK": "",
	})
	require.NoError(t, os.Symlink("a/y", filepath.Join(dir, "geth", "link")))

	m, err := BuildManifest("geth", dir)
	require.NoError(t, err)

	tarball := packToTar(t, m)

	cmd := exec.Command("tar", "-tf", "-")
	cmd.Stdin = bytes.NewReader(tarball)
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "./\n./a/\n./a/y\n./a/z\n./b\n./link\n", string(out),
		"--sort=name order, geth/'s contents with no prefix, the symlink kept as a link, nodekey and LOCK left out")

	assert.Equal(t, tarball, packToTar(t, m), "packing is deterministic")
}

func TestPackFailsOnUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a": "a", "b": "b"})
	require.NoError(t, os.Chmod(filepath.Join(dir, "b"), 0))

	m, err := BuildManifest("reth", dir)
	require.NoError(t, err)

	stream, wait, err := Pack(context.Background(), m, 3)
	require.NoError(t, err)

	_, _ = io.Copy(io.Discard, stream)
	require.ErrorContains(t, wait(), "tar")
}

func TestPackRejectsLevel(t *testing.T) {
	_, _, err := Pack(context.Background(), &Manifest{}, 22)
	require.ErrorContains(t, err, "zstd level")
}
