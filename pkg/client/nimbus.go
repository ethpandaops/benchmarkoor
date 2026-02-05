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
	return "statusim/nimbus-eth1:performance"
}

func (s *nimbusSpec) DefaultCommand() []string {
	return []string{
		// Data directory - should always point to /data
		"--data-dir=/data",
		// Peering
		"--max-peers=0",
		// "Public" JSON RPC API
		"--rpc=true",
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
	return "--custom-network="
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
	return nil
}
