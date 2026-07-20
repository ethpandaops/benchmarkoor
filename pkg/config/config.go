package config

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/ethpandaops/benchmarkoor/pkg/cpufreq"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/mitchellh/mapstructure"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultJWT is the default JWT secret used for Engine API authentication.
	DefaultJWT = "5a64f13bfb41a147711492237995b437433bcbec80a7eb2daae11132098d7bae"

	// DefaultContainerNetwork is the default container network name.
	DefaultContainerNetwork = "benchmarkoor"

	// DefaultLogLevel is the default logging level.
	DefaultLogLevel = "info"

	// DefaultResultsDir is the default directory for benchmark results.
	DefaultResultsDir = "./results"

	// DefaultPullPolicy is the default image pull policy.
	DefaultPullPolicy = "always"

	// DefaultDropCachesPath is the default path to the Linux drop_caches file.
	DefaultDropCachesPath = "/proc/sys/vm/drop_caches"

	// DefaultCPUSysfsPath is the default sysfs path for CPU frequency control.
	DefaultCPUSysfsPath = "/sys/devices/system/cpu"

	// LogTimestampFormat is the UTC timestamp format for log lines.
	LogTimestampFormat = "2006-01-02T15:04:05.000Z"

	// RollbackStrategyNone disables rollback after tests.
	RollbackStrategyNone = "none"

	// RollbackStrategyRPCDebugSetHead rolls back via eth_blockNumber + debug_setHead.
	RollbackStrategyRPCDebugSetHead = "rpc-debug-setHead"

	// RollbackStrategyContainerRecreate recreates the container between tests.
	// The data volume persists, so the client restarts from the same datadir.
	RollbackStrategyContainerRecreate = "container-recreate"

	// RollbackStrategyCheckpointRestore uses Podman's CRIU-based checkpoint/restore
	// to snapshot the container's memory state + ZFS snapshot the datadir after RPC
	// is ready, then instantly restore both per-test.
	// Requires container_runtime: "podman" and datadir.method: "zfs".
	RollbackStrategyCheckpointRestore = "container-checkpoint-restore"
)

// Config is the root configuration for benchmarkoor.
type Config struct {
	Global  GlobalConfig   `yaml:"global" mapstructure:"global"`
	Runner  RunnerConfig   `yaml:"runner" mapstructure:"runner"`
	API     *APIConfig     `yaml:"api,omitempty" mapstructure:"api"`
	Builder *BuilderConfig `yaml:"builder,omitempty" mapstructure:"builder"`
}

// BuilderConfig is the top-level builder block. It houses state-actor
// (materialises pre-populated datadirs) and eest_payloads (generates
// stateful EEST benchmark fixtures against such a datadir). Future
// builders plug in alongside.
type BuilderConfig struct {
	// RunTimeout caps the entire build (all builders and targets) as a Go
	// duration string (e.g. "2h"). Empty means no timeout. Overridable via
	// BENCHMARKOOR_BUILDER_RUN_TIMEOUT.
	RunTimeout string `yaml:"run_timeout,omitempty" mapstructure:"run_timeout"`
	// CleanupOnStart removes any leftover benchmarkoor resources (containers,
	// volumes, the build network, ZFS clones, overlay mounts, CPU-freq state)
	// before the build starts. The analogue of runner.cleanup_on_start.
	CleanupOnStart bool              `yaml:"cleanup_on_start" mapstructure:"cleanup_on_start"`
	StateActor     *StateActorConfig `yaml:"state_actor,omitempty" mapstructure:"state_actor"`
	// PreRuns is an optional stage that runs after StateActor and before
	// EESTPayloads: it advances a snapshot datadir (gas-bump + funding block +
	// fill-stateful on setup tests) and persists the result for EESTPayloads to
	// build on. See PreRunsConfig.
	PreRuns      *PreRunsConfig      `yaml:"pre_runs,omitempty" mapstructure:"pre_runs"`
	EESTPayloads *EESTPayloadsConfig `yaml:"eest_payloads,omitempty" mapstructure:"eest_payloads"`
}

// StateActorConfig configures how the state-actor binary is invoked via
// docker/podman to materialise pre-populated client datadirs. See
// https://github.com/ethereum/state-actor.
//
// The spec source (one of Spec/SpecFile) is shared across every target.
// Spec carries inline YAML content; SpecFile is a host path. They are
// mutually exclusive at the top level. Spec and target_size are
// complementary — when both are set state-actor uses the spec and
// treats target_size as a headroom budget for any further auto-fill.
//
// Spec may be written either as a structured YAML mapping (so editors give it
// syntax highlighting) or as a "|" block scalar; both normalize to the YAML
// body state-actor consumes. The field is excluded from Viper decoding and
// populated by normalizeStateActorSpec from the raw YAML (Viper can't decode a
// mapping into a string, and re-parsing preserves numbers/casing/comments).
//
// Config holds the per-target build parameters that can be hoisted up
// to avoid repeating them. Any field set on a target overrides the
// corresponding field in Config.
type StateActorConfig struct {
	ContainerRuntime string                    `yaml:"container_runtime,omitempty" mapstructure:"container_runtime"`
	Images           map[string]string         `yaml:"images,omitempty" mapstructure:"images"`
	PullPolicy       string                    `yaml:"pull_policy,omitempty" mapstructure:"pull_policy"`
	Spec             string                    `yaml:"spec,omitempty" mapstructure:"-"`
	SpecFile         string                    `yaml:"spec_file,omitempty" mapstructure:"spec_file"`
	Config           *StateActorClientDefaults `yaml:"config,omitempty" mapstructure:"config"`
	Targets          []StateActorTarget        `yaml:"targets,omitempty" mapstructure:"targets"`
}

// StateActorClientDefaults are the per-target build parameters that may
// be hoisted to the top level under `builder.state_actor.config`. Every
// field is also present on StateActorTarget; a non-nil/non-empty value
// on the target wins over the corresponding default.
//
// Pointer-typed fields (and pointer-typed bools) are required so that
// "explicitly false / zero" is distinguishable from "unset" — without
// this, a target could not opt out of a global archive=true.
type StateActorClientDefaults struct {
	TargetSize string  `yaml:"target_size,omitempty" mapstructure:"target_size"`
	Seed       *int64  `yaml:"seed,omitempty" mapstructure:"seed"`
	Fork       string  `yaml:"fork,omitempty" mapstructure:"fork"`
	ChainID    *int64  `yaml:"chain_id,omitempty" mapstructure:"chain_id"`
	GasLimit   *uint64 `yaml:"gas_limit,omitempty" mapstructure:"gas_limit"`
	Timestamp  *uint64 `yaml:"timestamp,omitempty" mapstructure:"timestamp"`
	ExtraData  string  `yaml:"extra_data,omitempty" mapstructure:"extra_data"`
	Archive    *bool   `yaml:"archive,omitempty" mapstructure:"archive"`
	BinaryTrie *bool   `yaml:"binary_trie,omitempty" mapstructure:"binary_trie"`
	GroupDepth *int    `yaml:"group_depth,omitempty" mapstructure:"group_depth"`
}

// StateActorTarget is one materialised datadir. The shared per-target
// build parameters mirror StateActorClientDefaults — see ResolveTarget
// for the merge semantics. Name/Client/OutputDir/TargetSize are
// intentionally not hoistable: they identify the target.
type StateActorTarget struct {
	Name       string  `yaml:"name,omitempty" mapstructure:"name"`
	Client     string  `yaml:"client" mapstructure:"client"`
	OutputDir  string  `yaml:"output_dir" mapstructure:"output_dir"`
	TargetSize string  `yaml:"target_size,omitempty" mapstructure:"target_size"`
	Force      bool    `yaml:"force,omitempty" mapstructure:"force"`
	Seed       *int64  `yaml:"seed,omitempty" mapstructure:"seed"`
	Fork       string  `yaml:"fork,omitempty" mapstructure:"fork"`
	ChainID    *int64  `yaml:"chain_id,omitempty" mapstructure:"chain_id"`
	GasLimit   *uint64 `yaml:"gas_limit,omitempty" mapstructure:"gas_limit"`
	Timestamp  *uint64 `yaml:"timestamp,omitempty" mapstructure:"timestamp"`
	ExtraData  string  `yaml:"extra_data,omitempty" mapstructure:"extra_data"`
	Archive    *bool   `yaml:"archive,omitempty" mapstructure:"archive"`
	BinaryTrie *bool   `yaml:"binary_trie,omitempty" mapstructure:"binary_trie"`
	GroupDepth *int    `yaml:"group_depth,omitempty" mapstructure:"group_depth"`
}

// ResolveTarget returns a copy of the i-th target with any unset fields
// filled in from StateActorConfig.Config. Identifier fields (Name,
// Client, OutputDir, TargetSize) and spec sources are not touched —
// those live exclusively on the target or the parent StateActorConfig.
//
// Resolution rule per field: per-target value wins when set (non-nil
// for pointer types, non-empty for strings); otherwise the value from
// Config is used. When Config is nil, the target is returned unchanged.
func (s *StateActorConfig) ResolveTarget(i int) StateActorTarget {
	t := s.Targets[i]
	if s.Config == nil {
		return t
	}

	g := s.Config

	if t.TargetSize == "" {
		t.TargetSize = g.TargetSize
	}

	if t.Seed == nil {
		t.Seed = g.Seed
	}

	if t.Fork == "" {
		t.Fork = g.Fork
	}

	if t.ChainID == nil {
		t.ChainID = g.ChainID
	}

	if t.GasLimit == nil {
		t.GasLimit = g.GasLimit
	}

	if t.Timestamp == nil {
		t.Timestamp = g.Timestamp
	}

	if t.ExtraData == "" {
		t.ExtraData = g.ExtraData
	}

	if t.Archive == nil {
		t.Archive = g.Archive
	}

	if t.BinaryTrie == nil {
		t.BinaryTrie = g.BinaryTrie
	}

	if t.GroupDepth == nil {
		t.GroupDepth = g.GroupDepth
	}

	return t
}

// StateActorSpecKind classifies how the top-level spec source is provided.
type StateActorSpecKind int

const (
	// StateActorSpecNone means no spec source is configured.
	StateActorSpecNone StateActorSpecKind = iota
	// StateActorSpecInline means the top-level Spec field carries the YAML body.
	StateActorSpecInline
	// StateActorSpecFile means the top-level SpecFile field holds a host path.
	StateActorSpecFile
)

// ResolveSpec returns the configured spec source. Spec (inline) wins over
// SpecFile when both are set, but validateBuilder rejects that case so
// in practice only one is non-empty at call time.
func (s *StateActorConfig) ResolveSpec() (StateActorSpecKind, string) {
	if s == nil {
		return StateActorSpecNone, ""
	}

	if s.Spec != "" {
		return StateActorSpecInline, s.Spec
	}

	if s.SpecFile != "" {
		return StateActorSpecFile, s.SpecFile
	}

	return StateActorSpecNone, ""
}

// EffectiveName returns the target's user-facing name. Defaults to
// Client when Name was not set, matching the `--target` filter behaviour
// in the build command.
func (t *StateActorTarget) EffectiveName() string {
	if t.Name != "" {
		return t.Name
	}

	return t.Client
}

// ImageFor returns the docker image configured for the given client.
// Empty string means no image is configured — validation enforces that
// every target's client has an entry in Images.
func (s *StateActorConfig) ImageFor(client string) string {
	if s == nil {
		return ""
	}

	return s.Images[client]
}

// stateActorSupportedClients lists the clients state-actor itself can
// materialise datadirs for. Nimbus is intentionally absent (state-actor
// does not implement a writer for it).
var stateActorSupportedClients = map[string]struct{}{
	"geth":       {},
	"reth":       {},
	"besu":       {},
	"nethermind": {},
	"ethrex":     {},
	"erigon":     {},
}

// stateActorValidPullPolicies mirrors the pull-policy vocabulary used by
// the runner side (pkg/docker, pkg/podman).
var stateActorValidPullPolicies = map[string]bool{
	"":               true,
	"always":         true,
	"if-not-present": true,
	"never":          true,
}

// EESTPayloadsConfig configures generation of stateful EEST benchmark
// fixtures via the `fill-stateful` command (execution-specs). See
// https://github.com/ethereum/execution-specs/pull/2637.
//
// Unlike state-actor, this builder is an orchestrator: per target it
// boots a filler EL client on a copy of a pre-populated snapshot datadir
// (e.g. produced by builder.state_actor), runs fill-stateful against the
// live client, and writes the resulting fixtures to the target's
// output_dir. The fixtures are later replayed by `benchmarkoor run`.
//
// FillImage is the container image carrying the `fill-stateful` command
// (uv + execution-specs). Config holds per-target defaults that can be
// hoisted to avoid repetition; any field set on a target wins.
type EESTPayloadsConfig struct {
	ContainerRuntime string `yaml:"container_runtime,omitempty" mapstructure:"container_runtime"`
	FillImage        string `yaml:"fill_image,omitempty" mapstructure:"fill_image"`
	// FillDockerfile, when set, makes benchmarkoor build the fill image from the
	// given Dockerfile at build time (using the container runtime), instead of
	// pulling a pre-built image. The built image is tagged FillImage when set,
	// otherwise DefaultFillImageTag. Both FillImage and FillDockerfile are
	// optional: when neither is set, benchmarkoor builds the fill image from a
	// default Dockerfile embedded in the binary.
	FillDockerfile string `yaml:"fill_dockerfile,omitempty" mapstructure:"fill_dockerfile"`
	PullPolicy     string `yaml:"pull_policy,omitempty" mapstructure:"pull_policy"`
	JWT            string `yaml:"jwt,omitempty" mapstructure:"jwt"`
	// FillCommand is the argv prefix invoked inside FillImage before the
	// fill-stateful flags. Defaults to ["uv", "run", "fill-stateful"].
	FillCommand []string `yaml:"fill_command,omitempty" mapstructure:"fill_command"`
	// EESTRepo / EESTRef select the execution-specs checkout used for filling.
	// benchmarkoor always clones the repo at this ref into an on-disk cache at
	// build time and mounts it into the fill container at /eest (the fill image
	// carries only the uv/python toolchain, not the repo). This lets the EEST
	// version live in config and change without rebuilding the image. Both
	// default when unset: EESTRepo to the execution-specs URL, EESTRef to
	// DefaultEESTRef.
	EESTRepo string               `yaml:"eest_repo,omitempty" mapstructure:"eest_repo"`
	EESTRef  string               `yaml:"eest_ref,omitempty" mapstructure:"eest_ref"`
	Config   *EESTPayloadDefaults `yaml:"config,omitempty" mapstructure:"config"`
	Targets  []EESTPayloadTarget  `yaml:"targets,omitempty" mapstructure:"targets"`
}

const (
	// DefaultEESTRepo is the execution-specs repository cloned for fill-stateful
	// when builder.eest_payloads.eest_repo is unset.
	DefaultEESTRepo = "https://github.com/ethereum/execution-specs.git"
	// DefaultEESTRef is the execution-specs ref cloned for fill-stateful when
	// builder.eest_payloads.eest_ref is unset (where fill-stateful currently lives).
	DefaultEESTRef = "forks/amsterdam"
	// DefaultFillImageTag is the tag applied to a fill image built from
	// fill_dockerfile when no explicit fill_image tag is given.
	DefaultFillImageTag = "benchmarkoor-eest-fill:local"
)

// BuildsFillImage reports whether benchmarkoor should build the fill image
// (rather than pulling a pre-built FillImage). It builds whenever FillDockerfile
// is set, or when no FillImage is configured to pull — in the latter case from
// the Dockerfile embedded in the binary.
func (e *EESTPayloadsConfig) BuildsFillImage() bool {
	return e.FillDockerfile != "" || e.FillImage == ""
}

// ResolveFillImageTag returns the image reference for the fill container: the
// configured FillImage, or DefaultFillImageTag when only a Dockerfile is set.
func (e *EESTPayloadsConfig) ResolveFillImageTag() string {
	if e.FillImage != "" {
		return e.FillImage
	}

	return DefaultFillImageTag
}

// ResolveEESTRepo returns the configured EEST repo URL, defaulting to
// DefaultEESTRepo.
func (e *EESTPayloadsConfig) ResolveEESTRepo() string {
	if e.EESTRepo != "" {
		return e.EESTRepo
	}

	return DefaultEESTRepo
}

// ResolveEESTRef returns the configured EEST ref, defaulting to DefaultEESTRef.
func (e *EESTPayloadsConfig) ResolveEESTRef() string {
	if e.EESTRef != "" {
		return e.EESTRef
	}

	return DefaultEESTRef
}

// DefaultFillCommand is the argv prefix used to invoke fill-stateful inside
// the fill image when EESTPayloadsConfig.FillCommand is unset.
var DefaultFillCommand = []string{"uv", "run", "fill-stateful"}

// ResolveFillCommand returns the configured fill-stateful argv prefix, or
// DefaultFillCommand when unset.
func (e *EESTPayloadsConfig) ResolveFillCommand() []string {
	if len(e.FillCommand) > 0 {
		return e.FillCommand
	}

	return DefaultFillCommand
}

// EESTPayloadDefaults are the per-target build parameters that may be
// hoisted to the top level under `builder.eest_payloads.config`. Every
// field is also present on EESTPayloadTarget; a non-nil/non-empty value
// on the target wins over the corresponding default. See ResolveTarget.
type EESTPayloadDefaults struct {
	FillerImage        string                       `yaml:"filler_image,omitempty" mapstructure:"filler_image"`
	Fork               string                       `yaml:"fork,omitempty" mapstructure:"fork"`
	Tests              []string                     `yaml:"tests,omitempty" mapstructure:"tests"`
	Filter             string                       `yaml:"filter,omitempty" mapstructure:"filter"`
	Marker             string                       `yaml:"marker,omitempty" mapstructure:"marker"`
	AddressStubsFile   string                       `yaml:"address_stubs_file,omitempty" mapstructure:"address_stubs_file"`
	AddressStubs       map[string]map[string]string `yaml:"address_stubs,omitempty" mapstructure:"address_stubs"`
	GasBenchmarkValues []int                        `yaml:"gas_benchmark_values,omitempty" mapstructure:"gas_benchmark_values"`
	FixedOpcodeCount   *[]float64                   `yaml:"fixed_opcode_count,omitempty" mapstructure:"fixed_opcode_count"`
	ExtractOpcodeCount *bool                        `yaml:"extract_opcode_count,omitempty" mapstructure:"extract_opcode_count"`
	DataDirMethod      string                       `yaml:"datadir_method,omitempty" mapstructure:"datadir_method"`
	MaxGasPerTest      *uint64                      `yaml:"max_gas_per_test,omitempty" mapstructure:"max_gas_per_test"`
	RPCSeedKey         string                       `yaml:"rpc_seed_key,omitempty" mapstructure:"rpc_seed_key"`
	FillerExtraArgs    []string                     `yaml:"filler_extra_args,omitempty" mapstructure:"filler_extra_args"`
}

