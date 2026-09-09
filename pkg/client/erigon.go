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
		"--sync.parallel-state-flushing=false", // Disable parallel state flushing
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

func (s *erigonSpec) DefaultConfigFiles() map[string]string {
	return nil
}

// SnapshotPrepareArgs returns nil; Erigon needs no snapshot-only args.
func (s *erigonSpec) SnapshotPrepareArgs() []string {
	return nil
}

// DBMaintenanceCommands returns Erigon's offline database commands.
//
// `erigon db compact` rewrites every mdbx database of the datadir without its
// free pages. On a state-actor snapshot it takes chaindata from 2.0GB to 32MB.
//
// `erigon seg du` is the inspection: it reports the datadir disk usage by
// category.
//
// `erigon seg retire` is offered as an OPT-IN preparation step, named
// "seg-retire". It freezes the block and history ranges out of the mdbx
// databases into segment files under <datadir>/snapshots and prunes what it
// froze, which is what leaves free pages for the compaction to reclaim. On a
// real synced datadir that is the pairing erigon documents, and it is where the
// compaction earns most of its space.
//
// It is opt-in because it needs a datadir whose history spans whole steps. On a
// short synthetic chain it finds nothing to freeze but prunes anyway, and erigon
// then refuses to reopen its own datadir. Verified in CI on a state-actor
// snapshot advanced to block 39:
//
//	retiring blocks from=0 to=39
//	Build state history snapshots      <- nothing to build, a step is 390625
//	Prune state history                <- advances the DB prune marker anyway
//
// and the next boot fails with
//
//	[snapshots] gap between snapshot files and DB for domain receipt:
//	files end at txNum 0 but the DB was pruned up to 390625
//
// So do not enable it for a state-actor or otherwise synthetic snapshot. The
// compaction alone is worth running there.
//
// Every command runs against a STOPPED client: `db compact` takes the datadir
// lock and opens each database exclusively, so a running node makes it fail.
// They take --datadir rather than inheriting the one in DefaultCommand, since a
// datadir config may mount the data somewhere other than /data.
//
// `erigon db compact` landed in Erigon 3.7.0-dev (erigon PR #23677, September
// 2026). An older binary fails the step with "command db not found".
func (s *erigonSpec) DBMaintenanceCommands(dataDir string) *DBMaintenanceCommands {
	return &DBMaintenanceCommands{
		Prepare: []DBMaintenanceStep{
			{
				Name: "seg-retire",
				Args: []string{"seg", "retire", "--datadir=" + dataDir},
				Why: "freezes block and history ranges into segment files so the" +
					" compaction can reclaim the pages they used; needs a datadir" +
					" whose history spans whole steps (390625 blocks), and RUINS a" +
					" synthetic snapshot that has less",
			},
		},
		Compact: []string{"db", "compact", "--datadir=" + dataDir},
		Inspect: []string{"seg", "du", "--datadir=" + dataDir, "--verbose"},
	}
}
