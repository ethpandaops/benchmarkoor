package client

// tempoSpec runs Tempo through its Reth-compatible authenticated RPC surface.
// Tempo intentionally disables the standard Engine API and exposes raw block
// execution as reth_newPayload/reth_forkchoiceUpdated instead.
type tempoSpec struct{}

// NewTempoSpec creates a Tempo client specification.
func NewTempoSpec() Spec {
	return &tempoSpec{}
}

// Ensure interface compliance.
var _ Spec = (*tempoSpec)(nil)

func (s *tempoSpec) Type() ClientType {
	return ClientTempo
}

func (s *tempoSpec) DefaultImage() string {
	return "docker.io/tempoxyz/tempo:latest"
}

func (s *tempoSpec) DefaultCommand() []string {
	return []string{
		"node",
		"--dev",
		"--dev.block-time=1h",
		"--datadir=/var/lib/reth",
		"--http",
		"--http.addr=0.0.0.0",
		"--http.api=all",
		"--http.port=8545",
		"--authrpc.jwtsecret=/tmp/jwtsecret",
		"--authrpc.addr=0.0.0.0",
		"--authrpc.port=8551",
		"--metrics=0.0.0.0:9001",
		"--disable-discovery",
		"--no-persist-peers",
		"--debug.startup-sync-state-idle",
		"--builder.max-tasks=1",
	}
}

func (s *tempoSpec) GenesisFlag() string {
	return "--chain="
}

func (s *tempoSpec) RequiresInit() bool {
	return false
}

func (s *tempoSpec) InitCommand() []string {
	return nil
}

func (s *tempoSpec) DataDir() string {
	return "/var/lib/reth"
}

func (s *tempoSpec) GenesisPath() string {
	return "/tmp/genesis.json"
}

func (s *tempoSpec) JWTPath() string {
	return "/tmp/jwtsecret"
}

func (s *tempoSpec) RPCPort() int {
	return 8545
}

func (s *tempoSpec) EnginePort() int {
	return 8551
}

func (s *tempoSpec) MetricsPort() int {
	return 9001
}

func (s *tempoSpec) DefaultEnvironment() map[string]string {
	return nil
}

func (s *tempoSpec) RPCRollbackSpec() *RPCRollbackSpec {
	return nil
}

func (s *tempoSpec) DefaultConfigFiles() map[string]string {
	return nil
}

func (s *tempoSpec) SnapshotPrepareArgs() []string {
	return []string{
		"--engine.persistence-threshold=0",
		"--engine.memory-block-buffer-target=0",
	}
}
