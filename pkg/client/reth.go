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
		"--datadir=/var/lib/reth",
		"--full",
		"--http",
		"--http.addr=0.0.0.0",
		"--http.api=admin,debug,eth,net,trace,txpool,web3,rpc,reth,ots,flashbots,miner,mev",
		"--http.port=8545",
		"--ws",
		"--ws.addr=0.0.0.0",
		"--ws.port=8546",
		"--ws.api=admin,debug,eth,net,trace,txpool,web3,rpc,reth,ots,flashbots,miner,mev",
		"--authrpc.jwtsecret=/tmp/jwtsecret",
		"--authrpc.addr=0.0.0.0",
		"--authrpc.port=8551",
		"--engine.disable-precompile-cache",
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
