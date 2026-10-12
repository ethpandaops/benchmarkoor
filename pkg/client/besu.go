package client

type besuSpec struct{}

// NewBesuSpec creates a new Besu client specification.
func NewBesuSpec() Spec {
	return &besuSpec{}
}

// Ensure interface compliance.
var _ Spec = (*besuSpec)(nil)

func (s *besuSpec) Type() ClientType {
	return ClientBesu
}

func (s *besuSpec) DefaultImage() string {
	return "hyperledger/besu:latest"
}

func (s *besuSpec) DefaultCommand() []string {
	return []string{
		// Data directory - should always point to /data
		"--data-path=/data",
		"--data-storage-format=BONSAI",
		// Trust the genesis state hash stored in the datadir instead of
		// recomputing it from the chainspec alloc. Required to boot a
		// state-actor snapshot: state-actor writes synthetic state (and the
		// genesis state root) directly into RocksDB and emits an empty
		// chainspec alloc, so the default recompute path would otherwise fail
		// with "Supplied genesis block does not match chain data stored". Has
		// no effect on a normal genesis-initialised datadir.
		"--genesis-state-hash-cache-enabled=true",
		// Peering / Syncing / TXPool
		"--p2p-enabled=false",
		"--sync-mode=FULL",
		"--max-peers=0",
		"--discovery-enabled=false",
		// "Public" JSON RPC API
		"--rpc-http-enabled=true",
		"--rpc-http-host=0.0.0.0",
		"--rpc-http-port=8545",
		"--rpc-http-api=ETH,NET,DEBUG,MINER,NET,PERM,ADMIN,TXPOOL,WEB3",
		"--rpc-http-cors-origins=*",
		"--Xhttp-timeout-seconds=660",
		"--host-allowlist=*",
		// "Engine" JSON RPC API
		"--engine-rpc-enabled=true",
		"--engine-jwt-secret=/tmp/jwtsecret",
		"--engine-rpc-port=8551",
		"--engine-host-allowlist=*",
		// Metrics
		"--metrics-enabled=true",
		"--metrics-host=0.0.0.0",
		"--metrics-port=8008",
		// Others
		//"--bonsai-historical-block-limit=10000",
		//"--bonsai-limit-trie-logs-enabled=false",
	}
}

func (s *besuSpec) GenesisFlag() string {
	return "--genesis-file="
}

func (s *besuSpec) RequiresInit() bool {
	return false
}

func (s *besuSpec) InitCommand() []string {
	return nil
}

func (s *besuSpec) DataDir() string {
	return "/data"
}

func (s *besuSpec) GenesisPath() string {
	return "/tmp/genesis.json"
}

func (s *besuSpec) JWTPath() string {
	return "/tmp/jwtsecret"
}

func (s *besuSpec) RPCPort() int {
	return 8545
}

func (s *besuSpec) EnginePort() int {
	return 8551
}

func (s *besuSpec) MetricsPort() int {
	return 8008
}

func (s *besuSpec) DefaultEnvironment() map[string]string {
	return map[string]string{
		"BESU_USER_NAME": "root",
	}
}

func (s *besuSpec) RPCRollbackSpec() *RPCRollbackSpec {
	return &RPCRollbackSpec{
		Method:    RollbackMethodSetHeadHex,
		RPCMethod: "debug_setHead",
	}
}

func (s *besuSpec) DefaultConfigFiles() map[string]string {
	return nil
}

// SnapshotPrepareArgs returns nil; Besu needs no snapshot-only args.
func (s *besuSpec) SnapshotPrepareArgs() []string {
	return nil
}

// DBMaintenanceCommands returns nil: Besu ships no offline compaction command.
//
// Checked against Besu 26.6.1. `besu storage` offers revert-variables,
// rocksdb {usage, x-stats}, trie-log {count, prune, export, import},
// revert-metadata and prune-pre-merge-blocks, and no --Xplugin-rocksdb-*
// option compacts either. `trie-log prune` is the nearest thing operators
// reach for, but it DELETES trie logs below the retention limit rather than
// rewriting the SSTs — a different operation, and one that takes away the
// history the Bonsai rollback needs between tests.
//
// `besu --data-path=<dir> storage rocksdb usage` would serve as the Inspect
// side, but Compact is what makes a client supported, so this stays nil.
func (s *besuSpec) DBMaintenanceCommands(_ string) *DBMaintenanceCommands {
	return nil
}
