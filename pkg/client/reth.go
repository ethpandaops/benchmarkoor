package client

type rethSpec struct{}

// NewRethSpec creates a new Reth client specification.
func NewRethSpec() Spec {
	return &rethSpec{}
}

// Ensure interface compliance.
var _ Spec = (*rethSpec)(nil)

func (s *rethSpec) Type() ClientType {
	return ClientReth
}

func (s *rethSpec) DefaultImage() string {
	return "ghcr.io/paradigmxyz/reth:latest"
}

func (s *rethSpec) DefaultCommand() []string {
	return []string{
		"node",
		// Data directory - should always point to /data
		"--datadir=/var/lib/reth",
		// Peering / Syncing
		//"--disable-discovery",
		//"--netrestrict=127.0.0.1/32",
		//"--max-peers=0",
		//"--trusted-only",
		// "Public" JSON RPC API
		"--http",
		"--http.addr=0.0.0.0",
		"--http.api=admin,debug,eth,net,trace,txpool,web3,rpc,reth,ots,flashbots,miner,mev",
		"--http.port=8545",
		// "Engine" JSON RPC API
		"--authrpc.jwtsecret=/tmp/jwtsecret",
		"--authrpc.addr=0.0.0.0",
		"--authrpc.port=8551",
		"--engine.disable-precompile-cache",
		// Trust the genesis stored in the datadir instead of recomputing it from
		// the chainspec alloc. Required to boot a state-actor snapshot: state-actor
		// direct-writes synthetic state (and the genesis) into reth's DB and emits a
		// chainspec whose alloc can't reproduce it, so reth would otherwise abort with
		// "genesis hash in the storage does not match the specified chainspec".
		// (Besu's equivalent is --genesis-state-hash-cache-enabled.)
		"--debug.skip-genesis-validation",
		// Others
		"--full",
	}
}

func (s *rethSpec) GenesisFlag() string {
	return "--chain="
}

func (s *rethSpec) RequiresInit() bool {
	return false
}

func (s *rethSpec) InitCommand() []string {
	return nil
}

func (s *rethSpec) DataDir() string {
	return "/var/lib/reth"
}

func (s *rethSpec) GenesisPath() string {
	return "/tmp/genesis.json"
}

func (s *rethSpec) JWTPath() string {
	return "/tmp/jwtsecret"
}

func (s *rethSpec) RPCPort() int {
	return 8545
}

func (s *rethSpec) EnginePort() int {
	return 8551
}

func (s *rethSpec) MetricsPort() int {
	return 8008
}

func (s *rethSpec) DefaultEnvironment() map[string]string {
	return nil
}

func (s *rethSpec) RPCRollbackSpec() *RPCRollbackSpec {
	return nil
}

func (s *rethSpec) DefaultConfigFiles() map[string]string {
	return nil
}

// SnapshotPrepareArgs makes reth persist every block during snapshot prep so the pre-run head reaches disk (else the snapshot lags a block and tests hang SYNCING).
func (s *rethSpec) SnapshotPrepareArgs() []string {
	return []string{
		"--engine.persistence-threshold=0",
		"--engine.memory-block-buffer-target=0",
	}
}

// DBMaintenanceCommands compacts Reth's MDBX database with `reth db copy
// --compact` (mdbx_copy -c, bundled since Reth 2.7.0): the database is
// rewritten without its free pages into <datadir>/db.compact and swapped in.
// The static files are append-only segments and are left alone.
//
// MDBX never shrinks: the pages a replay frees stay in mdbx.dat, and in every
// archive of it. The copy needs that much free space under the datadir, so the
// script fails before copying when the filesystem has less than the file's size.
//
// `reth db stats` is the inspection: tables, entries, sizes and the freelist.
// --datadir belongs to `db`, not to its subcommands (clap rejects it there).
func (s *rethSpec) DBMaintenanceCommands(dataDir string) *DBMaintenanceCommands {
	return &DBMaintenanceCommands{
		Compact:       []string{"sh", "-c", rethCompactScript, "reth-compact", dataDir},
		OwnEntrypoint: true,
		Inspect:       []string{"db", "--datadir=" + dataDir, "stats"},
	}
}

// rethCompactScript is the copy-and-swap around `reth db copy --compact`, in
// the reth image's /bin/sh. $1 is the datadir. mdbx_env_copy creates the
// destination directory itself and fails with EEXIST on an existing one;
// database.version lives next to mdbx.dat and is carried over.
const rethCompactScript = `set -eu
d=$1
dat=$d/db/mdbx.dat
[ -f "$dat" ] || { echo "no $dat: not a reth datadir" >&2; exit 1; }
size=$(stat -c %s "$dat")
free=$(df -B1 --output=avail "$d" | tail -n 1)
[ "$free" -gt "$size" ] || { echo "need $size bytes free under $d for the compacted copy, have $free" >&2; exit 1; }
rm -rf "$d/db.compact"
reth db --datadir="$d" copy --compact "$d/db.compact"
[ -f "$d/db.compact/mdbx.dat" ] || { echo "reth db copy wrote no $d/db.compact/mdbx.dat" >&2; exit 1; }
[ ! -f "$d/db/database.version" ] || cp "$d/db/database.version" "$d/db.compact/"
mv "$d/db" "$d/db.old" && mv "$d/db.compact" "$d/db" && rm -rf "$d/db.old"
echo "reth-compact: mdbx.dat $size -> $(stat -c %s "$d/db/mdbx.dat") bytes"
`
