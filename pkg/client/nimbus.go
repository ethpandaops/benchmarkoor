package client

type nimbusSpec struct{}

// NewNimbusSpec creates a new Nimbus client specification.
func NewNimbusSpec() Spec {
	return &nimbusSpec{}
}

// Ensure interface compliance.
var _ Spec = (*nimbusSpec)(nil)

func (s *nimbusSpec) Type() ClientType {
	return ClientNimbus
}

func (s *nimbusSpec) DefaultImage() string {
	return "statusim/nimbus-eth1:master"
}

func (s *nimbusSpec) DefaultCommand() []string {
	return []string{
		"executionClient",
		// Data directory - should always point to /data
		"--data-dir=/data",
		// Peering
		"--max-peers=0",
		// "Public" JSON RPC API
		"--rpc=true",
		"--rpc-api=eth,debug",
		"--http-address=0.0.0.0",
		"--http-port=8545",
		// "Engine" JSON RPC API
		"--jwt-secret=/tmp/jwtsecret",
		"--engine-api=true",
		"--engine-api-port=8551",
		"--engine-api-address=0.0.0.0",
		"--allowed-origins=*",
		// Metrics
		"--metrics=true",
		"--metrics-address=0.0.0.0",
		"--metrics-port=8008",
	}
}

func (s *nimbusSpec) GenesisFlag() string {
	return "--network="
}

func (s *nimbusSpec) RequiresInit() bool {
	return false
}

func (s *nimbusSpec) InitCommand() []string {
	return nil
}

func (s *nimbusSpec) DataDir() string {
	return "/data"
}

func (s *nimbusSpec) GenesisPath() string {
	return "/tmp/genesis.json"
}

func (s *nimbusSpec) JWTPath() string {
	return "/tmp/jwtsecret"
}

func (s *nimbusSpec) RPCPort() int {
	return 8545
}

func (s *nimbusSpec) EnginePort() int {
	return 8551
}

func (s *nimbusSpec) MetricsPort() int {
	return 8008
}

func (s *nimbusSpec) DefaultEnvironment() map[string]string {
	return nil
}

func (s *nimbusSpec) RPCRollbackSpec() *RPCRollbackSpec {
	return &RPCRollbackSpec{
		Method:    RollbackMethodSetHeadHex,
		RPCMethod: "debug_setHead",
	}
}

func (s *nimbusSpec) DefaultConfigFiles() map[string]string {
	return nil
}

// SnapshotPrepareArgs returns nil; Nimbus needs no snapshot-only args.
func (s *nimbusSpec) SnapshotPrepareArgs() []string {
	return nil
}
