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
	return "ethpandaops/geth:performance"
}

func (s *gethSpec) DefaultCommand() []string {
	return []string{
		"--datadir=/data",
		"--override.genesis=/tmp/genesis.json",
		"--syncmode=full",
		"--gcmode=archive",
		"--snapshot=false",
		"--nat=none",
		"--http",
		"--http.addr=0.0.0.0",
		"--http.vhosts=*",
		"--http.corsdomain=*",
		"--http.api=web3,eth,net",
		"--port=0",
		"--http.port=8545",
		"--maxpeers=0",
		"--nodiscover",
		"--bootnodes=",
		"--ws",
		"--ws.addr=0.0.0.0",
		"--ws.port=8546",
		"--ws.api=engine,eth,web3,net,debug",
		"--authrpc.jwtsecret=/tmp/jwtsecret",
		"--authrpc.addr=0.0.0.0",
		"--authrpc.port=8551",
		"--authrpc.vhosts=*",
		"--metrics",
		"--metrics.port=8008",
		"--verbosity=3",
	}
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
