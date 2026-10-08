package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ldbTestSetup creates two databases with besu's single-byte family names
// (0x01, 0x09 and 0x0a among them) next to nethermind-style ones.
const ldbTestSetup = `set -e
nl='
'
for db in /data/a/rdb /data/b; do
	mkdir -p $db
	ldb --db=$db --create_if_missing put k0 v0 >/dev/null
	for cf in "$(printf '\001')" "$(printf '\011')" Blocks "with space" "$nl"; do
		ldb --db=$db create_column_family "$cf" >/dev/null
	done
	# Every ldb run flushes on close, so each put leaves its own level-0 file.
	for i in 1 2 3; do
		for cf in default "$(printf '\001')" Blocks "$nl"; do
			ldb --db=$db --try_load_options --column_family="$cf" put k$i "v$i$cf" >/dev/null
		done
	done
done
`

const ldbTestDump = `set -e
nl='
'
for db in /data/a/rdb /data/b; do
	for cf in default "$(printf '\001')" "$(printf '\011')" Blocks "with space" "$nl"; do
		echo "$db $(printf %s "$cf" | od -An -tx1 | tr -d ' \n')"
		ldb --db=$db --try_load_options --column_family="$cf" get_property rocksdb.num-files-at-level0
		ldb --db=$db --try_load_options --column_family="$cf" --value_hex scan
	done
done
`

var levelZero = regexp.MustCompile(`(rocksdb\.num-files-at-level0: )\d+`)

// TestCompactDatadir_RealLdb runs the nethermind/besu compaction against real
// RocksDB databases with the ldb image named by BENCHMARKOOR_TEST_LDB_IMAGE
// (built from Dockerfile.rocksdb-ldb), through the docker daemon.
func TestCompactDatadir_RealLdb(t *testing.T) {
	image := os.Getenv("BENCHMARKOOR_TEST_LDB_IMAGE")
	if image == "" {
		t.Skip("BENCHMARKOOR_TEST_LDB_IMAGE is not set")
	}

	ctx := context.Background()
	log := logrus.New()

	mgr, err := docker.NewManager(log)
	require.NoError(t, err)
	require.NoError(t, mgr.Start(ctx))

	t.Cleanup(func() { _ = mgr.Stop() })

	dir := t.TempDir()
	mnt := docker.Mount{Type: "bind", Source: dir, Target: "/data"}

	sh := func(name, script string) string {
		var out bytes.Buffer

		require.NoError(t, mgr.RunInitContainer(ctx, &docker.ContainerSpec{
			Name:       fmt.Sprintf("benchmarkoor-ldb-test-%s-%d", name, os.Getpid()),
			Image:      image,
			Entrypoint: []string{"sh"},
			Command:    []string{"-c", script},
			Mounts:     []docker.Mount{mnt},
		}, &out, &out), out.String())

		return out.String()
	}

	t.Cleanup(func() { sh("chown", fmt.Sprintf("chown -R %d:%d /data", os.Getuid(), os.Getgid())) })

	sh("setup", ldbTestSetup)
	before := sh("before", ldbTestDump)
	require.Contains(t, before, "/data/b 0a\nrocksdb.num-files-at-level0: 2\n", "the newline family has data in level 0")

	compact := func() {
		require.NoError(t, CompactDatadir(ctx, log, mgr, &DatadirCompaction{
			Client:  client.ClientNethermind,
			DataDir: dir,
			Cfg:     &config.DBCompactionConfig{Enabled: true, Image: image},
		}))
	}

	compact()

	after := sh("after", ldbTestDump)
	assert.Equal(t, levelZero.ReplaceAllString(before, "${1}0"), after,
		"every family keeps its data and has nothing left in level 0")

	// ldb records its own merge operator in OPTIONS; a second pass accepts it.
	compact()
	assert.Equal(t, after, sh("again", ldbTestDump))
}
