package client

type gethSpec struct{}

// NewGethSpec creates a new Geth client specification.
func NewGethSpec() Spec {
	return &gethSpec{}
}

// Ensure interface compliance.
var _ Spec = (*gethSpec)(nil)

func (s *gethSpec) Type() ClientType {
	return ClientGeth
}

func (s *gethSpec) DefaultImage() string {
	return "ethereum/client-go:stable"
}

func (s *gethSpec) DefaultCommand() []string {
	return []string{
		// Data directory - should always point to /data
		"--datadir=/data",
		// Peering / Syncing
		"--port=0",
		"--syncmode=full",
		"--maxpeers=0",
		"--nodiscover",
		"--bootnodes=",
		//"--gcmode=archive",
		"--snapshot=false",
		"--nat=none",
		// "Public" JSON RPC API
		"--http",
		"--http.addr=0.0.0.0",
		"--http.vhosts=*",
		"--http.corsdomain=*",
		"--http.api=admin,debug,web3,eth,net",
		"--http.port=8545",
		// "Engine" JSON RPC API
		"--authrpc.jwtsecret=/tmp/jwtsecret",
		"--authrpc.addr=0.0.0.0",
		"--authrpc.port=8551",
		"--authrpc.vhosts=*",
		// Metrics
		"--metrics",
		"--metrics.port=8008",
	}
}

func (s *gethSpec) GenesisFlag() string {
	return "--override.genesis="
}

func (s *gethSpec) RequiresInit() bool {
	return false
}

func (s *gethSpec) InitCommand() []string {
	return nil
}

func (s *gethSpec) DataDir() string {
	return "/data"
}

func (s *gethSpec) GenesisPath() string {
	return "/tmp/genesis.json"
}

func (s *gethSpec) JWTPath() string {
	return "/tmp/jwtsecret"
}

func (s *gethSpec) RPCPort() int {
	return 8545
}

func (s *gethSpec) EnginePort() int {
	return 8551
}

func (s *gethSpec) MetricsPort() int {
	return 8008
}

func (s *gethSpec) DefaultEnvironment() map[string]string {
	return nil
}

func (s *gethSpec) RPCRollbackSpec() *RPCRollbackSpec {
	return &RPCRollbackSpec{
		Method:    RollbackMethodSetHeadHex,
		RPCMethod: "debug_setHead",
	}
}
