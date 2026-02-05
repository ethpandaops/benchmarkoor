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
	return "erigontech/erigon:latest"
}

func (s *erigonSpec) DefaultCommand() []string {
	return []string{
		// Data directory - should always point to /data
		"--datadir=/data",
		// Peering / Syncing / TXPool
		"--nat=none",
		"--maxpeers=0",
		"--txpool.disable",
		"--nodiscover",
		"--no-downloader",
		"--torrent.download.rate=0",
		"--torrent.upload.rate=0",
		// "Public" JSON RPC API
		"--http",
		"--http.addr=0.0.0.0",
		"--http.port=8545",
		"--http.vhosts=*",
		"--http.corsdomain=*",
		"--http.api=web3,eth,net,engine,debug",
		// "Engine" JSON RPC API
		"--authrpc.addr=0.0.0.0",
		"--authrpc.port=8551",
		"--authrpc.vhosts=*",
		"--authrpc.jwtsecret=/tmp/jwtsecret",
		// Metrics
		"--metrics",
		"--metrics.addr=0.0.0.0",
		"--metrics.port=8008",
		"--prune.mode=full",
		// Others
		"--log.dir.disable",               // We just need logs on the console
		"--private.api.addr=0.0.0.0:9090", // Erigon specific API
		"--externalcl",                    // Disables built in Caplin CL client.
		"--fcu.timeout=0",                 // Setting to 0 disables async FCU treatment (Default is 1s and then goes async)
		"--fcu.background.prune=false",    // Disables background pruning post FCU
		//"--fcu.background.commit=false",   // Needs erigon > v3.3.7
	}
}

func (s *erigonSpec) GenesisFlag() string {
	return "" // Erigon uses init container for genesis, not a command flag.
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

func (s *erigonSpec) DefaultEnvironment() map[string]string {
	return nil
}

func (s *erigonSpec) RPCRollbackSpec() *RPCRollbackSpec {
	return nil
}
