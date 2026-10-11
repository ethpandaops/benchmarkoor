package client

import _ "embed"

// RocksDBLdbImage is the RocksDB `ldb` tool that compacts nethermind and besu,
// built from Dockerfile.rocksdb-ldb. 11.8.1 is the RocksDB nethermind binds;
// besu's rocksdbjni 10.6.2 databases open with it too.
const RocksDBLdbImage = "ghcr.io/ethpandaops/benchmarkoor-rocksdb-ldb:11.8.1"

//go:embed rocksdb_compact.sh
var rocksDBCompactScript string

// rocksDBCompactCommands compacts every column family of every RocksDB
// database under dataDir with ldb, using the options each was written with.
// db_compaction.extra_args reach every `ldb compact`.
func rocksDBCompactCommands(dataDir string) *DBMaintenanceCommands {
	return &DBMaintenanceCommands{
		Compact:      []string{"sh", "-c", rocksDBCompactScript, "rocksdb-compact", dataDir},
		CompactImage: RocksDBLdbImage,
	}
}
