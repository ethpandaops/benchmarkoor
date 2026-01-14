package client

type nethermindSpec struct{}

// NewNethermindSpec creates a new Nethermind client specification.
func NewNethermindSpec() Spec {
	return &nethermindSpec{}
}

// Ensure interface compliance.
var _ Spec = (*nethermindSpec)(nil)

func (s *nethermindSpec) Type() ClientType {
	return ClientNethermind
}

func (s *nethermindSpec) DefaultImage() string {
	return "ethpandaops/nethermind:performance"
}

func (s *nethermindSpec) DefaultCommand() []string {
	return []string{
		"--datadir=/data",
		"--Init.ChainSpecPath=/tmp/genesis.json",
		"--config=none",
		"--JsonRpc.Enabled=true",
		"--JsonRpc.Host=0.0.0.0",
		"--JsonRpc.Port=8545",
		"--JsonRpc.JwtSecretFile=/tmp/jwtsecret",
		"--JsonRpc.EngineHost=0.0.0.0",
		"--JsonRpc.EnginePort=8551",
		"--Network.DiscoveryPort=0",
		"--Network.MaxActivePeers=0",
		"--Init.DiscoveryEnabled=false",
		"--HealthChecks.Enabled=true",
		"--Metrics.Enabled=true",
		"--Metrics.ExposePort=8008",
		"--Sync.MaxAttemptsToUpdatePivot=0",
		"--Init.AutoDump=None",
		"--Merge.NewPayloadBlockProcessingTimeout=70000",
		"--Merge.TerminalTotalDifficulty=0",
		"--Blocks.CachePrecompilesOnBlockProcessing=false",
	}
}

func (s *nethermindSpec) RequiresInit() bool {
	return false
}

func (s *nethermindSpec) InitCommand() []string {
	return nil
}

func (s *nethermindSpec) DataDir() string {
	return "/data"
}

func (s *nethermindSpec) GenesisPath() string {
	return "/tmp/genesis.json"
}

func (s *nethermindSpec) JWTPath() string {
	return "/tmp/jwtsecret"
}

func (s *nethermindSpec) RPCPort() int {
	return 8545
}

func (s *nethermindSpec) EnginePort() int {
	return 8551
}

func (s *nethermindSpec) MetricsPort() int {
	return 8008
}
