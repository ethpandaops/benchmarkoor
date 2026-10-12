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

// DBMaintenanceCommands returns nil; benchmarkoor has no offline
// compaction command for Reth yet.
func (s *rethSpec) DBMaintenanceCommands(_ string) *DBMaintenanceCommands {
	return nil
}