// EESTPayloadTarget is one fixture-generation run. Identity/locator fields
// (Name, FillerClient, SourceDir, OutputDir, Genesis, GenesisForkOverride,
// GenesisEIPOverride) live exclusively on the target; the remaining fields
// mirror EESTPayloadDefaults and are resolved via ResolveTarget.
type EESTPayloadTarget struct {
	Name         string `yaml:"name,omitempty" mapstructure:"name"`
	FillerClient string `yaml:"filler_client" mapstructure:"filler_client"`
	SourceDir    string `yaml:"source_dir" mapstructure:"source_dir"`
	OutputDir    string `yaml:"output_dir" mapstructure:"output_dir"`
	// Genesis is the genesis/chainspec the filler boots from (besu/nethermind
	// read their fork schedule from it). geth/erigon boot from the snapshot
	// datadir instead and activate forks via --override.<fork> in
	// FillerExtraArgs, so they need no Genesis. Mirrors runner client `genesis`.
	Genesis string `yaml:"genesis,omitempty" mapstructure:"genesis"`
	// GenesisForkOverride / GenesisEIPOverride patch the Genesis at filler boot
	// to activate a fork the file doesn't schedule (e.g. amsterdam on an osaka
	// snapshot), identically to the runner. GenesisForkOverride sets
	// config.<fork>Time in a geth-format genesis (besu/reth/ethrex);
	// GenesisEIPOverride sets params.eip<N>TransitionTimestamp in a parity
	// chainspec (nethermind).
	GenesisForkOverride map[string]uint64   `yaml:"genesis_fork_override,omitempty" mapstructure:"genesis_fork_override"`
	GenesisEIPOverride  *GenesisEIPOverride `yaml:"genesis_eip_override,omitempty" mapstructure:"genesis_eip_override"`
	Force               bool                `yaml:"force,omitempty" mapstructure:"force"`

	// Hoistable fields (mirror EESTPayloadDefaults): a non-empty/non-nil value
	// here wins over the corresponding builder.eest_payloads.config default.
	FillerImage string `yaml:"filler_image,omitempty" mapstructure:"filler_image"`
	Fork        string `yaml:"fork,omitempty" mapstructure:"fork"`
	// Tests are pytest paths inside the fill image, e.g. tests/benchmark/compute.
	Tests []string `yaml:"tests,omitempty" mapstructure:"tests"`
	// Filter is a pytest -k expression (substring/node-id selection).
	Filter string `yaml:"filter,omitempty" mapstructure:"filter"`
	// Marker is a pytest -m marker expression, orthogonal to Filter's -k. e.g.
	// "repricing" to select the gas-repricing reference benchmarks, or
	// "not repricing" to exclude them.
	Marker string `yaml:"marker,omitempty" mapstructure:"marker"`
	// AddressStubsFile points at a JSON file of named address stubs; AddressStubs
	// defines the same mapping inline (the builder materializes it to a temp JSON
	// file). They are mutually exclusive. Each stub maps a symbolic name to an
	// arbitrary set of string fields (e.g. addr, pkey) that fill-stateful resolves
	// against the snapshot's pre-deployed state via --address-stubs. When a target
	// sets neither, both are hoisted as a unit from the config defaults.
	AddressStubsFile   string                       `yaml:"address_stubs_file,omitempty" mapstructure:"address_stubs_file"`
	AddressStubs       map[string]map[string]string `yaml:"address_stubs,omitempty" mapstructure:"address_stubs"`
	GasBenchmarkValues []int                        `yaml:"gas_benchmark_values,omitempty" mapstructure:"gas_benchmark_values"`
	FixedOpcodeCount   *[]float64                   `yaml:"fixed_opcode_count,omitempty" mapstructure:"fixed_opcode_count"`
	// ExtractOpcodeCount enables fill-stateful's --extract-opcode-count: after
	// building each execution-phase block it is traced via debug_traceBlockByHash
	// with a custom JS opcode-counting tracer, recording per-opcode execution
	// counts in the fixture's _info.metadata.opcode_counts (one entry per
	// engineNewPayloads block). The per-block re-trace
	// with the custom tracer is what makes it slow, so it's opt-in. Works with any
	// filler exposing debug_traceBlockByHash + JS tracer support (geth is the
	// validated one).
	ExtractOpcodeCount *bool    `yaml:"extract_opcode_count,omitempty" mapstructure:"extract_opcode_count"`
	DataDirMethod      string   `yaml:"datadir_method,omitempty" mapstructure:"datadir_method"`
	MaxGasPerTest      *uint64  `yaml:"max_gas_per_test,omitempty" mapstructure:"max_gas_per_test"`
	RPCSeedKey         string   `yaml:"rpc_seed_key,omitempty" mapstructure:"rpc_seed_key"`
	FillerExtraArgs    []string `yaml:"filler_extra_args,omitempty" mapstructure:"filler_extra_args"`
}

// ResolveTarget returns a copy of the i-th target with any unset hoistable
// fields filled in from EESTPayloadsConfig.Config. Identity/locator fields
// are never touched. Per-target value wins when set (non-nil for pointer
// types, non-empty for strings/slices); otherwise the value from Config is
// used. When Config is nil, the target is returned unchanged.
func (e *EESTPayloadsConfig) ResolveTarget(i int) EESTPayloadTarget {
	t := e.Targets[i]
	if e.Config == nil {
		return t
	}

	g := e.Config

	if t.FillerImage == "" {
		t.FillerImage = g.FillerImage
	}

	if t.Fork == "" {
		t.Fork = g.Fork
	}

	if len(t.Tests) == 0 {
		t.Tests = g.Tests
	}

	if t.Filter == "" {
		t.Filter = g.Filter
	}

	if t.Marker == "" {
		t.Marker = g.Marker
	}

	// Hoist the address-stubs pair as a unit: a target that sets either form
	// keeps its own and inherits neither, preserving their mutual exclusion.
	if len(t.AddressStubs) == 0 && t.AddressStubsFile == "" {
		t.AddressStubs = g.AddressStubs
		t.AddressStubsFile = g.AddressStubsFile
	}

	if len(t.GasBenchmarkValues) == 0 {
		t.GasBenchmarkValues = g.GasBenchmarkValues
	}

	if t.FixedOpcodeCount == nil {
		t.FixedOpcodeCount = g.FixedOpcodeCount
	}

	if t.ExtractOpcodeCount == nil {
		t.ExtractOpcodeCount = g.ExtractOpcodeCount
	}

	if t.DataDirMethod == "" {
		t.DataDirMethod = g.DataDirMethod
	}

	if t.MaxGasPerTest == nil {
		t.MaxGasPerTest = g.MaxGasPerTest
	}

	if t.RPCSeedKey == "" {
		t.RPCSeedKey = g.RPCSeedKey
	}

	if len(t.FillerExtraArgs) == 0 {
		t.FillerExtraArgs = g.FillerExtraArgs
	}

	return t
}

// EffectiveName returns the target's user-facing name, defaulting to the
// filler client when Name was not set — matching the `--target` filter
// behaviour in the build command.
func (t *EESTPayloadTarget) EffectiveName() string {
	if t.Name != "" {
		return t.Name
	}

	return t.FillerClient
}

// eestFillerSupportedClients lists the clients benchmarkoor knows how to boot as
// the fill-stateful filler (see fillerCommand in pkg/builder). fill-stateful
// forces use_testing_build_block=True, so a filler MUST implement the
// testing_buildBlockV1 RPC and debug_setHead (the per-test chain rewind).
//
// Status as of this writing:
//   - geth: fully works (ethpandaops/geth implements both).
//   - besu: plumbed but blocked upstream — testing_buildBlockV1 (TESTING
//     namespace, besu-eth/besu#9838) + debug_setHead both respond, but
//     besu's self-built block fails its own engine_newPayloadV4 with a
//     World-State-Root mismatch. Kept as scaffolding.
//   - nethermind: plumbed but blocked upstream — testing_buildBlockV1 works
//     (Testing module, special image) but debug_setHead is unimplemented and
//     its receipt trips EEST's strict model. Kept as scaffolding.
//
// besu/nethermind stay listed so configs can experiment with them once the
// upstream gaps close; their example targets are commented out.
var eestFillerSupportedClients = map[string]struct{}{
	"geth":       {},
	"besu":       {},
	"nethermind": {},
}

// RunnerConfig contains all run-specific configuration settings.
type RunnerConfig struct {
	ContainerRuntime   string               `yaml:"container_runtime,omitempty" mapstructure:"container_runtime"`
	ClientLogsToStdout bool                 `yaml:"client_logs_to_stdout" mapstructure:"client_logs_to_stdout"`
	ContainerNetwork   string               `yaml:"container_network" mapstructure:"container_network"`
	CleanupOnStart     bool                 `yaml:"cleanup_on_start" mapstructure:"cleanup_on_start"`
	RunTimeout         string               `yaml:"run_timeout,omitempty" mapstructure:"run_timeout"`
	Directories        DirectoriesConfig    `yaml:"directories,omitempty" mapstructure:"directories"`
	DropCachesPath     string               `yaml:"drop_caches_path,omitempty" mapstructure:"drop_caches_path"`
	CPUSysfsPath       string               `yaml:"cpu_sysfs_path,omitempty" mapstructure:"cpu_sysfs_path"`
	GitHubToken        string               `yaml:"github_token,omitempty" mapstructure:"github_token"`
	LiveReporting      *LiveReportingConfig `yaml:"live_reporting,omitempty" mapstructure:"live_reporting"`
	Benchmark          BenchmarkConfig      `yaml:"benchmark" mapstructure:"benchmark"`
	Client             ClientConfig         `yaml:"client" mapstructure:"client"`
	Instances          []ClientInstance     `yaml:"instances" mapstructure:"instances"`
}

// LiveReportingConfig enables periodic run-status reports to a benchmarkoor
// API instance. When enabled, each run posts a snapshot of its state
// (status, test counts, etc.) to the API at a jittered interval so the UI
// can display in-progress runs.
type LiveReportingConfig struct {
	Enabled        bool    `yaml:"enabled" mapstructure:"enabled"`
	Endpoint       string  `yaml:"endpoint" mapstructure:"endpoint"`
	Token          string  `yaml:"token" mapstructure:"token"`
	DiscoveryPath  string  `yaml:"discovery_path" mapstructure:"discovery_path"`
	Interval       string  `yaml:"interval,omitempty" mapstructure:"interval"`               // default 1m
	JitterFraction float64 `yaml:"jitter_fraction,omitempty" mapstructure:"jitter_fraction"` // default 0.2
	Timeout        string  `yaml:"timeout,omitempty" mapstructure:"timeout"`                 // default 10s
	// Logs* control the on-demand benchmarkoor.log streamer. When
	// LogsEnabled is true, the runner opens a WebSocket to the API for
	// the lifetime of the run; log bytes only flow while at least one
	// UI client is actively watching.
	LogsEnabled  *bool  `yaml:"logs_enabled,omitempty" mapstructure:"logs_enabled"`   // default true
	LogsInterval string `yaml:"logs_interval,omitempty" mapstructure:"logs_interval"` // default 1s (while streaming)
}

// Default values for LiveReportingConfig when fields are left empty.
const (
	DefaultLiveReportingInterval       = time.Minute
	DefaultLiveReportingJitterFraction = 0.2
	DefaultLiveReportingTimeout        = 10 * time.Second
	DefaultLiveReportingLogsInterval   = 200 * time.Millisecond
)

// GetInterval returns the reporting interval with the default applied.
func (l *LiveReportingConfig) GetInterval() time.Duration {
	if l == nil || l.Interval == "" {
		return DefaultLiveReportingInterval
	}

	d, err := time.ParseDuration(l.Interval)
	if err != nil {
		return DefaultLiveReportingInterval
	}

	return d
}

// GetJitterFraction returns the jitter fraction with the default applied
// when the field is unset (zero value). To disable jitter entirely, use a
// negative value in the config and we'll clamp it to zero.
func (l *LiveReportingConfig) GetJitterFraction() float64 {
	if l == nil || l.JitterFraction == 0 {
		return DefaultLiveReportingJitterFraction
	}

	if l.JitterFraction < 0 {
		return 0
	}

	return l.JitterFraction
}

// GetTimeout returns the per-request timeout with the default applied.
func (l *LiveReportingConfig) GetTimeout() time.Duration {
	if l == nil || l.Timeout == "" {
		return DefaultLiveReportingTimeout
	}

	d, err := time.ParseDuration(l.Timeout)
	if err != nil {
		return DefaultLiveReportingTimeout
	}

	return d
}

// GetLogsEnabled reports whether the on-demand log streamer should run.
// Defaults to true when live reporting is enabled; set LogsEnabled to
// false explicitly to disable.
func (l *LiveReportingConfig) GetLogsEnabled() bool {
	if l == nil {
		return false
	}

	if l.LogsEnabled == nil {
		return true
	}

	return *l.LogsEnabled
}

// GetLogsInterval returns the file-tail push cadence with the default
// applied. Only relevant while a UI client is watching; between ticks
// the streamer sleeps.
func (l *LiveReportingConfig) GetLogsInterval() time.Duration {
	if l == nil || l.LogsInterval == "" {
		return DefaultLiveReportingLogsInterval
	}

	d, err := time.ParseDuration(l.LogsInterval)
	if err != nil {
		return DefaultLiveReportingLogsInterval
	}

	return d
}

// MetadataConfig contains arbitrary metadata labels for a benchmark run.
type MetadataConfig struct {
	Labels map[string]string `yaml:"labels,omitempty" mapstructure:"labels" json:"labels,omitempty"`
}

// GlobalConfig contains global application settings.
type GlobalConfig struct {
	LogLevel string `yaml:"log_level" mapstructure:"log_level"`
	// Env declares config-local variables available to ${VAR} / ${VAR:-default}
	// substitution throughout the file, as a per-config default for an env var of
	// the same name. A real shell env var of that name still wins, so configs stay
	// overridable. Consumed at load time (see envExpander); the parsed map is not
	// otherwise used and — unlike the substitution source — is Viper-lowercased.
	Env         map[string]string       `yaml:"env,omitempty" mapstructure:"env"`
	Directories GlobalDirectoriesConfig `yaml:"directories,omitempty" mapstructure:"directories"`
}

// GlobalDirectoriesConfig contains directory paths shared across the build and
// run commands.
type GlobalDirectoriesConfig struct {
	// CacheDir is the on-disk cache shared by both commands: executor git/archive
	// clones (run) and the EEST repo clone (build). If empty, defaults to
	// ~/.cache/benchmarkoor.
	CacheDir string `yaml:"cachedir,omitempty" mapstructure:"cachedir"`
}

// DirectoriesConfig contains runner-specific directory path configurations.
type DirectoriesConfig struct {
	// TmpDataDir is the directory for temporary datadir copies.
	// If empty, uses the system default temp directory.
	TmpDataDir string `yaml:"tmp_datadir,omitempty" mapstructure:"tmp_datadir"`
}

// ResolveCacheDir returns the configured global cache directory, defaulting to
// ~/.cache/benchmarkoor when unset.
func (c *Config) ResolveCacheDir() (string, error) {
	if c.Global.Directories.CacheDir != "" {
		return c.Global.Directories.CacheDir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory for cache dir: %w", err)
	}

	return filepath.Join(home, ".cache", "benchmarkoor"), nil
}

// BenchmarkConfig contains benchmark-specific settings.
type BenchmarkConfig struct {
	ResultsDir                      string               `yaml:"results_dir" mapstructure:"results_dir"`
	ResultsOwner                    string               `yaml:"results_owner,omitempty" mapstructure:"results_owner"`
	SkipTestRun                     bool                 `yaml:"skip_test_run" mapstructure:"skip_test_run"`
	SystemResourceCollectionEnabled *bool                `yaml:"system_resource_collection_enabled,omitempty" mapstructure:"system_resource_collection_enabled"`
	GenerateResultsIndex            bool                 `yaml:"generate_results_index" mapstructure:"generate_results_index"`
	GenerateResultsIndexMethod      string               `yaml:"generate_results_index_method,omitempty" mapstructure:"generate_results_index_method"`
	GenerateSuiteStats              bool                 `yaml:"generate_suite_stats" mapstructure:"generate_suite_stats"`
	GenerateSuiteStatsMethod        string               `yaml:"generate_suite_stats_method,omitempty" mapstructure:"generate_suite_stats_method"`
	ResultsUpload                   *ResultsUploadConfig `yaml:"results_upload,omitempty" mapstructure:"results_upload"`
	Tests                           TestsConfig          `yaml:"tests,omitempty" mapstructure:"tests"`
}

// ResultsUploadConfig contains configuration for uploading results.
type ResultsUploadConfig struct {
	S3 *S3UploadConfig `yaml:"s3,omitempty" mapstructure:"s3"`
}

// S3UploadConfig contains S3-compatible storage upload settings.
type S3UploadConfig struct {
	Enabled         bool   `yaml:"enabled" mapstructure:"enabled"`
	EndpointURL     string `yaml:"endpoint_url,omitempty" mapstructure:"endpoint_url"`
	Region          string `yaml:"region,omitempty" mapstructure:"region"`
	Bucket          string `yaml:"bucket" mapstructure:"bucket"`
	AccessKeyID     string `yaml:"access_key_id,omitempty" mapstructure:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key,omitempty" mapstructure:"secret_access_key"`
	Prefix          string `yaml:"prefix,omitempty" mapstructure:"prefix"`
	StorageClass    string `yaml:"storage_class,omitempty" mapstructure:"storage_class"`
	ACL             string `yaml:"acl,omitempty" mapstructure:"acl"`
	ForcePathStyle  bool   `yaml:"force_path_style" mapstructure:"force_path_style"`
	ParallelUploads int    `yaml:"parallel_uploads,omitempty" mapstructure:"parallel_uploads"`
}

// TestsConfig contains test execution settings.
type TestsConfig struct {
	Filter       string              `yaml:"filter,omitempty" mapstructure:"filter"`
	Metadata     MetadataConfig      `yaml:"metadata,omitempty" mapstructure:"metadata"`
	Source       SourceConfig        `yaml:"source,omitempty" mapstructure:"source"`
	OpcodeSource *OpcodeSourceConfig `yaml:"opcode_source,omitempty" mapstructure:"opcode_source"`
}

// SourceConfig defines where to find test files.
type SourceConfig struct {
	// New unified source options.
	Git          *GitSourceV2         `yaml:"git,omitempty" mapstructure:"git"`
	Local        *LocalSourceV2       `yaml:"local,omitempty" mapstructure:"local"`
	Archive      *ArchiveSourceConfig `yaml:"archive,omitempty" mapstructure:"archive"`
	EESTFixtures *EESTFixturesSource  `yaml:"eest_fixtures,omitempty" mapstructure:"eest_fixtures"`
}

