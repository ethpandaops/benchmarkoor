package datadir

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestZFSPreRunNames(t *testing.T) {
	snap, clone := ZFSPreRunNames("tank/snapshots/geth", "/data/advanced-geth")

	t.Run("deterministic per output_dir, so a re-run finds its previous clone", func(t *testing.T) {
		s2, c2 := ZFSPreRunNames("tank/snapshots/geth", "/data/advanced-geth/")
		assert.Equal(t, snap, s2)
		assert.Equal(t, clone, c2)
	})

	t.Run("distinct per output_dir, so two targets never share a clone", func(t *testing.T) {
		s2, c2 := ZFSPreRunNames("tank/snapshots/geth", "/data/advanced-geth-2")
		assert.NotEqual(t, snap, s2)
		assert.NotEqual(t, clone, c2)
	})

	t.Run("a child of the source dataset, so it shares its pool and quota", func(t *testing.T) {
		assert.True(t, strings.HasPrefix(snap, "tank/snapshots/geth@prerun-"))
		assert.True(t, strings.HasPrefix(clone, "tank/snapshots/geth/prerun-"))
	})

	t.Run("never matched by the orphan reaper: the output must outlive the process", func(t *testing.T) {
		assert.NotContains(t, snap, "@benchmarkoor-")
		assert.NotContains(t, clone, "/benchmarkoor-clone-")
		assert.NotContains(t, clone, "/benchmarkoor-cp-")
	})
}
