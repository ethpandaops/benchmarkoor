package client

import "fmt"

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
		// Config file with HTTP timeout overrides.
		"--config=/tmp/config.toml",
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

func (s *gethSpec) DefaultConfigFiles() map[string]string {
	return map[string]string{
		"/tmp/config.toml": `[Node.HTTPTimeouts]
ReadTimeout = 300000000000 # 300s
ReadHeaderTimeout = 300000000000 # 300s
WriteTimeout = 300000000000 # 300s
IdleTimeout = 120000000000 # 120s
`,
	}
}

// SnapshotPrepareArgs returns nil; Geth needs no snapshot-only args.
func (s *gethSpec) SnapshotPrepareArgs() []string {
	return nil
}

// DBMaintenanceCommands returns geth's offline database commands.
//
// Both run against a STOPPED client: `geth db compact` takes the database
// lock, so a running node makes it fail. The commands take --datadir rather
// than inheriting the one in DefaultCommand, since a datadir config may mount
// the data somewhere other than /data.
func (s *gethSpec) DBMaintenanceCommands(dataDir string) *DBMaintenanceCommands {
	return &DBMaintenanceCommands{
		Compact: []string{"db", "compact", "--datadir=" + dataDir},
		Inspect: []string{"db", "inspect", "--datadir=" + dataDir},
	}
}

// Ensure geth has a mempool profile.
var _ MempoolProfile = (*gethSpec)(nil)

// MempoolCommand returns the geth command for the mempool mode.
//
// --txpool.nolocals makes geth treat txs sent over RPC like txs from peers.
// Without it, geth exempts "local" txs from pricing and eviction rules, so the
// scenarios would test a path that network traffic never takes.
func (s *gethSpec) MempoolCommand(settings MempoolSettings) []string {
	l := settings.Limits

	return replaceFlags(s.DefaultCommand(), map[string]string{
		"--port=":                fmt.Sprintf("--port=%d", settings.P2PPort),
		"--maxpeers=":            "--maxpeers=50",
		"--http.api=":            "--http.api=admin,debug,web3,eth,net,txpool",
		"--txpool.accountslots=": fmt.Sprintf("--txpool.accountslots=%d", l.AccountSlots),
		"--txpool.globalslots=":  fmt.Sprintf("--txpool.globalslots=%d", l.GlobalSlots),
		"--txpool.accountqueue=": fmt.Sprintf("--txpool.accountqueue=%d", l.AccountQueue),
		"--txpool.globalqueue=":  fmt.Sprintf("--txpool.globalqueue=%d", l.GlobalQueue),
		"--txpool.pricebump=":    fmt.Sprintf("--txpool.pricebump=%d", l.PriceBump),
		"--blobpool.pricebump=":  fmt.Sprintf("--blobpool.pricebump=%d", l.BlobPriceBump),
		"--txpool.nolocals":      "--txpool.nolocals",
		"--miner.gaslimit=":      fmt.Sprintf("--miner.gaslimit=%d", settings.GasLimit),
		"--miner.gasprice=":      "--miner.gasprice=1",
		"--txpool.pricelimit=":   "--txpool.pricelimit=1",
		"--rpc.txfeecap=":        "--rpc.txfeecap=0",
	})
}