// EESTFixturesSource defines an EEST fixtures source from GitHub releases, artifacts,
// or local directories/tarballs.
type EESTFixturesSource struct {
	GitHubRepo     string `yaml:"github_repo,omitempty" mapstructure:"github_repo"`
	GitHubRelease  string `yaml:"github_release,omitempty" mapstructure:"github_release"`
	FixturesURL    string `yaml:"fixtures_url,omitempty" mapstructure:"fixtures_url"`
	GenesisURL     string `yaml:"genesis_url,omitempty" mapstructure:"genesis_url"`
	FixturesSubdir string `yaml:"fixtures_subdir,omitempty" mapstructure:"fixtures_subdir"`
	// GitHub Actions artifact support (alternative to releases).
	FixturesArtifactName  string `yaml:"fixtures_artifact_name,omitempty" mapstructure:"fixtures_artifact_name"`
	GenesisArtifactName   string `yaml:"genesis_artifact_name,omitempty" mapstructure:"genesis_artifact_name"`
	FixturesArtifactRunID string `yaml:"fixtures_artifact_run_id,omitempty" mapstructure:"fixtures_artifact_run_id"`
	GenesisArtifactRunID  string `yaml:"genesis_artifact_run_id,omitempty" mapstructure:"genesis_artifact_run_id"`
	// Local directory support (already-extracted fixtures).
	LocalFixturesDir string `yaml:"local_fixtures_dir,omitempty" mapstructure:"local_fixtures_dir"`
	LocalGenesisDir  string `yaml:"local_genesis_dir,omitempty" mapstructure:"local_genesis_dir"`
	// Local tarball support (.tar.gz files).
	LocalFixturesTarball string `yaml:"local_fixtures_tarball,omitempty" mapstructure:"local_fixtures_tarball"`
	LocalGenesisTarball  string `yaml:"local_genesis_tarball,omitempty" mapstructure:"local_genesis_tarball"`
	// PreRuns points at a builder.pre_runs bundle (engine_newPayload/
	// forkchoiceUpdated request lines advancing a raw snapshot to the setup head).
	// The runner replays it once per instance, before the benchmark fixtures, so
	// every client reaches the setup state on its own raw snapshot — no per-client
	// advanced datadirs. Already-applied blocks are skipped, so it is a no-op when
	// the datadir is already advanced.
	PreRuns *EESTPreRunsSource `yaml:"pre_runs,omitempty" mapstructure:"pre_runs"`
}

// PreRunBundleSubdir is the subdirectory (under a builder.pre_runs target's
// output_dir) where the replayable payload bundle is written, and the default
// EESTPreRunsSource.FixturesSubdir the runner reads it from.
const PreRunBundleSubdir = "pre_run_bundle"

// EESTPreRunsSource locates a builder.pre_runs payload bundle for the runner to
// replay before the benchmark fixtures. It mirrors the fixtures source's local
// layout: FixturesSubdir defaults to PreRunBundleSubdir.
type EESTPreRunsSource struct {
	LocalFixturesDir string `yaml:"local_fixtures_dir,omitempty" mapstructure:"local_fixtures_dir"`
	FixturesSubdir   string `yaml:"fixtures_subdir,omitempty" mapstructure:"fixtures_subdir"`
}

// UseArtifacts returns true if the source is configured to use GitHub Actions artifacts.
func (e *EESTFixturesSource) UseArtifacts() bool {
	return e.FixturesArtifactName != "" || e.GenesisArtifactName != ""
}

// HasGenesisArtifact reports whether a genesis artifact is explicitly configured.
// Genesis is optional in artifact mode — stateful-engine fixtures boot from a
// pre-populated snapshot datadir (runner.client.datadirs) and carry no genesis —
// so it is only resolved/downloaded when a name or run ID is set.
func (e *EESTFixturesSource) HasGenesisArtifact() bool {
	return e.GenesisArtifactName != "" || e.GenesisArtifactRunID != ""
}

// UseLocalDir returns true if the source is configured to use local directories.
func (e *EESTFixturesSource) UseLocalDir() bool {
	return e.LocalFixturesDir != "" || e.LocalGenesisDir != ""
}

// UseLocalTarball returns true if the source is configured to use local tarballs.
func (e *EESTFixturesSource) UseLocalTarball() bool {
	return e.LocalFixturesTarball != "" || e.LocalGenesisTarball != ""
}

// UseFixturesURL returns true when fixtures should be fetched from a standalone
// URL — a release/plain .tar.gz download or a GitHub Actions artifact URL. When
// github_release is also set, fixtures_url is instead a release-URL override, so
// this is false and release mode handles it.
func (e *EESTFixturesSource) UseFixturesURL() bool {
	return e.FixturesURL != "" && e.GitHubRelease == ""
}

// validate checks the EEST fixtures source configuration for errors.
// Exactly one mode must be specified: release, artifact, local_dir, or local_tarball.
func (e *EESTFixturesSource) validate() error {
	hasRelease := e.GitHubRelease != ""
	hasArtifacts := e.UseArtifacts()
	hasLocalDir := e.UseLocalDir()
	hasLocalTarball := e.UseLocalTarball()
	hasFixturesURL := e.UseFixturesURL()

	// Count active modes.
	modeCount := 0
	if hasRelease {
		modeCount++
	}

	if hasArtifacts {
		modeCount++
	}

	if hasLocalDir {
		modeCount++
	}

	if hasLocalTarball {
		modeCount++
	}

	if hasFixturesURL {
		modeCount++
	}

	if modeCount == 0 {
		return fmt.Errorf(
			"eest_fixtures: must specify one of: github_release, " +
				"fixtures_url, fixtures_artifact_name, " +
				"local_fixtures_dir/local_genesis_dir, " +
				"or local_fixtures_tarball/local_genesis_tarball",
		)
	}

	if modeCount > 1 {
		return fmt.Errorf(
			"eest_fixtures: cannot combine modes (release, fixtures_url, " +
				"artifact, local_dir, local_tarball are mutually exclusive)",
		)
	}

	// Validate remote modes require github_repo.
	if (hasRelease || hasArtifacts) && e.GitHubRepo == "" {
		return fmt.Errorf("eest_fixtures.github_repo is required for release/artifact modes")
	}

	// Validate local dir mode.
	if hasLocalDir {
		if e.LocalFixturesDir == "" {
			return fmt.Errorf("eest_fixtures: local_fixtures_dir is required for local directory mode")
		}

		if err := validateDirExists(e.LocalFixturesDir, "eest_fixtures.local_fixtures_dir"); err != nil {
			return err
		}

		// local_genesis_dir is optional: stateful-engine fixtures boot from a
		// pre-populated snapshot datadir (configured via runner.client.datadirs)
		// and carry no genesis. Validate it only when provided.
		if e.LocalGenesisDir != "" {
			if err := validateDirExists(e.LocalGenesisDir, "eest_fixtures.local_genesis_dir"); err != nil {
				return err
			}
		}
	}

	// Validate local tarball mode.
	if hasLocalTarball {
		if e.LocalFixturesTarball == "" {
			return fmt.Errorf("eest_fixtures: local_fixtures_tarball is required when local_genesis_tarball is set")
		}

		if e.LocalGenesisTarball == "" {
			return fmt.Errorf("eest_fixtures: local_genesis_tarball is required when local_fixtures_tarball is set")
		}

		if err := validateFileExists(e.LocalFixturesTarball, "eest_fixtures.local_fixtures_tarball"); err != nil {
			return err
		}

		if err := validateFileExists(e.LocalGenesisTarball, "eest_fixtures.local_genesis_tarball"); err != nil {
			return err
		}
	}

	return nil
}

// validateDirExists checks that the given path exists and is a directory.
func validateDirExists(path, field string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: path %q does not exist", field, path)
		}

		return fmt.Errorf("%s: checking path: %w", field, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%s: path %q is not a directory", field, path)
	}

	return nil
}

// validateFileExists checks that the given path exists and is a regular file.
func validateFileExists(path, field string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: path %q does not exist", field, path)
		}

		return fmt.Errorf("%s: checking path: %w", field, err)
	}

	if info.IsDir() {
		return fmt.Errorf("%s: path %q is a directory, expected a file", field, path)
	}

	return nil
}

// DefaultEESTFixturesSubdir is the default subdirectory within the fixtures tarball.
const DefaultEESTFixturesSubdir = "fixtures/blockchain_tests_engine_x"

// GitSourceV2 defines a git repository source for tests with step-based structure.
type GitSourceV2 struct {
	Repo        string       `yaml:"repo" mapstructure:"repo"`
	Version     string       `yaml:"version" mapstructure:"version"`
	PreRunSteps []string     `yaml:"pre_run_steps,omitempty" mapstructure:"pre_run_steps"`
	Steps       *StepsConfig `yaml:"steps,omitempty" mapstructure:"steps"`
}

// LocalSourceV2 defines a local directory source for tests with step-based structure.
type LocalSourceV2 struct {
	BaseDir     string       `yaml:"base_dir" mapstructure:"base_dir"`
	PreRunSteps []string     `yaml:"pre_run_steps,omitempty" mapstructure:"pre_run_steps"`
	Steps       *StepsConfig `yaml:"steps,omitempty" mapstructure:"steps"`
}

// ArchiveSourceConfig defines an archive file source for tests.
// The file can be a local path or a URL (HTTP/HTTPS) to a ZIP or tar.gz archive.
// Alternatively, `parts` can be a list of paths/URLs that are concatenated in
// order into the final archive (useful when the archive is split across
// multiple files because of per-asset size limits). `file` and `parts` are
// mutually exclusive.
type ArchiveSourceConfig struct {
	File        string       `yaml:"file,omitempty" mapstructure:"file"`
	Parts       []string     `yaml:"parts,omitempty" mapstructure:"parts"`
	PreRunSteps []string     `yaml:"pre_run_steps,omitempty" mapstructure:"pre_run_steps"`
	Steps       *StepsConfig `yaml:"steps,omitempty" mapstructure:"steps"`
}

// OpcodeSourceConfig defines an external opcode metadata file.
// The file is a JSON map of test name → opcode → count.
//
// Two modes are supported:
//   - Direct file: set File to a local path or URL pointing at the JSON file.
//   - Archive: set Archive to a .zip / .tar.gz (or GitHub Actions artifact URL).
//     The archive is downloaded + extracted, and File is then the filename
//     to look up inside the extracted tree.
type OpcodeSourceConfig struct {
	File    string `yaml:"file" mapstructure:"file"`                 // JSON file path — inside the archive when Archive is set, otherwise a local path or URL.
	Archive string `yaml:"archive,omitempty" mapstructure:"archive"` // Optional local path or URL to a .zip / .tar.gz archive containing File.
}

// StepsConfig defines glob patterns for each step type.
type StepsConfig struct {
	Setup   []string `yaml:"setup,omitempty" mapstructure:"setup"`
	Test    []string `yaml:"test,omitempty" mapstructure:"test"`
	Cleanup []string `yaml:"cleanup,omitempty" mapstructure:"cleanup"`
}

// IsConfigured returns true if any test source is configured.
func (s *SourceConfig) IsConfigured() bool {
	return s.Git != nil || s.Local != nil || s.Archive != nil || s.EESTFixtures != nil
}

// DefaultContainerDir is the default container mount path for data directories.
const DefaultContainerDir = "/data"

// GenesisEIPOverride activates a set of EIPs at a given timestamp in a
// parity/nethermind-format chainspec (which schedules forks per-EIP rather than
// by fork name). It is the parity-format counterpart of GenesisForkOverride: the
// devnet-specific EIP list lives in config and benchmarkoor patches the
// chainspec at boot.
type GenesisEIPOverride struct {
	// Timestamp is the activation time (unix seconds) applied to every listed EIP.
	Timestamp uint64 `yaml:"timestamp" mapstructure:"timestamp"`
	// EIPs are the EIP numbers to activate, e.g. [7928, 8037]. Each becomes a
	// params.eip<N>TransitionTimestamp entry.
	EIPs []uint64 `yaml:"eips" mapstructure:"eips"`
}

// PreRunPredeploy configures a pre-run that crosses a fork boundary at build
// time: it boots the filler at PreFork (the fork the snapshot is at), deploys
// Contracts via CREATE transactions on that fork, then lets the chain cross into
// the target fork (builder.pre_runs.config.fork) at genesis_eip_override's
// timestamp for the gas-bump and fill. This is how an osaka snapshot gets the
// amsterdam system contracts (e.g. EIP-8282) deployed before amsterdam
// activates, since a strict client rejects amsterdam blocks whose system
// contracts have no code.
type PreRunPredeploy struct {
	// PreFork is the fork the snapshot boots at, used for the funding and deploy
	// blocks built before the target fork activates (e.g. "osaka").
	PreFork string `yaml:"pre_fork" mapstructure:"pre_fork"`
	// DeployerKey is the 0x-prefixed private key that funds (via the pre-run
	// funding block) and signs the CREATE transactions. Its contracts land at the
	// CREATE addresses of this account at nonces 0..n-1.
	DeployerKey string `yaml:"deployer_key" mapstructure:"deployer_key"`
	// DeployerFundGwei is the withdrawal amount credited to the deployer in the
	// funding block (default DefaultPreRunFundingAmountGwei).
	DeployerFundGwei *uint64 `yaml:"deployer_fund_gwei,omitempty" mapstructure:"deployer_fund_gwei"`
	// Contracts are the runtime bytecodes to deploy, in order.
	Contracts []PreRunPredeployContract `yaml:"contracts" mapstructure:"contracts"`
}

// PreRunPredeployContract is one contract deployed by a PreRunPredeploy: Code is
// the 0x-prefixed runtime bytecode (what ends up as the deployed account's code;
// benchmarkoor wraps it in the minimal returning init code).
type PreRunPredeployContract struct {
	Code string `yaml:"code" mapstructure:"code"`
}

// DataDirConfig configures a pre-populated data directory for a client.
type DataDirConfig struct {
	SourceDir    string `yaml:"source_dir" json:"source_dir" mapstructure:"source_dir"`
	ContainerDir string `yaml:"container_dir,omitempty" json:"container_dir,omitempty" mapstructure:"container_dir"`
	Method       string `yaml:"method,omitempty" json:"method,omitempty" mapstructure:"method"`
}

// RetryNewPayloadsSyncingConfig configures retry behavior when engine_newPayload returns SYNCING.
type RetryNewPayloadsSyncingConfig struct {
	Enabled    bool   `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
	MaxRetries int    `yaml:"max_retries" mapstructure:"max_retries" json:"max_retries"`
	Backoff    string `yaml:"backoff" mapstructure:"backoff" json:"backoff"`
}

// RetryNewPayloadsFailedConfig configures retry behavior when engine_newPayload
// fails for any reason other than SYNCING (RPC/network error, JSON-RPC error,
// non-VALID payload status, etc.). When both this and the SYNCING config are
// enabled, SYNCING errors take the SYNCING retry path and everything else
// takes this path.
type RetryNewPayloadsFailedConfig struct {
	Enabled    bool   `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
	MaxRetries int    `yaml:"max_retries" mapstructure:"max_retries" json:"max_retries"`
	Backoff    string `yaml:"backoff" mapstructure:"backoff" json:"backoff"`
}

// CheckpointRestoreStrategyOptions configures options for the checkpoint-restore
// rollback strategy (CRIU-based checkpoint/restore with Podman).
type CheckpointRestoreStrategyOptions struct {
	TmpfsThreshold        string `yaml:"tmpfs_threshold,omitempty" mapstructure:"tmpfs_threshold" json:"tmpfs_threshold,omitempty"`
	TmpfsMaxSize          string `yaml:"tmpfs_max_size,omitempty" mapstructure:"tmpfs_max_size" json:"tmpfs_max_size,omitempty"`
	WaitAfterTCPDropConns string `yaml:"wait_after_tcp_drop_connections,omitempty" mapstructure:"wait_after_tcp_drop_connections" json:"wait_after_tcp_drop_connections,omitempty"`
	RestartContainer      bool   `yaml:"restart_container,omitempty" mapstructure:"restart_container" json:"restart_container,omitempty"`
}

// BootstrapFCUConfig configures the bootstrap FCU call used to confirm the
// client is fully synced and ready for test execution.
type BootstrapFCUConfig struct {
	Enabled       bool   `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
	MaxRetries    int    `yaml:"max_retries" mapstructure:"max_retries" json:"max_retries"`
	Backoff       string `yaml:"backoff" mapstructure:"backoff" json:"backoff"`
	HeadBlockHash string `yaml:"head_block_hash" mapstructure:"head_block_hash" json:"head_block_hash,omitempty"`
}

// DefaultOpcodeExtractionTimeout is the per-block trace timeout applied
// when opcode_extraction.timeout is unset. debug_traceBlockByNumber on
// fat blocks can run for many seconds; 2 minutes covers most cases
// without leaving a runaway trace stuck forever.
const DefaultOpcodeExtractionTimeout = "2m"

// OpcodeExtractionConfig configures the post-test opcode-extraction
// step that runs debug_traceBlockByNumber against each engine_newPayload*
// in the test step using a JS tracer that counts opcodes per tx. Counts
// are summed across txs (and uppercased) and written into a single
// test-opcodes.json file at the end of the run.
type OpcodeExtractionConfig struct {
	Enabled bool   `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
	Timeout string `yaml:"timeout,omitempty" mapstructure:"timeout" json:"timeout,omitempty"`
}

// EffectiveTimeout returns the per-block trace timeout, defaulting to
// DefaultOpcodeExtractionTimeout when unset/empty.
func (c *OpcodeExtractionConfig) EffectiveTimeout() time.Duration {
	if c == nil || c.Timeout == "" {
		d, _ := time.ParseDuration(DefaultOpcodeExtractionTimeout)

		return d
	}

	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		fallback, _ := time.ParseDuration(DefaultOpcodeExtractionTimeout)

		return fallback
	}

	return d
}

// PostTestRPCCall defines an arbitrary RPC call to execute after the test step.
type PostTestRPCCall struct {
	Method  string     `yaml:"method" mapstructure:"method" json:"method"`
	Params  []any      `yaml:"params" mapstructure:"params" json:"params"`
	Timeout string     `yaml:"timeout,omitempty" mapstructure:"timeout" json:"timeout,omitempty"`
	Dump    DumpConfig `yaml:"dump" mapstructure:"dump" json:"dump,omitempty"`
}

