package client

type erigonSpec struct{}

// NewErigonSpec creates a new Erigon client specification.
func NewErigonSpec() Spec {
	return &erigonSpec{}
}

// Ensure interface compliance.
var _ Spec = (*erigonSpec)(nil)

func (s *erigonSpec) Type() ClientType {
	return ClientErigon
}

func (s *erigonSpec) DefaultImage() string {
	return "ethpandaops/erigon:performance"
}

func (s *erigonSpec) DefaultCommand() []string {
	return []string{
		"--datadir=/data",
		"--externalcl",
		"--private.api.addr=0.0.0.0:9090",
		"--nat=any",
		"--http",
		"--http.addr=0.0.0.0",
		"--http.port=8545",
		"--http.vhosts=*",
		"--http.corsdomain=*",
		"--http.api=web3,eth,net,engine",
		"--txpool.disable",
		"--authrpc.addr=0.0.0.0",
		"--authrpc.port=8551",
		"--authrpc.vhosts=*",
		"--authrpc.jwtsecret=/tmp/jwtsecret",
		"--healthcheck",
		"--metrics",
		"--metrics.addr=0.0.0.0",
		"--metrics.port=8008",
		"--db.size.limit=2GB",
		"--experimental.always-generate-changesets",
	}
}

func (s *erigonSpec) RequiresInit() bool {
	return true
}

func (s *erigonSpec) InitCommand() []string {
	return []string{
		"init",
		"--datadir=/data",
		"/tmp/genesis.json",
	}
}

func (s *erigonSpec) DataDir() string {
	return "/data"
}

func (s *erigonSpec) GenesisPath() string {
	return "/tmp/genesis.json"
}

func (s *erigonSpec) JWTPath() string {
	return "/tmp/jwtsecret"
}

func (s *erigonSpec) RPCPort() int {
	return 8545
}

func (s *erigonSpec) EnginePort() int {
	return 8551
}

func (s *erigonSpec) MetricsPort() int {
	return 8008
}
