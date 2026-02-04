package client

type besuSpec struct{}

// NewBesuSpec creates a new Besu client specification.
func NewBesuSpec() Spec {
	return &besuSpec{}
}

// Ensure interface compliance.
var _ Spec = (*besuSpec)(nil)

func (s *besuSpec) Type() ClientType {
	return ClientBesu
}

func (s *besuSpec) DefaultImage() string {
	return "hyperledger/besu:latest"
}

func (s *besuSpec) DefaultCommand() []string {
	return []string{
		"--data-path=/data",
		//"--bonsai-historical-block-limit=10000",
		//"--bonsai-limit-trie-logs-enabled=false",
		"--data-storage-format=BONSAI",
		"--metrics-enabled=true",
		"--metrics-host=0.0.0.0",
		"--metrics-port=8008",
		"--engine-rpc-enabled=true",
		"--engine-jwt-secret=/tmp/jwtsecret",
		"--engine-rpc-port=8551",
		"--engine-host-allowlist=*",
		"--rpc-http-enabled=true",
		"--rpc-http-host=0.0.0.0",
		"--rpc-http-port=8545",
		"--rpc-http-api=ETH,NET,CLIQUE,DEBUG,MINER,NET,PERM,ADMIN,TXPOOL,WEB3",
		"--rpc-http-cors-origins=*",
		"--Xhttp-timeout-seconds=660",
		"--host-allowlist=*",
		"--p2p-enabled=false",
		"--sync-mode=FULL",
		"--max-peers=0",
		"--discovery-enabled=false",
	}
}

func (s *besuSpec) GenesisFlag() string {
	return "--genesis-file="
}

func (s *besuSpec) RequiresInit() bool {
	return false
}

func (s *besuSpec) InitCommand() []string {
	return nil
}

func (s *besuSpec) DataDir() string {
	return "/data"
}

func (s *besuSpec) GenesisPath() string {
	return "/tmp/genesis.json"
}

func (s *besuSpec) JWTPath() string {
	return "/tmp/jwtsecret"
}

func (s *besuSpec) RPCPort() int {
	return 8545
}

func (s *besuSpec) EnginePort() int {
	return 8551
}

func (s *besuSpec) MetricsPort() int {
	return 8008
}

func (s *besuSpec) DefaultEnvironment() map[string]string {
	return map[string]string{
		"BESU_USER_NAME": "root",
	}
}

func (s *besuSpec) RPCRollbackSpec() *RPCRollbackSpec {
	return &RPCRollbackSpec{
		Method:    RollbackMethodSetHeadHex,
		RPCMethod: "debug_setHead",
	}
}
