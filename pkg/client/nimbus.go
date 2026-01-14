package client

type nimbusSpec struct{}

// NewNimbusSpec creates a new Nimbus-EL client specification.
func NewNimbusSpec() Spec {
	return &nimbusSpec{}
}

// Ensure interface compliance.
var _ Spec = (*nimbusSpec)(nil)

func (s *nimbusSpec) Type() ClientType {
	return ClientNimbusEL
}

func (s *nimbusSpec) DefaultImage() string {
	return "statusim/nimbus-eth1:performance"
}

func (s *nimbusSpec) DefaultCommand() []string {
	return []string{
		"--custom-network=/tmp/genesis.json",
		"--data-dir=/data",
		"--metrics=true",
		"--metrics-address=0.0.0.0",
		"--metrics-port=8008",
		"--engine-api=true",
		"--max-peers=0",
		"--jwt-secret=/tmp/jwtsecret",
		"--engine-api-port=8551",
		"--engine-api-address=0.0.0.0",
		"--allowed-origins=*",
		"--rpc=true",
		"--http-address=0.0.0.0",
		"--http-port=8545",
	}
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
