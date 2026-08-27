package client

type ethrexSpec struct{}

// NewEthrexSpec creates a new ethrex client specification.
func NewEthrexSpec() Spec {
	return &ethrexSpec{}
}

// Ensure interface compliance.
var _ Spec = (*ethrexSpec)(nil)

func (s *ethrexSpec) Type() ClientType {
	return ClientEthrex
}

func (s *ethrexSpec) DefaultImage() string {
	return "ghcr.io/lambdaclass/ethrex:latest"
}

func (s *ethrexSpec) DefaultCommand() []string {
	return []string{
		// Data directory
		"--datadir=/data",
		// Peering / syncing — disabled for benchmarks
		"--p2p.disabled",
		"--syncmode=full",
		// "Public" JSON-RPC API
		"--http.addr=0.0.0.0",
		"--http.port=8545",
		// Match geth's exposed namespaces; ethrex defaults to eth,net,web3
		// only, so debug_/admin_ calls would otherwise be unavailable.
		"--http.api=eth,net,web3,debug,admin",
		// Engine API. authrpc.addr defaults to 127.0.0.1, must override
		// so the runner can reach it from outside the container.
		"--authrpc.addr=0.0.0.0",
		"--authrpc.port=8551",
		"--authrpc.jwtsecret=/tmp/jwtsecret",
		// Metrics
		"--metrics",
		"--metrics.port=8008",
	}
}

func (s *ethrexSpec) GenesisFlag() string {
	return "--network="
}

func (s *ethrexSpec) RequiresInit() bool {
	return false
}

func (s *ethrexSpec) InitCommand() []string {
	return nil
}

func (s *ethrexSpec) DataDir() string {
	return "/data"
}

func (s *ethrexSpec) GenesisPath() string {
	return "/network-config/genesis.json"
}

func (s *ethrexSpec) JWTPath() string {
	return "/tmp/jwtsecret"
}

func (s *ethrexSpec) RPCPort() int {
	return 8545
}

func (s *ethrexSpec) EnginePort() int {
	return 8551
}

func (s *ethrexSpec) MetricsPort() int {
	return 8008
}

func (s *ethrexSpec) DefaultEnvironment() map[string]string {
	return nil
}

// RPCRollbackSpec returns nil — ethrex does not support debug_setHead.
// Use a container-level rollback strategy (container-recreate or
// container-checkpoint-restore) when running tests.
func (s *ethrexSpec) RPCRollbackSpec() *RPCRollbackSpec {
	return nil
}

func (s *ethrexSpec) DefaultConfigFiles() map[string]string {
	return nil
}

// SnapshotPrepareArgs returns nil; Ethrex needs no snapshot-only args.
func (s *ethrexSpec) SnapshotPrepareArgs() []string {
	return nil
}

// DBMaintenanceCommands returns nil; benchmarkoor has no offline
// compaction command for Ethrex yet.
func (s *ethrexSpec) DBMaintenanceCommands(_ string) *DBMaintenanceCommands {
	return nil
}
