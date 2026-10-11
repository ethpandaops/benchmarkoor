// Package snapshot publishes a stopped client datadir in the
// snapshots.ethpandaops.io layout:
//
//	<prefix><network>/<client>/<block>/snapshot.tar.zst
//	<prefix><network>/<client>/<block>/_snapshot_eth_getBlockByNumber.json
//	<prefix><network>/<client>/<block>/_snapshot_metadata.json
//	<prefix><network>/<client>/latest   ("<block>\n")
package snapshot

import (
	"fmt"
	"path"
	"strconv"
)

const (
	// DefaultArchiveName is the archive's object name for a client without
	// an entry in archiveNames.
	DefaultArchiveName = "snapshot.tar.zst"

	HeadBlockName = "_snapshot_eth_getBlockByNumber.json"
	MetadataName  = "_snapshot_metadata.json"
	LatestName    = "latest"
)

// archiveNames are the names the snapshot_fetcher inventories (jochemnet,
// msf-2) fetch: reth's v2 storage and a pruned erigon each have their own.
var archiveNames = map[string]string{
	"reth":   "snapshot-v2.tar.zst",
	"erigon": "snapshot-pruned.tar.zst",
}

// ArchiveName returns client's default archive object name.
func ArchiveName(client string) string {
	if name, ok := archiveNames[client]; ok {
		return name
	}

	return DefaultArchiveName
}

// Layout names the objects of one snapshot.
type Layout struct {
	Prefix  string
	Network string
	Client  string
	Block   uint64
	// Archive overrides ArchiveName(Client).
	Archive string
}

func (l Layout) dir() string {
	return l.Prefix + path.Join(l.Network, l.Client, strconv.FormatUint(l.Block, 10))
}

// ArchiveKey is the key of the datadir archive.
func (l Layout) ArchiveKey() string {
	name := l.Archive
	if name == "" {
		name = ArchiveName(l.Client)
	}

	return l.dir() + "/" + name
}

// HeadBlockKey is the key of the head block's eth_getBlockByNumber response.
func (l Layout) HeadBlockKey() string { return l.dir() + "/" + HeadBlockName }

// MetadataKey is the key of the snapshot's metadata.
func (l Layout) MetadataKey() string { return l.dir() + "/" + MetadataName }

// LatestKey is the key naming the client's newest block.
func (l Layout) LatestKey() string {
	return l.Prefix + path.Join(l.Network, l.Client, LatestName)
}

// Validate rejects a layout whose keys would not be the ones consumers fetch.
func (l Layout) Validate() error {
	for name, v := range map[string]string{"network": l.Network, "client": l.Client, "archive": l.Archive} {
		if v == "" && name != "archive" {
			return fmt.Errorf("%s is required", name)
		}

		if v != "" && (path.Base(v) != v || v == "." || v == "..") {
			return fmt.Errorf("%s %q must be a single path segment", name, v)
		}
	}

	return nil
}