// DumpConfig configures response dumping for a post-test RPC call.
type DumpConfig struct {
	Enabled  bool   `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
	Filename string `yaml:"filename,omitempty" mapstructure:"filename" json:"filename,omitempty"`
}

// ResourceLimits configures container resource constraints.
type ResourceLimits struct {
	CpusetCount   *int         `yaml:"cpuset_count,omitempty" mapstructure:"cpuset_count" json:"cpuset_count,omitempty"`
	Cpuset        []int        `yaml:"cpuset,omitempty" mapstructure:"cpuset" json:"cpuset,omitempty"`
	Memory        string       `yaml:"memory,omitempty" mapstructure:"memory" json:"memory,omitempty"`
	SwapDisabled  bool         `yaml:"swap_disabled,omitempty" mapstructure:"swap_disabled" json:"swap_disabled,omitempty"`
	BlkioConfig   *BlkioConfig `yaml:"blkio_config,omitempty" mapstructure:"blkio_config" json:"blkio_config,omitempty"`
	CPUFreq       string       `yaml:"cpu_freq,omitempty" mapstructure:"cpu_freq" json:"cpu_freq,omitempty"`
	CPUTurboBoost *bool        `yaml:"cpu_turboboost,omitempty" mapstructure:"cpu_turboboost" json:"cpu_turboboost,omitempty"`
	CPUGovernor   string       `yaml:"cpu_freq_governor,omitempty" mapstructure:"cpu_freq_governor" json:"cpu_freq_governor,omitempty"`
}

// BlkioConfig configures container block I/O limits.
type BlkioConfig struct {
	DeviceReadBps   []ThrottleDevice `yaml:"device_read_bps,omitempty" mapstructure:"device_read_bps" json:"device_read_bps,omitempty"`
	DeviceReadIOps  []ThrottleDevice `yaml:"device_read_iops,omitempty" mapstructure:"device_read_iops" json:"device_read_iops,omitempty"`
	DeviceWriteBps  []ThrottleDevice `yaml:"device_write_bps,omitempty" mapstructure:"device_write_bps" json:"device_write_bps,omitempty"`
	DeviceWriteIOps []ThrottleDevice `yaml:"device_write_iops,omitempty" mapstructure:"device_write_iops" json:"device_write_iops,omitempty"`
}

// ThrottleDevice defines a device throttle setting.
type ThrottleDevice struct {
	Path string `yaml:"path" mapstructure:"path" json:"path"`
	Rate string `yaml:"rate" mapstructure:"rate" json:"rate"` // For bps: supports units like "12mb", "1024k". For iops: integer string.
}

// Validate checks the resource limits configuration for errors.
func (r *ResourceLimits) Validate(prefix string) error {
	if r == nil {
		return nil
	}

	// Check mutual exclusivity of cpuset_count and cpuset.
	if r.CpusetCount != nil && len(r.Cpuset) > 0 {
		return fmt.Errorf("%s: cpuset_count and cpuset are mutually exclusive", prefix)
	}

	// Get available CPU count.
	numCPUs, err := cpu.Counts(true)
	if err != nil {
		return fmt.Errorf("%s: failed to get CPU count: %w", prefix, err)
	}

	// Validate cpuset_count.
	if r.CpusetCount != nil {
		if *r.CpusetCount < 1 {
			return fmt.Errorf("%s: cpuset_count must be at least 1", prefix)
		}

		if *r.CpusetCount > numCPUs {
			return fmt.Errorf("%s: cpuset_count (%d) exceeds available CPUs (%d)", prefix, *r.CpusetCount, numCPUs)
		}
	}

	// Validate cpuset.
	if len(r.Cpuset) > 0 {
		seen := make(map[int]struct{}, len(r.Cpuset))

		for _, cpuID := range r.Cpuset {
			if cpuID < 0 || cpuID >= numCPUs {
				return fmt.Errorf("%s: cpuset contains invalid CPU %d (valid range: 0-%d)", prefix, cpuID, numCPUs-1)
			}

			if _, exists := seen[cpuID]; exists {
				return fmt.Errorf("%s: cpuset contains duplicate CPU %d", prefix, cpuID)
			}

			seen[cpuID] = struct{}{}
		}
	}

	// Validate memory format.
	if r.Memory != "" {
		if _, err := units.RAMInBytes(r.Memory); err != nil {
			return fmt.Errorf("%s: invalid memory format %q: %w", prefix, r.Memory, err)
		}
	}

	// Validate blkio_config.
	if r.BlkioConfig != nil {
		if err := r.BlkioConfig.Validate(prefix + ".blkio_config"); err != nil {
			return err
		}
	}

	return nil
}

// Validate checks the blkio configuration for errors.
func (b *BlkioConfig) Validate(prefix string) error {
	// Validate device_read_bps (bandwidth rates).
	for i, dev := range b.DeviceReadBps {
		if err := validateThrottleDeviceBps(dev, fmt.Sprintf("%s.device_read_bps[%d]", prefix, i)); err != nil {
			return err
		}
	}

	// Validate device_write_bps (bandwidth rates).
	for i, dev := range b.DeviceWriteBps {
		if err := validateThrottleDeviceBps(dev, fmt.Sprintf("%s.device_write_bps[%d]", prefix, i)); err != nil {
			return err
		}
	}

	// Validate device_read_iops (IOPS rates).
	for i, dev := range b.DeviceReadIOps {
		if err := validateThrottleDeviceIOps(dev, fmt.Sprintf("%s.device_read_iops[%d]", prefix, i)); err != nil {
			return err
		}
	}

	// Validate device_write_iops (IOPS rates).
	for i, dev := range b.DeviceWriteIOps {
		if err := validateThrottleDeviceIOps(dev, fmt.Sprintf("%s.device_write_iops[%d]", prefix, i)); err != nil {
			return err
		}
	}

	return nil
}

// validateThrottleDeviceBps validates a throttle device for bandwidth (bps) limits.
func validateThrottleDeviceBps(dev ThrottleDevice, prefix string) error {
	if dev.Path == "" {
		return fmt.Errorf("%s: path is required", prefix)
	}

	if dev.Rate == "" {
		return fmt.Errorf("%s: rate is required", prefix)
	}

	if _, err := units.RAMInBytes(dev.Rate); err != nil {
		return fmt.Errorf("%s: invalid rate format %q: %w", prefix, dev.Rate, err)
	}

	return nil
}

// validateThrottleDeviceIOps validates a throttle device for IOPS limits.
func validateThrottleDeviceIOps(dev ThrottleDevice, prefix string) error {
	if dev.Path == "" {
		return fmt.Errorf("%s: path is required", prefix)
	}

	if dev.Rate == "" {
		return fmt.Errorf("%s: rate is required", prefix)
	}

	rate, err := strconv.ParseUint(dev.Rate, 10, 64)
	if err != nil {
		return fmt.Errorf("%s: invalid iops rate %q (must be a positive integer): %w", prefix, dev.Rate, err)
	}

	if rate == 0 {
		return fmt.Errorf("%s: iops rate must be greater than 0", prefix)
	}

	return nil
}

// Validate checks the datadir configuration for errors.
func (d *DataDirConfig) Validate(prefix string) error {
	if d.SourceDir == "" {
		return fmt.Errorf("%s: source_dir is required", prefix)
	}

	validMethods := map[string]bool{"": true, "copy": true, "overlayfs": true, "fuse-overlayfs": true, "zfs": true, "direct": true, "schelk": true}
	if !validMethods[d.Method] {
		return fmt.Errorf("%s: invalid method %q, must be: copy, overlayfs, fuse-overlayfs, zfs, direct, schelk", prefix, d.Method)
	}

	// For method=schelk, source_dir lives under a schelk-managed mount
	// that may not be mounted yet at config load time. validateDataDirMethods
	// performs the mount and re-checks existence afterwards.
	if d.Method == "schelk" {
		return nil
	}

	info, err := os.Stat(d.SourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: source_dir %q does not exist", prefix, d.SourceDir)
		}

		return fmt.Errorf("%s: checking source_dir: %w", prefix, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%s: source_dir %q is not a directory", prefix, d.SourceDir)
	}

	return nil
}

// ClientConfig contains client configuration settings.
type ClientConfig struct {
	Config   ClientDefaults            `yaml:"config" mapstructure:"config"`
	DataDirs map[string]*DataDirConfig `yaml:"datadirs,omitempty" mapstructure:"datadirs"`
}

// ClientDefaults contains default settings for all clients.
type ClientDefaults struct {
	JWT                              string                            `yaml:"jwt" mapstructure:"jwt"`
	Genesis                          map[string]string                 `yaml:"genesis" mapstructure:"genesis"`
	DropMemoryCaches                 string                            `yaml:"drop_memory_caches,omitempty" mapstructure:"drop_memory_caches"`
	RollbackStrategy                 string                            `yaml:"rollback_strategy,omitempty" mapstructure:"rollback_strategy"`
	ResourceLimits                   *ResourceLimits                   `yaml:"resource_limits,omitempty" mapstructure:"resource_limits"`
	RetryNewPayloadsSyncingState     *RetryNewPayloadsSyncingConfig    `yaml:"retry_new_payloads_syncing_state,omitempty" mapstructure:"retry_new_payloads_syncing_state"`
	RetryNewPayloadsFailedState      *RetryNewPayloadsFailedConfig     `yaml:"retry_new_payloads_failed_state,omitempty" mapstructure:"retry_new_payloads_failed_state"`
	WaitAfterRPCReady                string                            `yaml:"wait_after_rpc_ready,omitempty" mapstructure:"wait_after_rpc_ready"`
	RunTimeout                       string                            `yaml:"run_timeout,omitempty" mapstructure:"run_timeout"`
	PostTestRPCCalls                 []PostTestRPCCall                 `yaml:"post_test_rpc_calls,omitempty" mapstructure:"post_test_rpc_calls"`
	PostTestSleepDuration            string                            `yaml:"post_test_sleep_duration,omitempty" mapstructure:"post_test_sleep_duration"`
	BootstrapFCU                     *BootstrapFCUConfig               `yaml:"bootstrap_fcu,omitempty" mapstructure:"bootstrap_fcu"`
	CheckpointRestoreStrategyOptions *CheckpointRestoreStrategyOptions `yaml:"checkpoint_restore_strategy_options,omitempty" mapstructure:"checkpoint_restore_strategy_options"`
	OpcodeExtraction                 *OpcodeExtractionConfig           `yaml:"opcode_extraction,omitempty" mapstructure:"opcode_extraction"`
	Metadata                         MetadataConfig                    `yaml:"metadata,omitempty" mapstructure:"metadata"`
}

// ClientInstance defines a single client instance to benchmark.
type ClientInstance struct {
	ID                               string                            `yaml:"id" mapstructure:"id"`
	Client                           string                            `yaml:"client" mapstructure:"client"`
	Image                            string                            `yaml:"image,omitempty" mapstructure:"image"`
	Entrypoint                       []string                          `yaml:"entrypoint,omitempty" mapstructure:"entrypoint"`
	Command                          []string                          `yaml:"command,omitempty" mapstructure:"command"`
	ExtraArgs                        []string                          `yaml:"extra_args,omitempty" mapstructure:"extra_args"`
	PullPolicy                       string                            `yaml:"pull_policy,omitempty" mapstructure:"pull_policy"`
	Restart                          string                            `yaml:"restart,omitempty" mapstructure:"restart"`
	Environment                      map[string]string                 `yaml:"environment,omitempty" mapstructure:"environment"`
	Genesis                          string                            `yaml:"genesis,omitempty" mapstructure:"genesis"`
	GenesisForkOverride              map[string]uint64                 `yaml:"genesis_fork_override,omitempty" mapstructure:"genesis_fork_override"`
	GenesisEIPOverride               *GenesisEIPOverride               `yaml:"genesis_eip_override,omitempty" mapstructure:"genesis_eip_override"`
	DataDir                          *DataDirConfig                    `yaml:"datadir,omitempty" mapstructure:"datadir"`
	DropMemoryCaches                 string                            `yaml:"drop_memory_caches,omitempty" mapstructure:"drop_memory_caches"`
	RollbackStrategy                 string                            `yaml:"rollback_strategy,omitempty" mapstructure:"rollback_strategy"`
	ResourceLimits                   *ResourceLimits                   `yaml:"resource_limits,omitempty" mapstructure:"resource_limits"`
	RetryNewPayloadsSyncingState     *RetryNewPayloadsSyncingConfig    `yaml:"retry_new_payloads_syncing_state,omitempty" mapstructure:"retry_new_payloads_syncing_state"`
	RetryNewPayloadsFailedState      *RetryNewPayloadsFailedConfig     `yaml:"retry_new_payloads_failed_state,omitempty" mapstructure:"retry_new_payloads_failed_state"`
	WaitAfterRPCReady                string                            `yaml:"wait_after_rpc_ready,omitempty" mapstructure:"wait_after_rpc_ready"`
	RunTimeout                       string                            `yaml:"run_timeout,omitempty" mapstructure:"run_timeout"`
	PostTestRPCCalls                 []PostTestRPCCall                 `yaml:"post_test_rpc_calls,omitempty" mapstructure:"post_test_rpc_calls"`
	PostTestSleepDuration            string                            `yaml:"post_test_sleep_duration,omitempty" mapstructure:"post_test_sleep_duration"`
	BootstrapFCU                     *BootstrapFCUConfig               `yaml:"bootstrap_fcu,omitempty" mapstructure:"bootstrap_fcu"`
	CheckpointRestoreStrategyOptions *CheckpointRestoreStrategyOptions `yaml:"checkpoint_restore_strategy_options,omitempty" mapstructure:"checkpoint_restore_strategy_options"`
	OpcodeExtraction                 *OpcodeExtractionConfig           `yaml:"opcode_extraction,omitempty" mapstructure:"opcode_extraction"`
	Metadata                         MetadataConfig                    `yaml:"metadata,omitempty" mapstructure:"metadata"`
}

// expandEnvWithDefaults is a mapping function for os.Expand that supports
// bash-style default values: ${VAR:-default} returns "default" when VAR is
// unset or empty. Plain variable references (${VAR} / $VAR) behave like
// os.Getenv.
func expandEnvWithDefaults(s string) string {
	name, defaultVal, hasDefault := strings.Cut(s, ":-")
	if hasDefault {
		if v := os.Getenv(name); v != "" {
			return v
		}

		return defaultVal
	}

	return os.Getenv(s)
}

// rawGlobalEnv is a minimal struct used to read global.env from the raw
// (pre-expansion) YAML. Read this way rather than from the parsed Config so the
// keys keep their original casing (Viper lowercases all map keys), since they
// are used as case-sensitive ${VAR} substitution names.
type rawGlobalEnv struct {
	Global struct {
		Env map[string]string `yaml:"env"`
	} `yaml:"global"`
}

// collectGlobalEnv parses global.env from each raw config and merges them
// (later files win). A value may itself reference the shell environment (e.g.
// "${BASE:-/tmp}/state-actor"), which is expanded here; values do not see one
// another.
func collectGlobalEnv(contents []string) map[string]string {
	env := make(map[string]string)

	for _, content := range contents {
		var rg rawGlobalEnv
		if err := yaml.Unmarshal([]byte(content), &rg); err != nil {
			continue
		}

		for k, val := range rg.Global.Env {
			env[k] = os.Expand(val, expandEnvWithDefaults)
		}
	}

	return env
}

// envExpander returns the os.Expand mapping used for ${VAR} / ${VAR:-default}
// substitution across config files. Resolution order is: the shell environment,
// then global.env from the config, then the inline default. Keeping the shell
// first means global.env acts as a per-config default that an env var can still
// override (e.g. in CI).
func envExpander(contents []string) func(string) string {
	globalEnv := collectGlobalEnv(contents)

	return func(s string) string {
		name, defaultVal, hasDefault := strings.Cut(s, ":-")

		if v := os.Getenv(name); v != "" {
			return v
		}

		if v, ok := globalEnv[name]; ok && v != "" {
			return v
		}

		if hasDefault {
			return defaultVal
		}

		return ""
	}
}

// Load reads and parses configuration files from the given paths.
// When multiple paths are provided, configs are merged in order (later values override earlier).
// Environment variables can be substituted in config values using ${VAR}, $VAR, or
// ${VAR:-default} syntax (the default is used when VAR is unset or empty).
// Additionally, environment variables with the prefix BENCHMARKOOR_ can override config values.
// For example, BENCHMARKOOR_GLOBAL_LOG_LEVEL overrides global.log_level.
func Load(paths ...string) (*Config, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one config path is required")
	}

	v := viper.New()

	// Configure environment variable handling for overrides.
	v.SetEnvPrefix("BENCHMARKOOR")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetConfigType("yaml")

	// Read every file up front so global.env (which may live in any of them) is
	// known before we expand ${VAR} references.
	contents := make([]string, 0, len(paths))

	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading config file %q: %w", path, err)
		}

		contents = append(contents, string(content))
	}

	expand := envExpander(contents)

	// Load and merge configs in order, collecting expanded YAML for
	// post-processing (Viper lowercases map keys, so we re-parse to
	// restore original casing for environment variables).
	rawYAMLs := make([]string, 0, len(paths))

	for i, content := range contents {
		expanded := os.Expand(content, expand)
		rawYAMLs = append(rawYAMLs, expanded)

		if i == 0 {
			if err := v.ReadConfig(strings.NewReader(expanded)); err != nil {
				return nil, fmt.Errorf("parsing config %q: %w", paths[i], err)
			}
		} else {
			if err := v.MergeConfig(strings.NewReader(expanded)); err != nil {
				return nil, fmt.Errorf("merging config %q: %w", paths[i], err)
			}
		}
	}

	// Bind all known configuration keys to allow env var overrides.
	bindEnvKeys(v)

	var cfg Config
	if err := v.Unmarshal(&cfg, viper.DecodeHook(
		mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
			dumpConfigDecodeHook(),
			bootstrapFCUDecodeHook(),
		),
	)); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	restoreEnvironmentKeyCasing(&cfg, rawYAMLs)
	restoreAddressStubsKeyCasing(&cfg, rawYAMLs)
	restorePreRunFillEnvKeyCasing(&cfg, rawYAMLs)
	normalizeStateActorSpec(&cfg, rawYAMLs)

	cfg.applyDefaults()

	return &cfg, nil
}

// bindEnvKeys explicitly binds configuration keys to environment variables.
// This is required for Viper to recognize env vars for keys not present in the config file.
func bindEnvKeys(v *viper.Viper) {
	keys := []string{
		// Global settings
		"global.log_level",
		"global.directories.cachedir",
		// Builder settings
		"builder.run_timeout",
		"builder.cleanup_on_start",
		// Runner settings
		"runner.container_runtime",
		"runner.client_logs_to_stdout",
		"runner.container_network",
		"runner.cleanup_on_start",
		"runner.run_timeout",
		"runner.directories.tmp_datadir",
		"runner.directories.tmp_cachedir",
		"runner.github_token",
		"runner.drop_caches_path",
		"runner.cpu_sysfs_path",
		// Runner benchmark settings
		"runner.benchmark.results_dir",
		"runner.benchmark.results_owner",
		"runner.benchmark.skip_test_run",
		"runner.benchmark.system_resource_collection_enabled",
		"runner.benchmark.generate_results_index",
		"runner.benchmark.generate_results_index_method",
		"runner.benchmark.generate_suite_stats",
		"runner.benchmark.generate_suite_stats_method",
		"runner.benchmark.tests.filter",
		// Runner client settings
		"runner.client.config.jwt",
		"runner.client.config.drop_memory_caches",
		"runner.client.config.rollback_strategy",
		"runner.client.config.wait_after_rpc_ready",
		"runner.client.config.run_timeout",
		// Runner client resource limits
		"runner.client.config.resource_limits.cpuset_count",
		"runner.client.config.resource_limits.memory",
		"runner.client.config.resource_limits.swap_disabled",
		"runner.client.config.resource_limits.cpu_freq",
		"runner.client.config.resource_limits.cpu_turboboost",
		"runner.client.config.resource_limits.cpu_freq_governor",
		// Runner client retry new payloads syncing state
		"runner.client.config.retry_new_payloads_syncing_state.enabled",
		"runner.client.config.retry_new_payloads_syncing_state.max_retries",
		"runner.client.config.retry_new_payloads_syncing_state.backoff",
		// Runner client bootstrap FCU
		"runner.client.config.bootstrap_fcu.enabled",
		"runner.client.config.bootstrap_fcu.max_retries",
		"runner.client.config.bootstrap_fcu.backoff",
		"runner.client.config.bootstrap_fcu.head_block_hash",
		// API settings
		"api.server.listen",
		"api.auth.session_ttl",
		"api.auth.github.client_id",
		"api.auth.github.client_secret",
		"api.auth.github.redirect_url",
		"api.database.driver",
		"api.database.sqlite.path",
		"api.database.postgres.host",
		"api.database.postgres.port",
		"api.database.postgres.user",
		"api.database.postgres.password",
		"api.database.postgres.database",
		"api.database.postgres.ssl_mode",
		// API storage settings
		"api.storage.s3.enabled",
		"api.storage.s3.endpoint_url",
		"api.storage.s3.region",
		"api.storage.s3.bucket",
		"api.storage.s3.access_key_id",
		"api.storage.s3.secret_access_key",
		"api.storage.s3.force_path_style",
		"api.storage.s3.presigned_urls.expiry",
		// Builder settings
		"builder.state_actor.container_runtime",
		"builder.state_actor.pull_policy",
		"builder.eest_payloads.container_runtime",
		"builder.eest_payloads.pull_policy",
	}

	for _, key := range keys {
		_ = v.BindEnv(key)
	}
}

// applyDefaults sets default values for unspecified configuration options.
func (c *Config) applyDefaults() {
	if c.Global.LogLevel == "" {
		c.Global.LogLevel = DefaultLogLevel
	}

	if c.Runner.ContainerNetwork == "" {
		c.Runner.ContainerNetwork = DefaultContainerNetwork
	}

	if c.Runner.Benchmark.ResultsDir == "" {
		c.Runner.Benchmark.ResultsDir = DefaultResultsDir
	}

	if c.Runner.Benchmark.SystemResourceCollectionEnabled == nil {
		enabled := true
		c.Runner.Benchmark.SystemResourceCollectionEnabled = &enabled
	}

	if c.Runner.Client.Config.JWT == "" {
		c.Runner.Client.Config.JWT = DefaultJWT
	}

	if c.Runner.Client.Config.Genesis == nil {
		c.Runner.Client.Config.Genesis = make(map[string]string, 6)
	}

	if c.Runner.Benchmark.ResultsUpload != nil &&
		c.Runner.Benchmark.ResultsUpload.S3 != nil &&
		c.Runner.Benchmark.ResultsUpload.S3.ParallelUploads == 0 {
		c.Runner.Benchmark.ResultsUpload.S3.ParallelUploads = 50
	}

	// Apply defaults to global datadirs.
	for _, dd := range c.Runner.Client.DataDirs {
		if dd != nil {
			if dd.Method == "" {
				dd.Method = "copy"
			}
			// Note: ContainerDir is intentionally not defaulted here.
			// If empty, the runner will use the client's spec.DataDir() at runtime.
		}
	}

	// Apply API defaults.
	if c.API != nil {
		if c.API.Server.Listen == "" {
			c.API.Server.Listen = ":9090"
		}

		if c.API.Auth.SessionTTL == "" {
			c.API.Auth.SessionTTL = "24h"
		}

		if c.API.Database.Driver == "" {
			c.API.Database.Driver = "sqlite"
		}

		if c.API.Database.Driver == "sqlite" && c.API.Database.SQLite.Path == "" {
			c.API.Database.SQLite.Path = "benchmarkoor.db"
		}

		if c.API.Database.Driver == "postgres" {
			if c.API.Database.Postgres.Port == 0 {
				c.API.Database.Postgres.Port = 5432
			}

			if c.API.Database.Postgres.SSLMode == "" {
				c.API.Database.Postgres.SSLMode = "disable"
			}
		}

		// Apply S3 storage defaults.
		if c.API.Storage.S3 != nil && c.API.Storage.S3.Enabled {
			if c.API.Storage.S3.Region == "" {
				c.API.Storage.S3.Region = "us-east-1"
			}

			if c.API.Storage.S3.PresignedURLs.Expiry == "" {
				c.API.Storage.S3.PresignedURLs.Expiry = "1h"
			}
		}

		if c.API.Server.RateLimit.Enabled {
			if c.API.Server.RateLimit.Auth.RequestsPerMinute == 0 {
				c.API.Server.RateLimit.Auth.RequestsPerMinute = 10
			}

			if c.API.Server.RateLimit.Public.RequestsPerMinute == 0 {
				c.API.Server.RateLimit.Public.RequestsPerMinute = 60
			}

			if c.API.Server.RateLimit.Authenticated.RequestsPerMinute == 0 {
				c.API.Server.RateLimit.Authenticated.RequestsPerMinute = 120
			}
		}
	}

	for i := range c.Runner.Instances {
		if c.Runner.Instances[i].PullPolicy == "" {
			c.Runner.Instances[i].PullPolicy = DefaultPullPolicy
		}

		// Apply defaults to instance-level datadir.
		if c.Runner.Instances[i].DataDir != nil {
			if c.Runner.Instances[i].DataDir.Method == "" {
				c.Runner.Instances[i].DataDir.Method = "copy"
			}
			// Note: ContainerDir is intentionally not defaulted here.
			// If empty, the runner will use the client's spec.DataDir() at runtime.
		}
	}

	// Apply builder.state_actor defaults. ContainerRuntime is intentionally
	// left empty so GetStateActorContainerRuntime can fall back to the
	// runner's runtime at call time.
	if c.Builder != nil && c.Builder.StateActor != nil {
		if c.Builder.StateActor.PullPolicy == "" {
			c.Builder.StateActor.PullPolicy = DefaultPullPolicy
		}
	}

	// Apply builder.eest_payloads defaults. ContainerRuntime is left empty
	// so GetEESTPayloadsContainerRuntime can fall back at call time; JWT
	// defaults to DefaultJWT so the filler client and fill-stateful share it.
	if c.Builder != nil && c.Builder.EESTPayloads != nil {
		if c.Builder.EESTPayloads.PullPolicy == "" {
			c.Builder.EESTPayloads.PullPolicy = DefaultPullPolicy
		}

		if c.Builder.EESTPayloads.JWT == "" {
			c.Builder.EESTPayloads.JWT = DefaultJWT
		}
	}

	// Apply builder.pre_runs defaults, mirroring eest_payloads: ContainerRuntime
	// falls back at call time; JWT defaults to DefaultJWT so the filler client,
	// benchmarkoor's own engine calls, and fill-stateful all share it.
	if c.Builder != nil && c.Builder.PreRuns != nil {
		if c.Builder.PreRuns.PullPolicy == "" {
			c.Builder.PreRuns.PullPolicy = DefaultPullPolicy
		}

		if c.Builder.PreRuns.JWT == "" {
			c.Builder.PreRuns.JWT = DefaultJWT
		}
	}
}

// GetStateActorContainerRuntime returns the container runtime to use for
// state-actor builds. Falls back to the runner's runtime when the builder
// block does not override it.
func (c *Config) GetStateActorContainerRuntime() string {
	if c.Builder != nil && c.Builder.StateActor != nil && c.Builder.StateActor.ContainerRuntime != "" {
		return c.Builder.StateActor.ContainerRuntime
	}

	return c.GetContainerRuntime()
}

// GetEESTPayloadsContainerRuntime returns the container runtime to use for
// eest_payloads builds. Falls back to the runner's runtime when the builder
// block does not override it.
func (c *Config) GetEESTPayloadsContainerRuntime() string {
	if c.Builder != nil && c.Builder.EESTPayloads != nil && c.Builder.EESTPayloads.ContainerRuntime != "" {
		return c.Builder.EESTPayloads.ContainerRuntime
	}

	return c.GetContainerRuntime()
}

// ValidateOpts controls optional validation behavior.
type ValidateOpts struct {
	// ActiveInstanceIDs limits validation to instances with these IDs.
	// When nil or empty, all instances are validated.
	ActiveInstanceIDs map[string]struct{}
	// ActiveClients limits global datadir validation to these client types.
	// When nil or empty, all global datadirs are validated.
	ActiveClients map[string]struct{}
}

// isInstanceActive returns true if the instance should be validated.
// When ActiveInstanceIDs is nil or empty, all instances are active.
func (o ValidateOpts) isInstanceActive(id string) bool {
	if len(o.ActiveInstanceIDs) == 0 {
		return true
	}

	_, ok := o.ActiveInstanceIDs[id]

	return ok
}

// Validate checks the configuration for errors.
// When opts is provided, datadir validation is scoped to active instances/clients.
func (c *Config) Validate(opts ...ValidateOpts) error {
	var opt ValidateOpts
	if len(opts) > 0 {
		opt = opts[0]
	}

	if len(c.Runner.Instances) == 0 {
		return fmt.Errorf("at least one client instance must be configured")
	}

	seenIDs := make(map[string]struct{}, len(c.Runner.Instances))

	for i, instance := range c.Runner.Instances {
		if instance.ID == "" {
			return fmt.Errorf("instance %d: id is required", i)
		}

		if _, exists := seenIDs[instance.ID]; exists {
			return fmt.Errorf("instance %d: duplicate id %q", i, instance.ID)
		}

		seenIDs[instance.ID] = struct{}{}

		if instance.Client == "" {
			return fmt.Errorf("instance %q: client type is required", instance.ID)
		}

		if !isValidClient(instance.Client) {
			return fmt.Errorf("instance %q: unknown client type %q", instance.ID, instance.Client)
		}

		// genesis_fork_override (geth-format <fork>Time) and genesis_eip_override
		// (parity eip<N>TransitionTimestamp) patch different genesis formats, so a
		// single instance can set at most one — mirrors the eest_payloads target
		// check. (Without this, the contradiction only surfaces at boot when the
		// override apply rejects the genesis format.)
		if len(instance.GenesisForkOverride) > 0 && instance.GenesisEIPOverride != nil {
			return fmt.Errorf(
				"instance %q: genesis_fork_override and genesis_eip_override are mutually exclusive",
				instance.ID,
			)
		}

		// Validate instance-level datadir (skip if not in active set).
		if instance.DataDir != nil {
			if len(opt.ActiveInstanceIDs) == 0 {
				if err := instance.DataDir.Validate(fmt.Sprintf("instance %q datadir", instance.ID)); err != nil {
					return err
				}
			} else if _, ok := opt.ActiveInstanceIDs[instance.ID]; ok {
				if err := instance.DataDir.Validate(fmt.Sprintf("instance %q datadir", instance.ID)); err != nil {
					return err
				}
			}
		}

		// Validate instance-level resource limits.
		if instance.ResourceLimits != nil {
			if err := instance.ResourceLimits.Validate(fmt.Sprintf("instance %q resource_limits", instance.ID)); err != nil {
				return err
			}
		}
	}

	// Validate global resource limits.
	if c.Runner.Client.Config.ResourceLimits != nil {
		if err := c.Runner.Client.Config.ResourceLimits.Validate("runner.client.config.resource_limits"); err != nil {
			return err
		}
	}

	// Validate global datadirs (skip if client not in active set).
	for client, dd := range c.Runner.Client.DataDirs {
		if dd != nil {
			if len(opt.ActiveClients) == 0 {
				if err := dd.Validate(fmt.Sprintf("client.datadirs.%s", client)); err != nil {
					return err
				}
			} else if _, ok := opt.ActiveClients[client]; ok {
				if err := dd.Validate(fmt.Sprintf("client.datadirs.%s", client)); err != nil {
					return err
				}
			}
		}
	}

	if c.Runner.Benchmark.ResultsDir != "" {
		dir := filepath.Dir(c.Runner.Benchmark.ResultsDir)
		if dir != "." && dir != ".." {
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				return fmt.Errorf("results directory parent %q does not exist", dir)
			}
		}
	}

	// Validate test source configuration.
	if err := c.Runner.Benchmark.Tests.Source.Validate(); err != nil {
		return fmt.Errorf("tests config: %w", err)
	}

	// Validate container_runtime setting.
	if err := c.validateContainerRuntime(); err != nil {
		return err
	}

	// Validate rollback_strategy settings.
	if err := c.validateRollbackStrategy(opt); err != nil {
		return err
	}

	// Validate per-method datadir requirements (binary availability, etc.).
	if err := c.validateDataDirMethods(opt); err != nil {
		return err
	}

	// Validate drop_memory_caches settings.
	if err := c.validateDropMemoryCaches(); err != nil {
		return err
	}

	// Validate cpu_freq settings.
	if err := c.validateCPUFreq(); err != nil {
		return err
	}

	// Validate retry_new_payloads_syncing_state settings.
	if err := c.validateRetryNewPayloadsSyncingState(); err != nil {
		return err
	}

	// Validate retry_new_payloads_failed_state settings.
	if err := c.validateRetryNewPayloadsFailedState(); err != nil {
		return err
	}

	// Validate wait_after_rpc_ready settings.
	if err := c.validateWaitAfterRPCReady(); err != nil {
		return err
	}

	// Validate post_test_sleep_duration settings.
	if err := c.validatePostTestSleepDuration(); err != nil {
		return err
	}

	// Validate run_timeout settings.
	if err := c.validateRunTimeout(); err != nil {
		return err
	}

	// Validate post_test_rpc_calls settings.
	if err := c.validatePostTestRPCCalls(); err != nil {
		return err
	}

	// Validate bootstrap_fcu settings.
	if err := c.validateBootstrapFCU(); err != nil {
		return err
	}

	// Validate opcode_extraction settings.
	if err := c.validateOpcodeExtraction(); err != nil {
		return err
	}

	// Validate results_upload settings.
	if err := c.validateResultsUpload(); err != nil {
		return err
	}

	// Validate live_reporting settings.
	if err := c.validateLiveReporting(); err != nil {
		return err
	}

	// Validate test filter (regex syntax if "regex:" prefixed).
	if err := c.validateTestFilter(); err != nil {
		return err
	}

	// Validate API settings.
	if err := c.ValidateAPI(); err != nil {
		return err
	}

	// Validate builder.state_actor settings.
	if err := c.validateBuilder(); err != nil {
		return err
	}

	return nil
}

// TestFilterRegexPrefix opts the test filter into regex matching when the
// configured value starts with this prefix. Without the prefix, the filter
// is treated as a substring match.
const TestFilterRegexPrefix = "regex:"

// validateTestFilter ensures that, when the test filter uses the "regex:"
// prefix, the trailing expression compiles as a Go regular expression.
// Substring filters are always valid.
func (c *Config) validateTestFilter() error {
	filter := c.Runner.Benchmark.Tests.Filter

	expr, ok := strings.CutPrefix(filter, TestFilterRegexPrefix)
	if !ok {
		return nil
	}

	if _, err := regexp.Compile(expr); err != nil {
		return fmt.Errorf("runner.benchmark.tests.filter: invalid regex %q: %w", expr, err)
	}

	return nil
}

// ValidateBuilder runs the validation rules relevant to `benchmarkoor build`:
// the runner's container_runtime (the builder falls back to it when its
// own container_runtime is unset) plus the builder.state_actor block
// itself. It deliberately skips the runner-side rules (instances,
// resource_limits, test source, rollback strategies, ...) which are
// required only for `benchmarkoor run`.
func (c *Config) ValidateBuilder() error {
	if err := c.validateContainerRuntime(); err != nil {
		return err
	}

	return c.validateBuilder()
}

// validateBuilder validates each configured builder block.
func (c *Config) validateBuilder() error {
	if c.Builder == nil {
		return nil
	}

	if c.Builder.RunTimeout != "" {
		if _, err := time.ParseDuration(c.Builder.RunTimeout); err != nil {
			return fmt.Errorf("invalid builder.run_timeout %q: %w",
				c.Builder.RunTimeout, err)
		}
	}

	if err := c.validateStateActor(); err != nil {
		return err
	}

	if err := c.validatePreRuns(); err != nil {
		return err
	}

	return c.validateEESTPayloads()
}

// validateStateActor enforces the builder.state_actor rules: supported
// clients, single-source-of-truth target_size XOR spec, archive/binary-trie
// applicability, group_depth range, image resolvability, and uniqueness of
// target names and output_dirs.
func (c *Config) validateStateActor() error {
	if c.Builder == nil || c.Builder.StateActor == nil {
		return nil
	}

	sa := c.Builder.StateActor

	if !validContainerRuntimes[sa.ContainerRuntime] {
		return fmt.Errorf(
			"builder.state_actor.container_runtime: invalid value %q "+
				"(must be \"docker\" or \"podman\")", sa.ContainerRuntime,
		)
	}

	if !stateActorValidPullPolicies[sa.PullPolicy] {
		return fmt.Errorf(
			"builder.state_actor.pull_policy: invalid value %q "+
				"(must be \"always\", \"if-not-present\", or \"never\")",
			sa.PullPolicy,
		)
	}

	if sa.Spec != "" && sa.SpecFile != "" {
		return fmt.Errorf(
			"builder.state_actor: spec (inline YAML) and spec_file (host path) are mutually exclusive",
		)
	}

	// spec and target_size are complementary, not mutually exclusive:
	// when both are set state-actor uses the spec and treats target_size
	// as a headroom budget for the auto-fill that follows.
	specKind, _ := sa.ResolveSpec()

	seenOutputs := make(map[string]int, len(sa.Targets))
	seenNames := make(map[string]int, len(sa.Targets))

	for i := range sa.Targets {
		// Resolve the target so applicability rules (archive/binary_trie/
		// group_depth) check the effective value — a global default in
		// builder.state_actor.config combined with the per-target value.
		t := sa.ResolveTarget(i)
		prefix := fmt.Sprintf("builder.state_actor.targets[%d]", i)

		if _, ok := stateActorSupportedClients[t.Client]; !ok {
			return fmt.Errorf(
				"%s.client: %q is not supported by state-actor "+
					"(must be geth, reth, besu, nethermind, ethrex, or erigon)",
				prefix, t.Client,
			)
		}

		name := t.EffectiveName()
		if prev, dup := seenNames[name]; dup {
			return fmt.Errorf(
				"%s: name %q duplicates targets[%d] (set an explicit name to disambiguate)",
				prefix, name, prev,
			)
		}

		seenNames[name] = i

		if t.OutputDir == "" {
			return fmt.Errorf("%s.output_dir is required", prefix)
		}

		if !filepath.IsAbs(t.OutputDir) {
			return fmt.Errorf(
				"%s.output_dir must be an absolute path, got %q",
				prefix, t.OutputDir,
			)
		}

		if prev, dup := seenOutputs[t.OutputDir]; dup {
			return fmt.Errorf(
				"%s.output_dir %q duplicates targets[%d].output_dir",
				prefix, t.OutputDir, prev,
			)
		}

		seenOutputs[t.OutputDir] = i

		if t.TargetSize == "" && specKind == StateActorSpecNone {
			return fmt.Errorf(
				"%s: no source resolved — set target_size on the target, set "+
					"builder.state_actor.config.target_size, or set a top-level "+
					"builder.state_actor.spec / spec_file",
				prefix,
			)
		}

		if t.TargetSize != "" {
			if _, err := ParseByteSize(t.TargetSize); err != nil {
				return fmt.Errorf("%s.target_size: %w", prefix, err)
			}
		}

		archive := t.Archive != nil && *t.Archive
		binaryTrie := t.BinaryTrie != nil && *t.BinaryTrie

		if archive && t.Client != "geth" && t.Client != "reth" {
			return fmt.Errorf("%s.archive: only supported for geth and reth", prefix)
		}

		if binaryTrie && t.Client != "geth" {
			return fmt.Errorf("%s.binary_trie: only supported for geth", prefix)
		}

		if t.GroupDepth != nil {
			if !binaryTrie {
				return fmt.Errorf("%s.group_depth: requires binary_trie=true", prefix)
			}

			if *t.GroupDepth < 1 || *t.GroupDepth > 8 {
				return fmt.Errorf(
					"%s.group_depth: must be in range 1..8, got %d",
					prefix, *t.GroupDepth,
				)
			}
		}

		if sa.ImageFor(t.Client) == "" {
			return fmt.Errorf(
				"%s: no image configured for client %q "+
					"(set builder.state_actor.images.%s)",
				prefix, t.Client, t.Client,
			)
		}
	}

	return nil
}

// validateEESTPayloads enforces the builder.eest_payloads rules: a
// configured fill_image, supported filler clients, required locator fields
// (source_dir/output_dir/tests/fork), valid datadir method and
// gas-benchmark values, absolute paths, and uniqueness of target names and
// output_dirs. Existence of source_dir/genesis_file/address_stubs_file is
// checked at build time, not here — a state-actor target earlier in the
// same config may still need to produce them.
func (c *Config) validateEESTPayloads() error {
	if c.Builder == nil || c.Builder.EESTPayloads == nil {
		return nil
	}

	ep := c.Builder.EESTPayloads

	if !validContainerRuntimes[ep.ContainerRuntime] {
		return fmt.Errorf(
			"builder.eest_payloads.container_runtime: invalid value %q "+
				"(must be \"docker\" or \"podman\")", ep.ContainerRuntime,
		)
	}

	if !stateActorValidPullPolicies[ep.PullPolicy] {
		return fmt.Errorf(
			"builder.eest_payloads.pull_policy: invalid value %q "+
				"(must be \"always\", \"if-not-present\", or \"never\")",
			ep.PullPolicy,
		)
	}

	// Neither fill_image nor fill_dockerfile is required: with both unset,
	// benchmarkoor builds the fill image from the Dockerfile embedded in the
	// binary. fill_image pulls a pre-built image; fill_dockerfile builds from a
	// Dockerfile on disk.

	// Reject a missing Dockerfile at config time so typos surface early.
	// Relative paths are resolved against the working directory.
	if ep.FillDockerfile != "" {
		if _, err := os.Stat(ep.FillDockerfile); err != nil {
			return fmt.Errorf("builder.eest_payloads.fill_dockerfile: %w", err)
		}
	}

	seenOutputs := make(map[string]int, len(ep.Targets))
	seenNames := make(map[string]int, len(ep.Targets))

	for i := range ep.Targets {
		t := ep.ResolveTarget(i)
		prefix := fmt.Sprintf("builder.eest_payloads.targets[%d]", i)

		if _, ok := eestFillerSupportedClients[t.FillerClient]; !ok {
			return fmt.Errorf(
				"%s.filler_client: %q cannot act as the fill-stateful filler "+
					"(supported: geth, besu, nethermind)",
				prefix, t.FillerClient,
			)
		}

		name := t.EffectiveName()
		if prev, dup := seenNames[name]; dup {
			return fmt.Errorf(
				"%s: name %q duplicates targets[%d] (set an explicit name to disambiguate)",
				prefix, name, prev,
			)
		}

		seenNames[name] = i

		if err := validateEESTPayloadPaths(&t, prefix, seenOutputs, i); err != nil {
			return err
		}

		if len(t.Tests) == 0 {
			return fmt.Errorf(
				"%s.tests is required (at least one pytest path, e.g. tests/benchmark/compute)",
				prefix,
			)
		}

		if t.Fork == "" {
			return fmt.Errorf(
				"%s.fork is required (set it on the target or builder.eest_payloads.config.fork)",
				prefix,
			)
		}

		if t.FillerImage == "" {
			return fmt.Errorf(
				"%s.filler_image is required (e.g. ethpandaops/geth:master)", prefix,
			)
		}

		if !validDataDirMethods[t.DataDirMethod] {
			return fmt.Errorf(
				"%s.datadir_method: invalid value %q "+
					"(must be copy, overlayfs, fuse-overlayfs, zfs, direct, or schelk)",
				prefix, t.DataDirMethod,
			)
		}

		if err := validateGasBenchmarkValues(t.GasBenchmarkValues, prefix); err != nil {
			return err
		}

		if err := validateFixedOpcodeCount(t.FixedOpcodeCount, prefix); err != nil {
			return err
		}

		if len(t.GasBenchmarkValues) > 0 && t.FixedOpcodeCount != nil {
			return fmt.Errorf(
				"%s: gas_benchmark_values and fixed_opcode_count are mutually exclusive "+
					"(fill-stateful rejects both)", prefix,
			)
		}
	}

	return nil
}

// validateEESTPayloadPaths checks output_dir / genesis /
// address_stubs_file are absolute and output_dir is unique.
func validateEESTPayloadPaths(t *EESTPayloadTarget, prefix string, seenOutputs map[string]int, i int) error {
	if t.SourceDir == "" {
		return fmt.Errorf("%s.source_dir is required", prefix)
	}

	if !filepath.IsAbs(t.SourceDir) {
		return fmt.Errorf("%s.source_dir must be an absolute path, got %q", prefix, t.SourceDir)
	}

	if t.OutputDir == "" {
		return fmt.Errorf("%s.output_dir is required", prefix)
	}

	if !filepath.IsAbs(t.OutputDir) {
		return fmt.Errorf("%s.output_dir must be an absolute path, got %q", prefix, t.OutputDir)
	}

	if prev, dup := seenOutputs[t.OutputDir]; dup {
		return fmt.Errorf(
			"%s.output_dir %q duplicates targets[%d].output_dir", prefix, t.OutputDir, prev,
		)
	}

	seenOutputs[t.OutputDir] = i

	if t.Genesis != "" && !isHTTPURLRef(t.Genesis) && !filepath.IsAbs(t.Genesis) {
		return fmt.Errorf("%s.genesis must be an absolute path or http(s) URL, got %q", prefix, t.Genesis)
	}

	// genesis_fork_override / genesis_eip_override patch the boot genesis, so
	// they require one. (geth/erigon fillers boot from the datadir and use
	// --override.<fork> in filler_extra_args instead.)
	if len(t.GenesisForkOverride) > 0 && t.Genesis == "" {
		return fmt.Errorf("%s.genesis_fork_override requires genesis", prefix)
	}

	if t.GenesisEIPOverride != nil && len(t.GenesisEIPOverride.EIPs) > 0 && t.Genesis == "" {
		return fmt.Errorf("%s.genesis_eip_override requires genesis", prefix)
	}

	if len(t.GenesisForkOverride) > 0 && t.GenesisEIPOverride != nil {
		return fmt.Errorf(
			"%s: genesis_fork_override and genesis_eip_override are mutually exclusive", prefix,
		)
	}

	if t.AddressStubsFile != "" && !filepath.IsAbs(t.AddressStubsFile) {
		return fmt.Errorf(
			"%s.address_stubs_file must be an absolute path, got %q", prefix, t.AddressStubsFile,
		)
	}

	if t.AddressStubsFile != "" && len(t.AddressStubs) > 0 {
		return fmt.Errorf(
			"%s: address_stubs_file and address_stubs are mutually exclusive", prefix,
		)
	}

	for name, stub := range t.AddressStubs {
		if stub["addr"] == "" {
			return fmt.Errorf("%s.address_stubs[%q].addr is required", prefix, name)
		}
	}

	return nil
}

// validateGasBenchmarkValues checks a list of positive integers (millions of
// gas), e.g. [10, 30]. An empty list is allowed.
func validateGasBenchmarkValues(values []int, prefix string) error {
	for _, v := range values {
		if v < 1 {
			return fmt.Errorf(
				"%s.gas_benchmark_values: %d is not a positive integer (millions of gas)", prefix, v,
			)
		}
	}

	return nil
}

// validateFixedOpcodeCount checks a list of positive numbers (thousands of
// opcodes), e.g. [0.5, 1, 2]. A nil pointer (unset) is allowed, as is a
// non-nil empty list (passes the bare --fixed-opcode-count flag, which uses
// the fill image's .fixed_opcode_counts.json default).
func validateFixedOpcodeCount(values *[]float64, prefix string) error {
	if values == nil {
		return nil
	}

	for _, v := range *values {
		if v <= 0 {
			return fmt.Errorf(
				"%s.fixed_opcode_count: %v is not a positive number (thousands of opcodes)", prefix, v,
			)
		}
	}

	return nil
}

// isHTTPURLRef reports whether s is an http(s) URL rather than a local path.
// Genesis/chainspec refs may be either; the builder downloads URLs at build time.
func isHTTPURLRef(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// validDataDirMethods mirrors the datadir.method vocabulary accepted by
// pkg/datadir.NewProvider and DataDirConfig validation.
var validDataDirMethods = map[string]bool{
	"":               true, // unset → copy
	"copy":           true,
	"overlayfs":      true,
	"fuse-overlayfs": true,
	"zfs":            true,
	"direct":         true,
	"schelk":         true,
}

// validateLiveReporting checks the runner.live_reporting config when enabled.
func (c *Config) validateLiveReporting() error {
	lr := c.Runner.LiveReporting
	if lr == nil || !lr.Enabled {
		return nil
	}

	if lr.Endpoint == "" {
		return fmt.Errorf("runner.live_reporting.endpoint is required when enabled")
	}

	if lr.Token == "" {
		return fmt.Errorf("runner.live_reporting.token is required when enabled")
	}

	if lr.DiscoveryPath == "" {
		return fmt.Errorf("runner.live_reporting.discovery_path is required when enabled")
	}

	if lr.Interval != "" {
		if _, err := time.ParseDuration(lr.Interval); err != nil {
			return fmt.Errorf(
				"runner.live_reporting.interval: invalid duration %q: %w",
				lr.Interval, err,
			)
		}
	}

	if lr.Timeout != "" {
		if _, err := time.ParseDuration(lr.Timeout); err != nil {
			return fmt.Errorf(
				"runner.live_reporting.timeout: invalid duration %q: %w",
				lr.Timeout, err,
			)
		}
	}

	if lr.LogsInterval != "" {
		d, err := time.ParseDuration(lr.LogsInterval)
		if err != nil {
			return fmt.Errorf(
				"runner.live_reporting.logs_interval: invalid duration %q: %w",
				lr.LogsInterval, err,
			)
		}

		if d <= 0 {
			return fmt.Errorf(
				"runner.live_reporting.logs_interval: must be > 0, got %v",
				d,
			)
		}
	}

	if lr.JitterFraction >= 1 {
		return fmt.Errorf(
			"runner.live_reporting.jitter_fraction: must be < 1, got %v",
			lr.JitterFraction,
		)
	}

	return nil
}

// Validate checks the source configuration for errors.
func (s *SourceConfig) Validate() error {
	// No source configured is valid (tests are optional).
	if !s.IsConfigured() {
		return nil
	}

	// Count configured sources.
	count := 0
	if s.Git != nil {
		count++
	}

	if s.Local != nil {
		count++
	}

	if s.Archive != nil {
		count++
	}

	if s.EESTFixtures != nil {
		count++
	}

	if count > 1 {
		return fmt.Errorf("cannot specify multiple sources (git, local, archive, eest_fixtures)")
	}

	if s.Git != nil {
		if s.Git.Repo == "" {
			return fmt.Errorf("git.repo is required")
		}

		if s.Git.Version == "" {
			return fmt.Errorf("git.version is required")
		}
	}

	if s.Local != nil {
		if s.Local.BaseDir == "" {
			return fmt.Errorf("local.base_dir is required")
		}

		if _, err := os.Stat(s.Local.BaseDir); os.IsNotExist(err) {
			return fmt.Errorf("local.base_dir %q does not exist", s.Local.BaseDir)
		}
	}

	if s.Archive != nil {
		hasFile := s.Archive.File != ""
		hasParts := len(s.Archive.Parts) > 0

		if !hasFile && !hasParts {
			return fmt.Errorf("archive.file or archive.parts is required")
		}

		if hasFile && hasParts {
			return fmt.Errorf("archive.file and archive.parts are mutually exclusive")
		}
	}

	if s.EESTFixtures != nil {
		if err := s.EESTFixtures.validate(); err != nil {
			return err
		}
	}

	return nil
}

// validClients is the list of supported client types.
var validClients = map[string]struct{}{
	"geth":       {},
	"nethermind": {},
	"besu":       {},
	"erigon":     {},
	"nimbus":     {},
	"reth":       {},
	"ethrex":     {},
}

// validDropMemoryCachesValues contains valid values for drop_memory_caches.
var validDropMemoryCachesValues = map[string]bool{
	"":         true, // Unset (inherits or disabled)
	"disabled": true, // Explicitly disabled (default)
	"tests":    true, // Between tests
	"steps":    true, // Between all steps
}

// isValidClient checks if the given client type is supported.
func isValidClient(client string) bool {
	_, ok := validClients[client]

	return ok
}

// GetGenesisURL returns the genesis URL for a client instance.
func (c *Config) GetGenesisURL(instance *ClientInstance) string {
	if instance.Genesis != "" {
		return instance.Genesis
	}

	return c.Runner.Client.Config.Genesis[instance.Client]
}

// GetDropMemoryCaches returns the drop_memory_caches setting for an instance.
// Instance-level setting takes precedence over global default.
// Returns empty string if neither is set (disabled).
func (c *Config) GetDropMemoryCaches(instance *ClientInstance) string {
	if instance.DropMemoryCaches != "" {
		return instance.DropMemoryCaches
	}

	return c.Runner.Client.Config.DropMemoryCaches
}

// validRollbackStrategies contains valid values for rollback_strategy.
var validRollbackStrategies = map[string]bool{
	"":                                true, // Unset (defaults to "none")
	RollbackStrategyNone:              true, // Explicitly disabled
	RollbackStrategyRPCDebugSetHead:   true, // Rollback via debug_setHead RPC
	RollbackStrategyContainerRecreate: true, // Recreate container between tests
	RollbackStrategyCheckpointRestore: true, // Podman checkpoint/restore + ZFS
}

// validContainerRuntimes contains valid values for container_runtime.
var validContainerRuntimes = map[string]bool{
	"":       true, // Unset (defaults to "docker")
	"docker": true,
	"podman": true,
}

// resolveDataDir returns the effective datadir config for an instance.
// Instance-level datadir takes precedence over global datadirs.
func (c *Config) resolveDataDir(instance *ClientInstance) *DataDirConfig {
	if instance.DataDir != nil {
		return instance.DataDir
	}

	if c.Runner.Client.DataDirs != nil {
		return c.Runner.Client.DataDirs[instance.Client]
	}

	return nil
}

// GetContainerRuntime returns the container runtime to use.
// Returns "docker" if unset or empty.
func (c *Config) GetContainerRuntime() string {
	if c.Runner.ContainerRuntime != "" {
		return c.Runner.ContainerRuntime
	}

	return "docker"
}

// GetRollbackStrategy returns the rollback_strategy setting for an instance.
// Instance-level setting takes precedence over global default.
// Returns "rpc-debug-setHead" if neither is set.
func (c *Config) GetRollbackStrategy(instance *ClientInstance) string {
	if instance.RollbackStrategy != "" {
		return instance.RollbackStrategy
	}

	if c.Runner.Client.Config.RollbackStrategy != "" {
		return c.Runner.Client.Config.RollbackStrategy
	}

	return RollbackStrategyRPCDebugSetHead
}

// GetDropCachesPath returns the path to the drop_caches file.
// Returns the configured path or the default (/proc/sys/vm/drop_caches).
func (c *Config) GetDropCachesPath() string {
	if c.Runner.DropCachesPath != "" {
		return c.Runner.DropCachesPath
	}

	return DefaultDropCachesPath
}

// GetCPUSysfsPath returns the sysfs base path for CPU frequency control.
// Returns the configured path or the default (/sys/devices/system/cpu).
func (c *Config) GetCPUSysfsPath() string {
	if c.Runner.CPUSysfsPath != "" {
		return c.Runner.CPUSysfsPath
	}

	return DefaultCPUSysfsPath
}

// GetResourceLimits returns the resource limits for an instance.
// Instance-level limits take precedence over global defaults.
// Returns nil if no limits are configured.
func (c *Config) GetResourceLimits(instance *ClientInstance) *ResourceLimits {
	if instance.ResourceLimits != nil {
		return instance.ResourceLimits
	}

	return c.Runner.Client.Config.ResourceLimits
}

// GetRetryNewPayloadsSyncingState returns the retry config for an instance.
// Instance-level config takes precedence over global defaults.
// Returns nil if no config is set.
func (c *Config) GetRetryNewPayloadsSyncingState(instance *ClientInstance) *RetryNewPayloadsSyncingConfig {
	if instance.RetryNewPayloadsSyncingState != nil {
		return instance.RetryNewPayloadsSyncingState
	}

	return c.Runner.Client.Config.RetryNewPayloadsSyncingState
}

// GetRetryNewPayloadsFailedState returns the failed-state retry config for an
// instance. Instance-level config takes precedence over global defaults.
// Returns nil if no config is set.
func (c *Config) GetRetryNewPayloadsFailedState(instance *ClientInstance) *RetryNewPayloadsFailedConfig {
	if instance.RetryNewPayloadsFailedState != nil {
		return instance.RetryNewPayloadsFailedState
	}

	return c.Runner.Client.Config.RetryNewPayloadsFailedState
}

// GetWaitAfterRPCReady returns the duration to wait after RPC becomes ready.
// This gives clients time to complete internal initialization (e.g., Erigon's staged sync)
// before test execution begins.
// Instance-level config takes precedence over global defaults. Returns 0 if not set.
func (c *Config) GetWaitAfterRPCReady(instance *ClientInstance) time.Duration {
	var waitStr string

	if instance.WaitAfterRPCReady != "" {
		waitStr = instance.WaitAfterRPCReady
	} else {
		waitStr = c.Runner.Client.Config.WaitAfterRPCReady
	}

	if waitStr == "" {
		return 0
	}

	d, err := time.ParseDuration(waitStr)
	if err != nil {
		return 0
	}

	return d
}

// GetPostTestSleepDuration returns the duration to sleep after each test.
// Instance-level value overrides the global default. Returns 0 if not set.
func (c *Config) GetPostTestSleepDuration(instance *ClientInstance) time.Duration {
	var sleepStr string

	if instance.PostTestSleepDuration != "" {
		sleepStr = instance.PostTestSleepDuration
	} else {
		sleepStr = c.Runner.Client.Config.PostTestSleepDuration
	}

	if sleepStr == "" {
		return 0
	}

	d, err := time.ParseDuration(sleepStr)
	if err != nil {
		return 0
	}

	return d
}

// GetRunnerRunTimeout returns the global runner-level timeout that caps
// the entire run (all instances, setup, and teardown). Returns 0 if not set.
func (c *Config) GetRunnerRunTimeout() time.Duration {
	if c.Runner.RunTimeout == "" {
		return 0
	}

	d, err := time.ParseDuration(c.Runner.RunTimeout)
	if err != nil {
		return 0
	}

	return d
}

// GetBuilderRunTimeout returns the global builder-level timeout that caps the
// entire build (all builders and targets). Returns 0 if not set.
func (c *Config) GetBuilderRunTimeout() time.Duration {
	if c.Builder == nil || c.Builder.RunTimeout == "" {
		return 0
	}

	d, err := time.ParseDuration(c.Builder.RunTimeout)
	if err != nil {
		return 0
	}

	return d
}

// GetRunTimeout returns the maximum duration for test execution.
// Instance-level config takes precedence over global defaults. Returns 0 if not set.
func (c *Config) GetRunTimeout(instance *ClientInstance) time.Duration {
	var s string

	if instance.RunTimeout != "" {
		s = instance.RunTimeout
	} else {
		s = c.Runner.Client.Config.RunTimeout
	}

	if s == "" {
		return 0
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}

	return d
}

// GetPostTestRPCCalls returns the post-test RPC calls for an instance.
// Instance-level config completely replaces the global default.
// Returns nil if not configured at either level.
func (c *Config) GetPostTestRPCCalls(instance *ClientInstance) []PostTestRPCCall {
	if len(instance.PostTestRPCCalls) > 0 {
		return instance.PostTestRPCCalls
	}

	return c.Runner.Client.Config.PostTestRPCCalls
}

// GetBootstrapFCU returns the bootstrap FCU config for an instance.
// Instance-level config takes precedence over global default.
// Returns nil if not configured at either level.
func (c *Config) GetBootstrapFCU(instance *ClientInstance) *BootstrapFCUConfig {
	if instance.BootstrapFCU != nil {
		return instance.BootstrapFCU
	}

	return c.Runner.Client.Config.BootstrapFCU
}

// GetOpcodeExtraction returns the opcode-extraction config for an instance.
// Instance-level config (when non-nil) fully replaces the global default.
// Returns nil if not configured at either level.
func (c *Config) GetOpcodeExtraction(instance *ClientInstance) *OpcodeExtractionConfig {
	if instance.OpcodeExtraction != nil {
		return instance.OpcodeExtraction
	}

	return c.Runner.Client.Config.OpcodeExtraction
}

// GetCheckpointRestoreStrategyOptions returns the checkpoint-restore strategy
// options for an instance. Instance-level config (when non-nil) fully replaces
// the global default. Returns nil if not configured at either level.
func (c *Config) GetCheckpointRestoreStrategyOptions(
	instance *ClientInstance,
) *CheckpointRestoreStrategyOptions {
	if instance.CheckpointRestoreStrategyOptions != nil {
		return instance.CheckpointRestoreStrategyOptions
	}

	return c.Runner.Client.Config.CheckpointRestoreStrategyOptions
}

// GetCheckpointTmpfsThreshold returns the tmpfs_threshold for an instance.
// Instance-level setting takes precedence over global default.
// Returns empty string if not configured (feature disabled).
func (c *Config) GetCheckpointTmpfsThreshold(instance *ClientInstance) string {
	opts := c.GetCheckpointRestoreStrategyOptions(instance)
	if opts == nil {
		return ""
	}

	return opts.TmpfsThreshold
}

// GetCheckpointTmpfsMaxSize returns the explicit tmpfs mount size cap for an
// instance. Returns 0 when not configured (caller should fall back to a
// default such as 2x the tmpfs threshold).
func (c *Config) GetCheckpointTmpfsMaxSize(instance *ClientInstance) uint64 {
	opts := c.GetCheckpointRestoreStrategyOptions(instance)
	if opts == nil || opts.TmpfsMaxSize == "" {
		return 0
	}

	size, err := ParseByteSize(opts.TmpfsMaxSize)
	if err != nil {
		return 0
	}

	return size
}

// GetCheckpointWaitAfterTCPDropConns returns the duration to wait after
// dropping TCP connections before checkpointing. Instance-level setting
// takes precedence over global default. Returns 10s if not configured.
func (c *Config) GetCheckpointWaitAfterTCPDropConns(
	instance *ClientInstance,
) time.Duration {
	const defaultWait = 10 * time.Second

	opts := c.GetCheckpointRestoreStrategyOptions(instance)
	if opts == nil || opts.WaitAfterTCPDropConns == "" {
		return defaultWait
	}

	d, err := time.ParseDuration(opts.WaitAfterTCPDropConns)
	if err != nil {
		return defaultWait
	}

	return d
}

// GetCheckpointRestartContainer returns whether the container should be
// restarted before taking a CRIU checkpoint. Restarting ensures a clean
// process state (cold caches, clean DB shutdown) for a reliable checkpoint.
// Instance-level setting takes precedence over global default.
func (c *Config) GetCheckpointRestartContainer(instance *ClientInstance) bool {
	opts := c.GetCheckpointRestoreStrategyOptions(instance)
	if opts == nil {
		return false
	}

	return opts.RestartContainer
}

// GetMetadataLabels returns the merged metadata labels for an instance.
// Client-level metadata labels serve as defaults; instance-level labels
// override specific keys.
func (c *Config) GetMetadataLabels(instance *ClientInstance) map[string]string {
	defaults := c.Runner.Client.Config.Metadata.Labels
	overrides := instance.Metadata.Labels

	if len(defaults) == 0 && len(overrides) == 0 {
		return nil
	}

	merged := make(map[string]string, len(defaults)+len(overrides))
	for k, v := range defaults {
		merged[k] = v
	}

	for k, v := range overrides {
		merged[k] = v
	}

	return merged
}

// ParseByteSize parses a human-readable byte size string into bytes.
// Uses the same format as resource_limits.memory (Docker go-units):
// e.g. "32g", "512m", "1024k", "1073741824".
func ParseByteSize(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}

	n, err := units.RAMInBytes(s)
	if err != nil {
		return 0, fmt.Errorf("invalid byte size %q: %w", s, err)
	}

	if n < 0 {
		return 0, fmt.Errorf("invalid byte size %q: negative value", s)
	}

	return uint64(n), nil
}

// validateDropMemoryCaches validates drop_memory_caches settings and checks permissions.
func (c *Config) validateDropMemoryCaches() error {
	// Check all instances for valid values and if feature is enabled.
	enabled := false

	for _, instance := range c.Runner.Instances {
		value := c.GetDropMemoryCaches(&instance)

		if !validDropMemoryCachesValues[value] {
			return fmt.Errorf("instance %q: invalid drop_memory_caches value %q (must be \"disabled\", \"tests\", or \"steps\")",
				instance.ID, value)
		}

		if value != "" && value != "disabled" {
			enabled = true
		}
	}

	if !enabled {
		return nil
	}

	dropCachesPath := c.GetDropCachesPath()

	// Check OS - drop_memory_caches is Linux-only (skip if custom path is configured).
	if c.Runner.DropCachesPath == "" && runtime.GOOS != "linux" {
		return fmt.Errorf("drop_memory_caches is only supported on Linux (current OS: %s)", runtime.GOOS)
	}

	// Verify write access to drop_caches file.
	file, err := os.OpenFile(dropCachesPath, os.O_WRONLY, 0)
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("drop_memory_caches is enabled but no write permission to %s (requires root)", dropCachesPath)
		}

		return fmt.Errorf("drop_memory_caches: cannot access %s: %w", dropCachesPath, err)
	}

	_ = file.Close()

	return nil
}

// validateRollbackStrategy validates rollback_strategy settings for active instances.
func (c *Config) validateRollbackStrategy(opt ValidateOpts) error {
	for _, instance := range c.Runner.Instances {
		if !opt.isInstanceActive(instance.ID) {
			continue
		}

		value := c.GetRollbackStrategy(&instance)

		if !validRollbackStrategies[value] {
			return fmt.Errorf(
				"instance %q: invalid rollback_strategy value %q"+
					" (must be %q, %q, %q, or %q)",
				instance.ID, value,
				RollbackStrategyNone,
				RollbackStrategyRPCDebugSetHead,
				RollbackStrategyContainerRecreate,
				RollbackStrategyCheckpointRestore,
			)
		}

		// checkpoint-restore requires podman runtime.
		if value == RollbackStrategyCheckpointRestore {
			if c.GetContainerRuntime() != "podman" {
				return fmt.Errorf(
					"instance %q: rollback_strategy %q requires"+
						" container_runtime: \"podman\"",
					instance.ID, value,
				)
			}

			// checkpoint-restore with a configured datadir requires ZFS
			// (ZFS snapshots for rollback). Without a datadir, copy-based
			// rollback is used instead, so no restriction applies.
			dd := c.resolveDataDir(&instance)
			if dd != nil && dd.Method != "zfs" {
				return fmt.Errorf(
					"instance %q: rollback_strategy %q with datadir"+
						" requires datadir.method: \"zfs\"",
					instance.ID, value,
				)
			}
		}

		// Validate checkpoint_restore_strategy_options.tmpfs_threshold if set.
		threshold := c.GetCheckpointTmpfsThreshold(&instance)
		if threshold != "" {
			if _, err := ParseByteSize(threshold); err != nil {
				return fmt.Errorf(
					"instance %q: invalid checkpoint_restore_strategy_options.tmpfs_threshold %q: %w",
					instance.ID, threshold, err,
				)
			}
		}

		// Validate checkpoint_restore_strategy_options.tmpfs_max_size if set.
		opts := c.GetCheckpointRestoreStrategyOptions(&instance)
		if opts != nil && opts.TmpfsMaxSize != "" {
			if _, err := ParseByteSize(opts.TmpfsMaxSize); err != nil {
				return fmt.Errorf(
					"instance %q: invalid checkpoint_restore_strategy_options.tmpfs_max_size %q: %w",
					instance.ID, opts.TmpfsMaxSize, err,
				)
			}
		}
	}

	return nil
}

// validateDataDirMethods runs per-method preflight for active instances.
// For method=schelk: verifies the binary is on PATH, ensures the schelk
// scratch volume is mounted (calling `schelk mount` if not), and then
// verifies each instance's source_dir exists under the schelk mount
// point. This shifts the existence check out of DataDirConfig.Validate
// for schelk since the path only materialises after mount.
func (c *Config) validateDataDirMethods(opt ValidateOpts) error {
	type schelkInstance struct {
		id, sourceDir string
	}

	var schelkInstances []schelkInstance

	for _, instance := range c.Runner.Instances {
		if !opt.isInstanceActive(instance.ID) {
			continue
		}

		dd := c.resolveDataDir(&instance)
		if dd != nil && dd.Method == "schelk" {
			schelkInstances = append(schelkInstances, schelkInstance{
				id:        instance.ID,
				sourceDir: dd.SourceDir,
			})
		}
	}

	if len(schelkInstances) == 0 {
		return nil
	}

	bin := datadir.SchelkBinary()

	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf(
			"datadir.method \"schelk\" requires the `%s` binary on PATH "+
				"(override with %s): %w",
			bin, datadir.SchelkBinaryEnv, err,
		)
	}

	state, err := datadir.ReadSchelkState(datadir.SchelkStatePath())
	if err != nil {
		return fmt.Errorf("datadir.method \"schelk\": %w", err)
	}

	if state.MountPoint == "" {
		return fmt.Errorf("datadir.method \"schelk\": schelk state has no mount_point — run `schelk init-new` or `schelk init-from` first")
	}

	// Ensure the scratch is mounted (shared with the state-actor builder's
	// preflight). Errors on the crash-inconsistent state (pointing the user at
	// `schelk full-recover`) or a failed `schelk mount`.
	if err := datadir.EnsureSchelkMounted(context.Background(), nil); err != nil {
		return fmt.Errorf("datadir.method %q: %w", "schelk", err)
	}

	for _, si := range schelkInstances {
		info, statErr := os.Stat(si.sourceDir)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return fmt.Errorf(
					"instance %q datadir: source_dir %q does not exist under schelk mount %q",
					si.id, si.sourceDir, state.MountPoint,
				)
			}

			return fmt.Errorf("instance %q datadir: checking source_dir: %w", si.id, statErr)
		}

		if !info.IsDir() {
			return fmt.Errorf("instance %q datadir: source_dir %q is not a directory", si.id, si.sourceDir)
		}
	}

	return nil
}

// validateContainerRuntime validates the container_runtime field.
func (c *Config) validateContainerRuntime() error {
	if !validContainerRuntimes[c.Runner.ContainerRuntime] {
		return fmt.Errorf(
			"invalid container_runtime %q (must be \"docker\" or \"podman\")",
			c.Runner.ContainerRuntime,
		)
	}

	return nil
}

// validateCPUFreq validates cpu_freq settings and checks system capabilities.
func (c *Config) validateCPUFreq() error {
	// Check all instances for CPU frequency settings.
	enabled := false

	for _, instance := range c.Runner.Instances {
		limits := c.GetResourceLimits(&instance)
		if limits == nil {
			continue
		}

		if limits.CPUFreq != "" || limits.CPUTurboBoost != nil || limits.CPUGovernor != "" {
			enabled = true

			break
		}
	}

	if !enabled {
		return nil
	}

	// Check OS - CPU frequency control is Linux-only.
	if runtime.GOOS != "linux" {
		return fmt.Errorf("cpu_freq is only supported on Linux (current OS: %s)", runtime.GOOS)
	}

	sysfsPath := c.GetCPUSysfsPath()

	// Check if cpufreq subsystem is available.
	if !cpufreq.IsCPUFreqSupported(sysfsPath) {
		return fmt.Errorf("cpu_freq: cpufreq subsystem not available (no scaling_governor in sysfs)")
	}

	// Check write access.
	if err := cpufreq.HasWriteAccess(sysfsPath); err != nil {
		return fmt.Errorf("cpu_freq: %w", err)
	}

	// Validate each instance's settings.
	for _, instance := range c.Runner.Instances {
		limits := c.GetResourceLimits(&instance)
		if limits == nil {
			continue
		}

		// Validate frequency format and bounds.
		if limits.CPUFreq != "" && strings.ToUpper(limits.CPUFreq) != "MAX" {
			freqKHz, err := cpufreq.ParseFrequency(limits.CPUFreq)
			if err != nil {
				return fmt.Errorf("instance %q: invalid cpu_freq %q: %w", instance.ID, limits.CPUFreq, err)
			}

			if err := cpufreq.ValidateFrequency(sysfsPath, freqKHz); err != nil {
				return fmt.Errorf("instance %q: %w", instance.ID, err)
			}
		}

		// Validate governor.
		if limits.CPUGovernor != "" {
			if err := cpufreq.ValidateGovernor(sysfsPath, limits.CPUGovernor); err != nil {
				return fmt.Errorf("instance %q: %w", instance.ID, err)
			}
		}
	}

	return nil
}

// validateRetryNewPayloadsSyncingState validates retry_new_payloads_syncing_state settings.
func (c *Config) validateRetryNewPayloadsSyncingState() error {
	for _, instance := range c.Runner.Instances {
		cfg := c.GetRetryNewPayloadsSyncingState(&instance)
		if cfg == nil || !cfg.Enabled {
			continue
		}

		if cfg.MaxRetries < 1 {
			return fmt.Errorf("instance %q: retry_new_payloads_syncing_state.max_retries must be at least 1",
				instance.ID)
		}

		if cfg.Backoff == "" {
			return fmt.Errorf("instance %q: retry_new_payloads_syncing_state.backoff is required when enabled",
				instance.ID)
		}

		if _, err := time.ParseDuration(cfg.Backoff); err != nil {
			return fmt.Errorf("instance %q: invalid retry_new_payloads_syncing_state.backoff %q: %w",
				instance.ID, cfg.Backoff, err)
		}
	}

	return nil
}

// validateRetryNewPayloadsFailedState validates retry_new_payloads_failed_state settings.
func (c *Config) validateRetryNewPayloadsFailedState() error {
	for _, instance := range c.Runner.Instances {
		cfg := c.GetRetryNewPayloadsFailedState(&instance)
		if cfg == nil || !cfg.Enabled {
			continue
		}

		if cfg.MaxRetries < 1 {
			return fmt.Errorf("instance %q: retry_new_payloads_failed_state.max_retries must be at least 1",
				instance.ID)
		}

		if cfg.Backoff == "" {
			return fmt.Errorf("instance %q: retry_new_payloads_failed_state.backoff is required when enabled",
				instance.ID)
		}

		if _, err := time.ParseDuration(cfg.Backoff); err != nil {
			return fmt.Errorf("instance %q: invalid retry_new_payloads_failed_state.backoff %q: %w",
				instance.ID, cfg.Backoff, err)
		}
	}

	return nil
}

// validateWaitAfterRPCReady validates wait_after_rpc_ready settings.
func (c *Config) validateWaitAfterRPCReady() error {
	for _, instance := range c.Runner.Instances {
		waitStr := instance.WaitAfterRPCReady
		if waitStr == "" {
			waitStr = c.Runner.Client.Config.WaitAfterRPCReady
		}

		if waitStr != "" {
			if _, err := time.ParseDuration(waitStr); err != nil {
				return fmt.Errorf("instance %q: invalid wait_after_rpc_ready %q: %w",
					instance.ID, waitStr, err)
			}
		}
	}

	return nil
}

// validatePostTestSleepDuration validates post_test_sleep_duration settings.
func (c *Config) validatePostTestSleepDuration() error {
	for _, instance := range c.Runner.Instances {
		sleepStr := instance.PostTestSleepDuration
		if sleepStr == "" {
			sleepStr = c.Runner.Client.Config.PostTestSleepDuration
		}

		if sleepStr != "" {
			if _, err := time.ParseDuration(sleepStr); err != nil {
				return fmt.Errorf("instance %q: invalid post_test_sleep_duration %q: %w",
					instance.ID, sleepStr, err)
			}
		}
	}

	return nil
}

// validateRunTimeout validates run_timeout settings.
func (c *Config) validateRunTimeout() error {
	if c.Runner.RunTimeout != "" {
		if _, err := time.ParseDuration(c.Runner.RunTimeout); err != nil {
			return fmt.Errorf("invalid runner.run_timeout %q: %w",
				c.Runner.RunTimeout, err)
		}
	}

	for _, instance := range c.Runner.Instances {
		s := instance.RunTimeout
		if s == "" {
			s = c.Runner.Client.Config.RunTimeout
		}

		if s != "" {
			if _, err := time.ParseDuration(s); err != nil {
				return fmt.Errorf("instance %q: invalid run_timeout %q: %w",
					instance.ID, s, err)
			}
		}
	}

	return nil
}

// validatePostTestRPCCalls validates post_test_rpc_calls settings.
func (c *Config) validatePostTestRPCCalls() error {
	// Validate global-level calls.
	for i, call := range c.Runner.Client.Config.PostTestRPCCalls {
		if err := validatePostTestRPCCall(call, fmt.Sprintf("client.config.post_test_rpc_calls[%d]", i)); err != nil {
			return err
		}
	}

	// Validate instance-level calls.
	for _, instance := range c.Runner.Instances {
		for i, call := range instance.PostTestRPCCalls {
			prefix := fmt.Sprintf("instance %q post_test_rpc_calls[%d]", instance.ID, i)
			if err := validatePostTestRPCCall(call, prefix); err != nil {
				return err
			}
		}
	}

	return nil
}

// validatePostTestRPCCall validates a single post-test RPC call configuration.
func validatePostTestRPCCall(call PostTestRPCCall, prefix string) error {
	if call.Method == "" {
		return fmt.Errorf("%s: method is required", prefix)
	}

	if call.Timeout != "" {
		d, err := time.ParseDuration(call.Timeout)
		if err != nil {
			return fmt.Errorf("%s: invalid timeout %q: %w", prefix, call.Timeout, err)
		}

		if d <= 0 {
			return fmt.Errorf("%s: timeout must be positive, got %q", prefix, call.Timeout)
		}
	}

	if call.Dump.Enabled && call.Dump.Filename == "" {
		return fmt.Errorf("%s: dump.filename is required when dump is enabled", prefix)
	}

	return nil
}

// validateBootstrapFCU validates bootstrap_fcu settings.
func (c *Config) validateBootstrapFCU() error {
	for _, instance := range c.Runner.Instances {
		cfg := c.GetBootstrapFCU(&instance)
		if cfg == nil || !cfg.Enabled {
			continue
		}

		if cfg.MaxRetries < 1 {
			return fmt.Errorf("instance %q: bootstrap_fcu.max_retries must be at least 1",
				instance.ID)
		}

		if cfg.Backoff == "" {
			return fmt.Errorf("instance %q: bootstrap_fcu.backoff is required when enabled",
				instance.ID)
		}

		if _, err := time.ParseDuration(cfg.Backoff); err != nil {
			return fmt.Errorf("instance %q: invalid bootstrap_fcu.backoff %q: %w",
				instance.ID, cfg.Backoff, err)
		}

		if cfg.HeadBlockHash != "" {
			if !strings.HasPrefix(cfg.HeadBlockHash, "0x") || len(cfg.HeadBlockHash) != 66 {
				return fmt.Errorf(
					"instance %q: bootstrap_fcu.head_block_hash must be a 0x-prefixed"+
						" 32-byte hex string, got %q", instance.ID, cfg.HeadBlockHash,
				)
			}
		}
	}

	return nil
}

// validateOpcodeExtraction validates opcode_extraction settings.
// Timeout (when set) must parse as a positive Go duration. An empty
// timeout falls back to DefaultOpcodeExtractionTimeout.
func (c *Config) validateOpcodeExtraction() error {
	for _, instance := range c.Runner.Instances {
		cfg := c.GetOpcodeExtraction(&instance)
		if cfg == nil || !cfg.Enabled {
			continue
		}

		if cfg.Timeout == "" {
			continue
		}

		d, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return fmt.Errorf(
				"instance %q: invalid opcode_extraction.timeout %q: %w",
				instance.ID, cfg.Timeout, err,
			)
		}

		if d <= 0 {
			return fmt.Errorf(
				"instance %q: opcode_extraction.timeout must be positive, got %q",
				instance.ID, cfg.Timeout,
			)
		}
	}

	return nil
}

// validateResultsUpload validates results_upload settings.
func (c *Config) validateResultsUpload() error {
	if c.Runner.Benchmark.ResultsUpload == nil || c.Runner.Benchmark.ResultsUpload.S3 == nil {
		return nil
	}

	s3Cfg := c.Runner.Benchmark.ResultsUpload.S3
	if !s3Cfg.Enabled {
		return nil
	}

	if s3Cfg.Bucket == "" {
		return fmt.Errorf("results_upload.s3: bucket is required when enabled")
	}

	if s3Cfg.EndpointURL != "" {
		u, err := url.Parse(s3Cfg.EndpointURL)
		if err != nil {
			return fmt.Errorf("results_upload.s3: invalid endpoint_url: %w", err)
		}

		if u.Path != "" && u.Path != "/" {
			return fmt.Errorf(
				"results_upload.s3: endpoint_url should not contain a path (%q); "+
					"set only the scheme and host (e.g. %q), the bucket name is configured separately",
				u.Path, u.Scheme+"://"+u.Host,
			)
		}
	}

	return nil
}

// validRoles contains the valid user role values.
var validRoles = map[string]bool{
	"admin":    true,
	"readonly": true,
}

// ValidateAPI validates the API configuration if present.
func (c *Config) ValidateAPI() error {
	if c.API == nil {
		return nil
	}

	// Validate database driver.
	switch c.API.Database.Driver {
	case "sqlite", "postgres":
	default:
		return fmt.Errorf(
			"api.database.driver: invalid value %q (must be \"sqlite\" or \"postgres\")",
			c.API.Database.Driver,
		)
	}

	// Validate postgres required fields.
	if c.API.Database.Driver == "postgres" {
		pg := c.API.Database.Postgres
		if pg.Host == "" {
			return fmt.Errorf("api.database.postgres.host is required")
		}

		if pg.User == "" {
			return fmt.Errorf("api.database.postgres.user is required")
		}

		if pg.Database == "" {
			return fmt.Errorf("api.database.postgres.database is required")
		}
	}

	// Validate session TTL is parseable.
	if _, err := time.ParseDuration(c.API.Auth.SessionTTL); err != nil {
		return fmt.Errorf(
			"api.auth.session_ttl: invalid duration %q: %w",
			c.API.Auth.SessionTTL, err,
		)
	}

	// At least one auth provider must be enabled.
	if !c.API.Auth.Basic.Enabled && !c.API.Auth.GitHub.Enabled {
		return fmt.Errorf("api.auth: at least one auth provider must be enabled")
	}

	// Validate basic auth users.
	if c.API.Auth.Basic.Enabled {
		if len(c.API.Auth.Basic.Users) == 0 {
			return fmt.Errorf(
				"api.auth.basic: at least one user is required when enabled",
			)
		}

		seen := make(map[string]struct{}, len(c.API.Auth.Basic.Users))

		for i, u := range c.API.Auth.Basic.Users {
			if u.Username == "" {
				return fmt.Errorf(
					"api.auth.basic.users[%d]: username is required", i,
				)
			}

			if u.Password == "" {
				return fmt.Errorf(
					"api.auth.basic.users[%d]: password is required", i,
				)
			}

			if !validRoles[u.Role] {
				return fmt.Errorf(
					"api.auth.basic.users[%d]: invalid role %q "+
						"(must be \"admin\" or \"readonly\")",
					i, u.Role,
				)
			}

			if _, exists := seen[u.Username]; exists {
				return fmt.Errorf(
					"api.auth.basic.users[%d]: duplicate username %q",
					i, u.Username,
				)
			}

			seen[u.Username] = struct{}{}
		}
	}

	// Validate GitHub auth required fields.
	if c.API.Auth.GitHub.Enabled {
		if c.API.Auth.GitHub.ClientID == "" {
			return fmt.Errorf("api.auth.github.client_id is required when enabled")
		}

		if c.API.Auth.GitHub.ClientSecret == "" {
			return fmt.Errorf(
				"api.auth.github.client_secret is required when enabled",
			)
		}

		if c.API.Auth.GitHub.RedirectURL == "" {
			return fmt.Errorf(
				"api.auth.github.redirect_url is required when enabled",
			)
		}

		// Validate role values in mappings.
		for org, role := range c.API.Auth.GitHub.OrgRoleMapping {
			if !validRoles[role] {
				return fmt.Errorf(
					"api.auth.github.org_role_mapping[%q]: invalid role %q",
					org, role,
				)
			}
		}

		for user, role := range c.API.Auth.GitHub.UserRoleMapping {
			if !validRoles[role] {
				return fmt.Errorf(
					"api.auth.github.user_role_mapping[%q]: invalid role %q",
					user, role,
				)
			}
		}
	}

	// Validate storage settings.
	if err := c.validateAPIStorage(); err != nil {
		return err
	}

	// Validate indexing settings.
	if err := c.validateAPIIndexing(); err != nil {
		return err
	}

	// Validate ingest settings.
	if err := c.validateAPIIngest(); err != nil {
		return err
	}

	return nil
}

// validateAPIIngest validates the ingest configuration.
func (c *Config) validateAPIIngest() error {
	if c.API.Ingest == nil {
		return nil
	}

	if c.API.Ingest.Token == "" {
		return fmt.Errorf("api.ingest.token is required when ingest is configured")
	}

	if c.API.Ingest.StaleThreshold != "" {
		if _, err := time.ParseDuration(c.API.Ingest.StaleThreshold); err != nil {
			return fmt.Errorf(
				"api.ingest.stale_threshold: invalid duration %q: %w",
				c.API.Ingest.StaleThreshold, err,
			)
		}
	}

	return nil
}

// validateAPIStorage validates the API storage configuration.
func (c *Config) validateAPIStorage() error {
	s3Enabled := c.API.Storage.S3 != nil && c.API.Storage.S3.Enabled
	localEnabled := c.API.Storage.Local != nil && c.API.Storage.Local.Enabled

	if s3Enabled && localEnabled {
		return fmt.Errorf(
			"api.storage: only one backend (s3 or local) may be enabled at a time",
		)
	}

	if s3Enabled {
		if err := c.validateAPIS3Storage(); err != nil {
			return err
		}
	}

	if localEnabled {
		if err := c.validateAPILocalStorage(); err != nil {
			return err
		}
	}

	return nil
}

// validateAPIS3Storage validates S3 storage settings.
func (c *Config) validateAPIS3Storage() error {
	s3Cfg := c.API.Storage.S3

	if s3Cfg.Bucket == "" {
		return fmt.Errorf("api.storage.s3: bucket is required when enabled")
	}

	if len(s3Cfg.DiscoveryPaths) == 0 {
		return fmt.Errorf(
			"api.storage.s3: at least one discovery_path is required when enabled",
		)
	}

	for i, p := range s3Cfg.DiscoveryPaths {
		if p == "" {
			return fmt.Errorf(
				"api.storage.s3.discovery_paths[%d]: path must not be empty", i,
			)
		}

		if strings.Contains(p, "..") {
			return fmt.Errorf(
				"api.storage.s3.discovery_paths[%d]: path must not contain \"..\"", i,
			)
		}
	}

	if _, err := time.ParseDuration(s3Cfg.PresignedURLs.Expiry); err != nil {
		return fmt.Errorf(
			"api.storage.s3.presigned_urls.expiry: invalid duration %q: %w",
			s3Cfg.PresignedURLs.Expiry, err,
		)
	}

	return nil
}

// validateAPILocalStorage validates local filesystem storage settings.
func (c *Config) validateAPILocalStorage() error {
	localCfg := c.API.Storage.Local

	if len(localCfg.DiscoveryPaths) == 0 {
		return fmt.Errorf(
			"api.storage.local: at least one discovery_path is required when enabled",
		)
	}

	for name, dir := range localCfg.DiscoveryPaths {
		// Validate the map key (URL prefix).
		if name == "" {
			return fmt.Errorf(
				"api.storage.local.discovery_paths: key must not be empty",
			)
		}

		if strings.Contains(name, "..") {
			return fmt.Errorf(
				"api.storage.local.discovery_paths[%s]: "+
					"key must not contain \"..\"", name,
			)
		}

		if strings.Contains(name, "/") {
			return fmt.Errorf(
				"api.storage.local.discovery_paths[%s]: "+
					"key must not contain \"/\"", name,
			)
		}

		// Validate the map value (absolute directory path).
		if dir == "" {
			return fmt.Errorf(
				"api.storage.local.discovery_paths[%s]: "+
					"path must not be empty", name,
			)
		}

		if !filepath.IsAbs(dir) {
			return fmt.Errorf(
				"api.storage.local.discovery_paths[%s]: "+
					"path must be absolute, got %q", name, dir,
			)
		}

		if strings.Contains(dir, "..") {
			return fmt.Errorf(
				"api.storage.local.discovery_paths[%s]: "+
					"path must not contain \"..\"", name,
			)
		}
	}

	return nil
}

// validateAPIIndexing validates the indexing service configuration.
func (c *Config) validateAPIIndexing() error {
	idx := c.API.Indexing
	if idx == nil || !idx.Enabled {
		return nil
	}

	// At least one storage backend must be configured for indexing.
	s3Enabled := c.API.Storage.S3 != nil && c.API.Storage.S3.Enabled
	localEnabled := c.API.Storage.Local != nil && c.API.Storage.Local.Enabled

	if !s3Enabled && !localEnabled {
		return fmt.Errorf(
			"api.indexing: at least one storage backend " +
				"(s3 or local) must be configured when indexing is enabled",
		)
	}

	// Validate interval.
	interval := idx.Interval
	if interval == "" {
		interval = "10m"
	}

	if _, err := time.ParseDuration(interval); err != nil {
		return fmt.Errorf(
			"api.indexing.interval: invalid duration %q: %w",
			idx.Interval, err,
		)
	}

	// Validate concurrency.
	if idx.Concurrency < 0 {
		return fmt.Errorf(
			"api.indexing.concurrency: must be >= 0 (0 means default)",
		)
	}

	// Validate database driver.
	switch idx.Database.Driver {
	case "sqlite", "postgres":
	default:
		return fmt.Errorf(
			"api.indexing.database.driver: invalid value %q "+
				"(must be \"sqlite\" or \"postgres\")",
			idx.Database.Driver,
		)
	}

	if idx.Database.Driver == "sqlite" && idx.Database.SQLite.Path == "" {
		return fmt.Errorf(
			"api.indexing.database.sqlite.path is required",
		)
	}

	if idx.Database.Driver == "postgres" {
		pg := idx.Database.Postgres
		if pg.Host == "" {
			return fmt.Errorf(
				"api.indexing.database.postgres.host is required",
			)
		}

		if pg.User == "" {
			return fmt.Errorf(
				"api.indexing.database.postgres.user is required",
			)
		}

		if pg.Database == "" {
			return fmt.Errorf(
				"api.indexing.database.postgres.database is required",
			)
		}
	}

	return nil
}

// dumpConfigDecodeHook returns a mapstructure decode hook that converts
// a boolean value to DumpConfig{Enabled: bool}.
// This allows users to write `dump: true` as shorthand for `dump: {enabled: true}`.
func dumpConfigDecodeHook() mapstructure.DecodeHookFuncType {
	return func(from reflect.Type, to reflect.Type, data any) (any, error) {
		if to != reflect.TypeOf(DumpConfig{}) {
			return data, nil
		}

		if from.Kind() == reflect.Bool {
			return DumpConfig{Enabled: data.(bool)}, nil
		}

		return data, nil
	}
}

// bootstrapFCUDecodeHook returns a mapstructure decode hook that converts
// a boolean value to BootstrapFCUConfig.
// This allows users to write `bootstrap_fcu: true` as shorthand for the full struct.
func bootstrapFCUDecodeHook() mapstructure.DecodeHookFuncType {
	return func(from reflect.Type, to reflect.Type, data any) (any, error) {
		if to != reflect.TypeOf(BootstrapFCUConfig{}) {
			return data, nil
		}

		if from.Kind() == reflect.Bool {
			if data.(bool) {
				return BootstrapFCUConfig{
					Enabled:    true,
					MaxRetries: 30,
					Backoff:    "1s",
				}, nil
			}

			return BootstrapFCUConfig{Enabled: false}, nil
		}

		return data, nil
	}
}

// rawRunnerConfig is a minimal struct used to re-parse environment map keys
// with their original casing, since Viper lowercases all map keys internally.
type rawRunnerConfig struct {
	Runner struct {
		Instances []struct {
			ID          string            `yaml:"id"`
			Environment map[string]string `yaml:"environment"`
		} `yaml:"instances"`
	} `yaml:"runner"`
}

// restoreEnvironmentKeyCasing re-parses the raw YAML to recover the original
// casing of environment variable keys that Viper lowercased.
func restoreEnvironmentKeyCasing(cfg *Config, rawYAMLs []string) {
	envByID := make(map[string]map[string]string, len(cfg.Runner.Instances))

	for _, raw := range rawYAMLs {
		var parsed rawRunnerConfig
		if err := yaml.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}

		for _, inst := range parsed.Runner.Instances {
			if inst.Environment != nil {
				envByID[inst.ID] = inst.Environment
			}
		}
	}

	for i := range cfg.Runner.Instances {
		if orig, ok := envByID[cfg.Runner.Instances[i].ID]; ok {
			cfg.Runner.Instances[i].Environment = orig
		}
	}
}

// rawEESTStubs holds just the inline address_stubs map for one level of the
// eest_payloads config (the global config block or a single target).
type rawEESTStubs struct {
	AddressStubs map[string]map[string]string `yaml:"address_stubs"`
}

// rawEESTBuilderConfig is a minimal struct used to re-parse inline
// address_stubs maps, whose stub-name keys Viper lowercases (it is
// case-insensitive). EEST resolves stub names by exact match, so the
// original casing must be restored — both at the global config level
// (hoisted into targets via ResolveTarget) and per target.
type rawEESTBuilderConfig struct {
	Builder struct {
		EESTPayloads struct {
			Config  *rawEESTStubs  `yaml:"config"`
			Targets []rawEESTStubs `yaml:"targets"`
		} `yaml:"eest_payloads"`
	} `yaml:"builder"`
}

// restoreAddressStubsKeyCasing re-parses the raw YAML to recover the original
// casing of inline address_stubs stub-name keys that Viper lowercased. Viper
// replaces (rather than appends) list values on merge, so the last config file
// that defines targets (or the config block) wins — mirror that with a
// last-wins positional match.
func restoreAddressStubsKeyCasing(cfg *Config, rawYAMLs []string) {
	if cfg.Builder == nil || cfg.Builder.EESTPayloads == nil {
		return
	}

	ep := cfg.Builder.EESTPayloads

	// configStubs accumulates the config-block stubs across all files (later
	// files win per key), mirroring how Viper deep-merges the config map — so a
	// config.address_stubs set in an earlier file isn't dropped when a later
	// file only touches some other config field. Targets, by contrast, are
	// replaced wholesale on merge, so the last file's list wins (see below).
	configStubs := make(map[string]map[string]string)

	var rawTargets []rawEESTStubs

	for _, raw := range rawYAMLs {
		var parsed rawEESTBuilderConfig
		if err := yaml.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}

		if c := parsed.Builder.EESTPayloads.Config; c != nil {
			for name, stub := range c.AddressStubs {
				configStubs[name] = stub
			}
		}

		if len(parsed.Builder.EESTPayloads.Targets) > 0 {
			rawTargets = parsed.Builder.EESTPayloads.Targets
		}
	}

	// Global config defaults (hoisted into targets at resolve time).
	if ep.Config != nil && len(configStubs) > 0 {
		ep.Config.AddressStubs = configStubs
	}

	// Per-target stubs. Only restore when the winning file's target list aligns
	// 1:1 with the resolved config; otherwise leave the (lowercased) keys
	// untouched rather than risk mismatching stubs onto the wrong target.
	if len(rawTargets) != len(ep.Targets) {
		return
	}

	for i := range ep.Targets {
		if len(rawTargets[i].AddressStubs) > 0 {
			ep.Targets[i].AddressStubs = rawTargets[i].AddressStubs
		}
	}
}

// normalizeStateActorSpec resolves builder.state_actor.spec from the raw YAML
// into a YAML string body. The field is excluded from Viper decoding so it can
// be authored either as a structured mapping (for editor syntax highlighting)
// or as a "|" block scalar — both normalize to the body state-actor consumes.
// Re-parsing the raw YAML preserves number formatting, value casing and
// comments that a Viper round-trip would lose. Last file with a spec wins (it
// is a scalar override, not a merged map).
func normalizeStateActorSpec(cfg *Config, rawYAMLs []string) {
	if cfg.Builder == nil || cfg.Builder.StateActor == nil {
		return
	}

	for _, raw := range rawYAMLs {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil || len(doc.Content) == 0 {
			continue
		}

		spec := yamlMapValue(yamlMapValue(yamlMapValue(doc.Content[0], "builder"), "state_actor"), "spec")
		if spec == nil {
			continue
		}

		body, err := stateActorSpecBody(spec)
		if err != nil {
			continue
		}

		cfg.Builder.StateActor.Spec = body
	}
}

// stateActorSpecBody serializes a spec node to the YAML body state-actor reads:
// a scalar (a "|" block) yields its string content verbatim; a mapping is
// re-marshaled to YAML.
func stateActorSpecBody(node *yaml.Node) (string, error) {
	if node.Kind == yaml.ScalarNode {
		return node.Value, nil
	}

	out, err := yaml.Marshal(node)
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// yamlMapValue returns the value node for key in a YAML mapping node, or nil
// when the node is not a mapping or the key is absent.
func yamlMapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}

	return nil
}
