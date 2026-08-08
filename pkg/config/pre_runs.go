package config

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultPreRunGasLimit is the gas-bump target when a pre-run target (or
	// builder.pre_runs.config) leaves gas_limit unset: 1 TGas. The pre-run
	// builds empty blocks until the live head's gas limit reaches this, so the
	// benchmark blocks the eest_payloads stage later builds can be arbitrarily
	// large. Matches the filler's miner gas limit.
	DefaultPreRunGasLimit uint64 = 1_000_000_000_000

	// DefaultPreRunGasBumpMaxBlocks caps how many empty gas-bump blocks the
	// pre-run will build before giving up on reaching gas_limit — a safety
	// bound against a filler that never raises its gas limit. Each block can
	// raise the limit by at most 1/1024, so this comfortably covers a ramp from
	// a ~30M-gas snapshot to 1 TGas.
	DefaultPreRunGasBumpMaxBlocks = 20_000

	// DefaultPreRunFundingAmountGwei is the withdrawal amount credited to each
	// funding account when amount_gwei is unset: the max uint64 (2^64-1) gwei,
	// mirroring NethermindEth/gas-benchmarks' funding block.
	DefaultPreRunFundingAmountGwei uint64 = 1<<64 - 1
)

// PreRunsConfig configures builder.pre_runs — an optional stage that runs
// after builder.state_actor and before builder.eest_payloads. Per target it
// boots a filler EL client on a writable copy of a snapshot datadir, ramps the
// block gas limit up via empty blocks, builds a funding block, runs
// fill-stateful (with --no-reset-between-tests, so the deployed state persists)
// on the configured setup tests, then persists the advanced datadir to
// output_dir. A later eest_payloads target uses that datadir as its source_dir,
// so the benchmark fill builds on top of the pre-run's setup state.
//
// The fill toolchain fields mirror EESTPayloadsConfig: the fill image is built
// or pulled the same way, and the execution-specs checkout is selected via
// EESTRepo/EESTRef. Config holds per-target defaults hoisted via ResolveTarget.
type PreRunsConfig struct {
	ContainerRuntime string   `yaml:"container_runtime,omitempty" mapstructure:"container_runtime"`
	FillImage        string   `yaml:"fill_image,omitempty" mapstructure:"fill_image"`
	FillDockerfile   string   `yaml:"fill_dockerfile,omitempty" mapstructure:"fill_dockerfile"`
	PullPolicy       string   `yaml:"pull_policy,omitempty" mapstructure:"pull_policy"`
	JWT              string   `yaml:"jwt,omitempty" mapstructure:"jwt"`
	FillCommand      []string `yaml:"fill_command,omitempty" mapstructure:"fill_command"`
	// EESTRepo / EESTRef select the execution-specs checkout used for filling
	// the setup tests. The setup fixtures for the accumulate-state flow require
	// fill-stateful's --no-reset-between-tests flag, so this typically points at
	// a branch carrying it (e.g. skylenet/execution-specs).
	EESTRepo string          `yaml:"eest_repo,omitempty" mapstructure:"eest_repo"`
	EESTRef  string          `yaml:"eest_ref,omitempty" mapstructure:"eest_ref"`
	Config   *PreRunDefaults `yaml:"config,omitempty" mapstructure:"config"`
	Targets  []PreRunTarget  `yaml:"targets,omitempty" mapstructure:"targets"`
}

// PreRunFundingAccount is one account credited in the pre-run's funding block
// via a beacon withdrawal. AmountGwei defaults to DefaultPreRunFundingAmountGwei
// when nil.
type PreRunFundingAccount struct {
	Address    string  `yaml:"address" mapstructure:"address"`
	AmountGwei *uint64 `yaml:"amount_gwei,omitempty" mapstructure:"amount_gwei"`
}

// PreRunFundingPool credits a deterministic pool of derived EOAs in the funding
// block, so stateful benchmarks that draw senders from a pool (EEST's
// yield_distinct_sender) have funded senders. The i-th address is the EOA of
// private key int(keccak256(base_key_seed)) + i, matching execution-specs'
// SENDER_BASE_KEY derivation. Count must cover the largest per-variant sender
// count (gas_benchmark_value / per-iteration cost) across the selected tests.
type PreRunFundingPool struct {
	BaseKeySeed string  `yaml:"base_key_seed" mapstructure:"base_key_seed"`
	Count       int     `yaml:"count" mapstructure:"count"`
	AmountGwei  *uint64 `yaml:"amount_gwei,omitempty" mapstructure:"amount_gwei"`
}

// PreRunDefaults are the per-target build parameters hoistable to the top level
// under builder.pre_runs.config. Every field is also present on PreRunTarget; a
// non-nil/non-empty value on the target wins. See ResolveTarget.
type PreRunDefaults struct {
	FillerImage      string                       `yaml:"filler_image,omitempty" mapstructure:"filler_image"`
	Fork             string                       `yaml:"fork,omitempty" mapstructure:"fork"`
	Tests            []string                     `yaml:"tests,omitempty" mapstructure:"tests"`
	Filter           string                       `yaml:"filter,omitempty" mapstructure:"filter"`
	Marker           string                       `yaml:"marker,omitempty" mapstructure:"marker"`
	AddressStubsFile string                       `yaml:"address_stubs_file,omitempty" mapstructure:"address_stubs_file"`
	AddressStubs     map[string]map[string]string `yaml:"address_stubs,omitempty" mapstructure:"address_stubs"`
	RPCSeedKey       string                       `yaml:"rpc_seed_key,omitempty" mapstructure:"rpc_seed_key"`
	DataDirMethod    string                       `yaml:"datadir_method,omitempty" mapstructure:"datadir_method"`
	FillerExtraArgs  []string                     `yaml:"filler_extra_args,omitempty" mapstructure:"filler_extra_args"`
	// FillEnv are extra environment variables passed to the fill-stateful
	// container, e.g. BLOATNET_RECEIVER_CONTRACT_COUNT to shrink the setup for a
	// smoke run. Merged over benchmarkoor's own fill env (which wins on conflict).
	FillEnv map[string]string `yaml:"fill_env,omitempty" mapstructure:"fill_env"`
	// GasBenchmarkValues are the millions-of-gas budgets fill-stateful
	// parametrizes the setup tests against (e.g. test_setup_contracts packs its
	// deployment txs into blocks of this size). Passed as fill-stateful's
	// --gas-benchmark-values.
	GasBenchmarkValues []int `yaml:"gas_benchmark_values,omitempty" mapstructure:"gas_benchmark_values"`
	// GasLimit is the gas-bump target (default DefaultPreRunGasLimit).
	GasLimit *uint64 `yaml:"gas_limit,omitempty" mapstructure:"gas_limit"`
	// GasBumpMaxBlocks caps the empty gas-bump blocks (default
	// DefaultPreRunGasBumpMaxBlocks).
	GasBumpMaxBlocks *int `yaml:"gas_bump_max_blocks,omitempty" mapstructure:"gas_bump_max_blocks"`
	// FundingAccounts are credited in the funding block. Empty means the
	// funding block is skipped.
	FundingAccounts []PreRunFundingAccount `yaml:"funding_accounts,omitempty" mapstructure:"funding_accounts"`
	// FundingPools credit derived sender pools in the funding block (see
	// PreRunFundingPool) — for stateful benchmarks that draw senders from a pool.
	FundingPools []PreRunFundingPool `yaml:"funding_pools,omitempty" mapstructure:"funding_pools"`
	// Predeploy, when set, deploys contracts on a pre-fork before the target fork
	// activates (see PreRunPredeploy). Requires genesis_eip_override to schedule
	// the target fork's activation timestamp.
	Predeploy *PreRunPredeploy `yaml:"predeploy,omitempty" mapstructure:"predeploy"`
	// SchelkOptions configures schelk-specific behaviour; only valid when
	// datadir_method is "schelk".
	SchelkOptions *SchelkOptions `yaml:"schelk_options,omitempty" mapstructure:"schelk_options"`
}

// PreRunTarget is one pre-run: advance a snapshot datadir by gas-bumping,
// funding, and filling setup tests, persisting the result to OutputDir.
// Identity/locator fields live on the target; the remaining fields mirror
// PreRunDefaults and are resolved via ResolveTarget.
type PreRunTarget struct {
	Name         string `yaml:"name,omitempty" mapstructure:"name"`
	FillerClient string `yaml:"filler_client" mapstructure:"filler_client"`
	SourceDir    string `yaml:"source_dir" mapstructure:"source_dir"`
	OutputDir    string `yaml:"output_dir" mapstructure:"output_dir"`
	// BundleDir overrides where the replay bundle is written (see
	// BundleParentDir). Needed when the advanced datadir is a schelk scratch,
	// whose contents a later `schelk restore` discards.
	BundleDir string `yaml:"bundle_dir,omitempty" mapstructure:"bundle_dir"`

	Genesis             string              `yaml:"genesis,omitempty" mapstructure:"genesis"`
	GenesisForkOverride map[string]uint64   `yaml:"genesis_fork_override,omitempty" mapstructure:"genesis_fork_override"`
	GenesisEIPOverride  *GenesisEIPOverride `yaml:"genesis_eip_override,omitempty" mapstructure:"genesis_eip_override"`
	Force               bool                `yaml:"force,omitempty" mapstructure:"force"`

	// ReplayFrom makes this a REPLAY target: instead of running the fill
	// (gas-bump + funding + fill-stateful), it boots FillerClient on a copy of
	// SourceDir and replays a recorded .request bundle onto it, so OutputDir
	// becomes the advanced datadir. This works for non-filler clients
	// (reth/ethrex) too, since replay needs only the engine API. It resolves two
	// ways: a declared non-replay pre_runs target name (replays that target's
	// output_dir/pre_run_bundle bundle) or an absolute path to a .request file or
	// a pre_run_bundle directory.
	ReplayFrom string `yaml:"replay_from,omitempty" mapstructure:"replay_from"`

	// Hoistable fields (mirror PreRunDefaults).
	FillerImage        string                       `yaml:"filler_image,omitempty" mapstructure:"filler_image"`
	Fork               string                       `yaml:"fork,omitempty" mapstructure:"fork"`
	Tests              []string                     `yaml:"tests,omitempty" mapstructure:"tests"`
	Filter             string                       `yaml:"filter,omitempty" mapstructure:"filter"`
	Marker             string                       `yaml:"marker,omitempty" mapstructure:"marker"`
	AddressStubsFile   string                       `yaml:"address_stubs_file,omitempty" mapstructure:"address_stubs_file"`
	AddressStubs       map[string]map[string]string `yaml:"address_stubs,omitempty" mapstructure:"address_stubs"`
	RPCSeedKey         string                       `yaml:"rpc_seed_key,omitempty" mapstructure:"rpc_seed_key"`
	DataDirMethod      string                       `yaml:"datadir_method,omitempty" mapstructure:"datadir_method"`
	FillerExtraArgs    []string                     `yaml:"filler_extra_args,omitempty" mapstructure:"filler_extra_args"`
	FillEnv            map[string]string            `yaml:"fill_env,omitempty" mapstructure:"fill_env"`
	GasBenchmarkValues []int                        `yaml:"gas_benchmark_values,omitempty" mapstructure:"gas_benchmark_values"`
	GasLimit           *uint64                      `yaml:"gas_limit,omitempty" mapstructure:"gas_limit"`
	GasBumpMaxBlocks   *int                         `yaml:"gas_bump_max_blocks,omitempty" mapstructure:"gas_bump_max_blocks"`
	FundingAccounts    []PreRunFundingAccount       `yaml:"funding_accounts,omitempty" mapstructure:"funding_accounts"`
	FundingPools       []PreRunFundingPool          `yaml:"funding_pools,omitempty" mapstructure:"funding_pools"`
	Predeploy          *PreRunPredeploy             `yaml:"predeploy,omitempty" mapstructure:"predeploy"`
	SchelkOptions      *SchelkOptions               `yaml:"schelk_options,omitempty" mapstructure:"schelk_options"`
}

// BuildsFillImage reports whether benchmarkoor should build the fill image
// rather than pulling a pre-built FillImage. Mirrors EESTPayloadsConfig.
func (p *PreRunsConfig) BuildsFillImage() bool {
	return p.FillDockerfile != "" || p.FillImage == ""
}

// ResolveFillImageTag returns the image reference for the fill container.
func (p *PreRunsConfig) ResolveFillImageTag() string {
	if p.FillImage != "" {
		return p.FillImage
	}

	return DefaultFillImageTag
}

// ResolveEESTRepo returns the configured EEST repo URL, defaulting to
// DefaultEESTRepo.
func (p *PreRunsConfig) ResolveEESTRepo() string {
	if p.EESTRepo != "" {
		return p.EESTRepo
	}

	return DefaultEESTRepo
}

// ResolveEESTRef returns the configured EEST ref, defaulting to DefaultEESTRef.
func (p *PreRunsConfig) ResolveEESTRef() string {
	if p.EESTRef != "" {
		return p.EESTRef
	}

	return DefaultEESTRef
}

// ResolveFillCommand returns the configured fill-stateful argv prefix, or
// DefaultFillCommand when unset.
func (p *PreRunsConfig) ResolveFillCommand() []string {
	if len(p.FillCommand) > 0 {
		return p.FillCommand
	}

	return DefaultFillCommand
}

// ResolveTarget returns a copy of the i-th target with any unset hoistable
// fields filled in from PreRunsConfig.Config. Identity/locator fields are never
// touched. Per-target value wins when set. When Config is nil, the target is
// returned unchanged.
func (p *PreRunsConfig) ResolveTarget(i int) PreRunTarget {
	t := p.Targets[i]
	if p.Config == nil {
		return t
	}

	g := p.Config

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

	if len(t.AddressStubs) == 0 && t.AddressStubsFile == "" {
		t.AddressStubs = g.AddressStubs
		t.AddressStubsFile = g.AddressStubsFile
	}

	if t.RPCSeedKey == "" {
		t.RPCSeedKey = g.RPCSeedKey
	}

	if t.DataDirMethod == "" {
		t.DataDirMethod = g.DataDirMethod
	}

	if len(t.FillerExtraArgs) == 0 {
		t.FillerExtraArgs = g.FillerExtraArgs
	}

	if len(t.FillEnv) == 0 {
		t.FillEnv = g.FillEnv
	}

	if len(t.GasBenchmarkValues) == 0 {
		t.GasBenchmarkValues = g.GasBenchmarkValues
	}

	if t.GasLimit == nil {
		t.GasLimit = g.GasLimit
	}

	if t.GasBumpMaxBlocks == nil {
		t.GasBumpMaxBlocks = g.GasBumpMaxBlocks
	}

	if len(t.FundingAccounts) == 0 {
		t.FundingAccounts = g.FundingAccounts
	}

	if len(t.FundingPools) == 0 {
		t.FundingPools = g.FundingPools
	}

	if t.Predeploy == nil {
		t.Predeploy = g.Predeploy
	}

	if t.SchelkOptions == nil {
		t.SchelkOptions = g.SchelkOptions
	}

	return t
}

// EffectiveName returns the target's user-facing name, defaulting to the filler
// client when Name was not set.
func (t *PreRunTarget) EffectiveName() string {
	if t.Name != "" {
		return t.Name
	}

	return t.FillerClient
}

// IsReplay reports whether the target advances its datadir by replaying a
// recorded bundle (ReplayFrom set) instead of running the fill.
func (t *PreRunTarget) IsReplay() bool {
	return t.ReplayFrom != ""
}

// IsInPlace reports whether the pre-run advances source_dir in place instead of
// copying it to output_dir first. This is the schelk case: the source lives on a
// copy-on-write scratch, so the filler boots directly on it (no multi-TB copy)
// and output_dir is unused. Other methods copy source_dir → output_dir.
func (t *PreRunTarget) IsInPlace() bool {
	return t.DataDirMethod == "schelk"
}

// PredeployActivationTS returns the target-fork activation timestamp that a
// predeploy's pre-fork blocks must precede, read from whichever genesis override
// schedules it. The two override forms are genesis-format specific and mutually
// exclusive in practice: genesis_eip_override schedules per-EIP transitions in a
// parity/nethermind chainspec, genesis_fork_override sets <fork>Time in a
// geth-format genesis. Preferring the EIP override matches patchFillerGenesis,
// which applies fork overrides first — a target setting both is already
// ambiguous, and validation rejects a zero timestamp either way.
//
// ok is false when neither override schedules the target fork, or it does so at
// timestamp 0 (which would put activation at or before every block, leaving no
// pre-fork window to deploy in).
func (t *PreRunTarget) PredeployActivationTS() (timestamp uint64, ok bool) {
	if t.GenesisEIPOverride != nil && len(t.GenesisEIPOverride.EIPs) > 0 {
		return t.GenesisEIPOverride.Timestamp, t.GenesisEIPOverride.Timestamp > 0
	}

	if ts, found := t.GenesisForkOverride[t.Fork]; found {
		return ts, ts > 0
	}

	return 0, false
}

// AdvancedDir returns the directory that holds the advanced datadir after the
// pre-run: source_dir for an in-place (schelk) target, output_dir otherwise.
// Downstream stages (eest_payloads, runner) boot from it.
func (t *PreRunTarget) AdvancedDir() string {
	if t.IsInPlace() {
		return t.SourceDir
	}

	return t.OutputDir
}

// ShouldPromote reports whether the advanced datadir should replace the schelk
// virgin baseline once the pre-run's client has stopped. Validation guarantees
// schelk_options is only set on a schelk target, so the method check here is
// belt-and-braces against a caller building a target by hand.
func (t *PreRunTarget) ShouldPromote() bool {
	return t.SchelkOptions != nil && t.SchelkOptions.Promote && t.IsInPlace()
}

// BundleParentDir returns the directory the replay bundle is written under (in
// its PreRunBundleSubdir subdirectory), and that replay_from and the runner's
// eest_fixtures.pre_runs read it back from. It defaults to AdvancedDir, so the
// bundle travels with the datadir it describes.
//
// bundle_dir overrides that, and an in-place (schelk) target generally needs it
// to: there the advanced datadir IS the schelk scratch, so the bundle lands on a
// copy-on-write volume that the next `schelk restore` discards — including the
// restore the runner itself performs to get back to the baseline the bundle
// replays onto. Pointing bundle_dir at ordinary storage keeps the bundle alive
// across that reset.
func (t *PreRunTarget) BundleParentDir() string {
	if t.BundleDir != "" {
		return t.BundleDir
	}

	return t.AdvancedDir()
}

// ResolveGasLimit returns the gas-bump target, defaulting to
// DefaultPreRunGasLimit.
func (t *PreRunTarget) ResolveGasLimit() uint64 {
	if t.GasLimit != nil {
		return *t.GasLimit
	}

	return DefaultPreRunGasLimit
}

// ResolveGasBumpMaxBlocks returns the gas-bump safety cap, defaulting to
// DefaultPreRunGasBumpMaxBlocks.
func (t *PreRunTarget) ResolveGasBumpMaxBlocks() int {
	if t.GasBumpMaxBlocks != nil {
		return *t.GasBumpMaxBlocks
	}

	return DefaultPreRunGasBumpMaxBlocks
}

// ResolveAmountGwei returns the funding account's withdrawal amount in gwei,
// defaulting to DefaultPreRunFundingAmountGwei.
func (a *PreRunFundingAccount) ResolveAmountGwei() uint64 {
	if a.AmountGwei != nil {
		return *a.AmountGwei
	}

	return DefaultPreRunFundingAmountGwei
}

// ResolveAmountGwei returns the pool's per-address withdrawal amount in gwei,
// defaulting to DefaultPreRunFundingAmountGwei.
func (p *PreRunFundingPool) ResolveAmountGwei() uint64 {
	if p.AmountGwei != nil {
		return *p.AmountGwei
	}

	return DefaultPreRunFundingAmountGwei
}

// ResolveDeployerFundGwei returns the deployer's funding-block withdrawal amount
// in gwei, defaulting to DefaultPreRunFundingAmountGwei.
func (p *PreRunPredeploy) ResolveDeployerFundGwei() uint64 {
	if p.DeployerFundGwei != nil {
		return *p.DeployerFundGwei
	}

	return DefaultPreRunFundingAmountGwei
}

// rawPreRunFillEnv holds just the fill_env map for one level of the pre_runs
// config (the config block or a single target).
type rawPreRunFillEnv struct {
	FillEnv map[string]string `yaml:"fill_env"`
}

// rawPreRunsBuilderConfig re-parses builder.pre_runs to recover fill_env keys
// with their original casing (Viper lowercases all map keys, which would break
// case-sensitive environment variable names like BLOATNET_RECEIVER_CONTRACT_COUNT).
type rawPreRunsBuilderConfig struct {
	Builder struct {
		PreRuns struct {
			Config  *rawPreRunFillEnv  `yaml:"config"`
			Targets []rawPreRunFillEnv `yaml:"targets"`
		} `yaml:"pre_runs"`
	} `yaml:"builder"`
}

// restorePreRunFillEnvKeyCasing re-parses the raw YAML to recover the original
// casing of builder.pre_runs fill_env keys that Viper lowercased. Mirrors
// restoreAddressStubsKeyCasing: the config block accumulates across files (later
// files win per key), and per-target maps are restored positionally only when
// the winning file's target list aligns 1:1 with the resolved config.
func restorePreRunFillEnvKeyCasing(cfg *Config, rawYAMLs []string) {
	if cfg.Builder == nil || cfg.Builder.PreRuns == nil {
		return
	}

	pr := cfg.Builder.PreRuns

	configEnv := make(map[string]string)

	var rawTargets []rawPreRunFillEnv

	for _, raw := range rawYAMLs {
		var parsed rawPreRunsBuilderConfig
		if err := yaml.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}

		if c := parsed.Builder.PreRuns.Config; c != nil {
			maps.Copy(configEnv, c.FillEnv)
		}

		if len(parsed.Builder.PreRuns.Targets) > 0 {
			rawTargets = parsed.Builder.PreRuns.Targets
		}
	}

	if pr.Config != nil && len(configEnv) > 0 {
		pr.Config.FillEnv = configEnv
	}

	if len(rawTargets) != len(pr.Targets) {
		return
	}

	for i := range pr.Targets {
		if len(rawTargets[i].FillEnv) > 0 {
			pr.Targets[i].FillEnv = rawTargets[i].FillEnv
		}
	}
}

// GetPreRunsContainerRuntime returns the container runtime to use for pre_runs
// builds, falling back to the runner's runtime when unset.
func (c *Config) GetPreRunsContainerRuntime() string {
	if c.Builder != nil && c.Builder.PreRuns != nil && c.Builder.PreRuns.ContainerRuntime != "" {
		return c.Builder.PreRuns.ContainerRuntime
	}

	return c.GetContainerRuntime()
}

// validatePreRuns enforces the builder.pre_runs rules: valid container
// runtime/pull policy, an existing fill_dockerfile when set, supported filler
// clients, required locator fields (source_dir/output_dir/tests/fork/
// filler_image), a valid datadir method, and uniqueness of target names and
// output_dirs. Existence of source_dir/genesis/address_stubs_file is checked at
// build time (an earlier state-actor target may still produce them).
func (c *Config) validatePreRuns() error {
	if c.Builder == nil || c.Builder.PreRuns == nil {
		return nil
	}

	pr := c.Builder.PreRuns

	if !validContainerRuntimes[pr.ContainerRuntime] {
		return fmt.Errorf(
			"builder.pre_runs.container_runtime: invalid value %q "+
				"(must be \"docker\" or \"podman\")", pr.ContainerRuntime,
		)
	}

	if !stateActorValidPullPolicies[pr.PullPolicy] {
		return fmt.Errorf(
			"builder.pre_runs.pull_policy: invalid value %q "+
				"(must be \"always\", \"if-not-present\", or \"never\")",
			pr.PullPolicy,
		)
	}

	if pr.FillDockerfile != "" {
		if _, err := os.Stat(pr.FillDockerfile); err != nil {
			return fmt.Errorf("builder.pre_runs.fill_dockerfile: %w", err)
		}
	}

	seenOutputs := make(map[string]int, len(pr.Targets))
	seenNames := make(map[string]int, len(pr.Targets))

	// There is ONE schelk volume per host, so a promote by one target replaces the
	// baseline every other target restores from. Allowing two would mean the last
	// promote silently wins — and a promote cannot be undone.
	promoter := -1

	// Pre-collect target names + replay-ness so a replay_from can reference an
	// earlier non-replay target regardless of loop position.
	targetIndex := make(map[string]int, len(pr.Targets))
	targetIsReplay := make(map[string]bool, len(pr.Targets))

	for i := range pr.Targets {
		rt := pr.ResolveTarget(i)
		targetIndex[rt.EffectiveName()] = i
		targetIsReplay[rt.EffectiveName()] = rt.IsReplay()
	}

	for i := range pr.Targets {
		t := pr.ResolveTarget(i)
		prefix := fmt.Sprintf("builder.pre_runs.targets[%d]", i)

		name := t.EffectiveName()
		if prev, dup := seenNames[name]; dup {
			return fmt.Errorf(
				"%s: name %q duplicates targets[%d] (set an explicit name to disambiguate)",
				prefix, name, prev,
			)
		}

		seenNames[name] = i

		if err := validatePreRunPaths(&t, prefix, seenOutputs, i); err != nil {
			return err
		}

		if t.ShouldPromote() {
			if promoter >= 0 {
				return fmt.Errorf(
					"%s.schelk_options.promote: targets[%d] already promotes, and both share the "+
						"one schelk volume — only one target may promote it", prefix, promoter,
				)
			}

			promoter = i
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

		// Replay targets advance by replaying a bundle, not by filling. They can
		// use any bootable client (incl. non-fillers) and need none of the
		// fill-specific config below.
		if t.IsReplay() {
			if err := validateReplayFrom(&t, prefix, targetIndex, targetIsReplay, i); err != nil {
				return err
			}

			continue
		}

		if _, ok := eestFillerSupportedClients[t.FillerClient]; !ok {
			return fmt.Errorf(
				"%s.filler_client: %q cannot act as the fill-stateful filler "+
					"(supported: geth, besu, nethermind)",
				prefix, t.FillerClient,
			)
		}

		if len(t.Tests) == 0 {
			return fmt.Errorf(
				"%s.tests is required (at least one pytest path, e.g. "+
					"tests/benchmark/stateful/bloatnet/test_setup_contracts.py)",
				prefix,
			)
		}

		if t.Fork == "" {
			return fmt.Errorf(
				"%s.fork is required (set it on the target or builder.pre_runs.config.fork)",
				prefix,
			)
		}

		if t.GasLimit != nil && *t.GasLimit == 0 {
			return fmt.Errorf("%s.gas_limit must be > 0 when set", prefix)
		}

		if t.GasBumpMaxBlocks != nil && *t.GasBumpMaxBlocks < 0 {
			return fmt.Errorf("%s.gas_bump_max_blocks must be >= 0 when set", prefix)
		}

		for j := range t.FundingAccounts {
			if t.FundingAccounts[j].Address == "" {
				return fmt.Errorf("%s.funding_accounts[%d].address is required", prefix, j)
			}
		}

		for j := range t.FundingPools {
			if t.FundingPools[j].BaseKeySeed == "" {
				return fmt.Errorf("%s.funding_pools[%d].base_key_seed is required", prefix, j)
			}

			if t.FundingPools[j].Count <= 0 {
				return fmt.Errorf("%s.funding_pools[%d].count must be > 0", prefix, j)
			}
		}

		if t.Predeploy != nil {
			if err := validatePredeploy(&t, prefix); err != nil {
				return err
			}
		}
	}

	return nil
}

// validatePredeploy enforces the builder.pre_runs predeploy rules: a pre_fork,
// a valid deployer key, at least one valid-hex contract, and a
// genesis_eip_override that schedules the target fork at a positive activation
// timestamp the deploy blocks precede.
func validatePredeploy(t *PreRunTarget, prefix string) error {
	p := t.Predeploy

	if p.PreFork == "" {
		return fmt.Errorf(
			"%s.predeploy.pre_fork is required (the fork the snapshot boots at, e.g. osaka)", prefix,
		)
	}

	if err := validateHexPrivateKey(p.DeployerKey); err != nil {
		return fmt.Errorf("%s.predeploy.deployer_key: %w", prefix, err)
	}

	if len(p.Contracts) == 0 {
		return fmt.Errorf("%s.predeploy.contracts is required (at least one contract to deploy)", prefix)
	}

	for j := range p.Contracts {
		if err := validatePredeployContract(&p.Contracts[j], fmt.Sprintf("%s.predeploy.contracts[%d]", prefix, j)); err != nil {
			return err
		}
	}

	if _, ok := t.PredeployActivationTS(); !ok {
		return fmt.Errorf(
			"%s.predeploy requires the target fork's activation to be scheduled at a "+
				"positive timestamp the pre-fork deploy blocks precede: set "+
				"genesis_eip_override (parity/nethermind chainspec) or "+
				"genesis_fork_override.%s (geth-format genesis)", prefix, t.Fork,
		)
	}

	if p.DeployerFundGwei != nil && *p.DeployerFundGwei == 0 {
		return fmt.Errorf("%s.predeploy.deployer_fund_gwei must be > 0 when set", prefix)
	}

	return nil
}

// validatePredeployContract enforces that a predeploy entry uses exactly one of
// its two forms: `code` (plain CREATE of runtime bytecode) or `to` + `data` +
// `address` (a call to a deployer contract, e.g. a CREATE2 factory). The call
// form requires `address` because, unlike CREATE, benchmarkoor cannot derive
// where the code will land without knowing the callee's deployment scheme — it
// is what the post-block code check verifies against.
func validatePredeployContract(c *PreRunPredeployContract, prefix string) error {
	if c.IsCall() {
		if c.Code != "" {
			return fmt.Errorf(
				"%s: set either code (plain CREATE) or to+data (deployer call), not both", prefix,
			)
		}

		if err := validateHexAddress(c.To); err != nil {
			return fmt.Errorf("%s.to: %w", prefix, err)
		}

		if err := validateHexBytecode(c.Data); err != nil {
			return fmt.Errorf("%s.data: %w", prefix, err)
		}

		if c.Address == "" {
			return fmt.Errorf(
				"%s.address is required with to+data (the address the deployed code is "+
					"verified at; it cannot be derived from the call)", prefix,
			)
		}

		if err := validateHexAddress(c.Address); err != nil {
			return fmt.Errorf("%s.address: %w", prefix, err)
		}

		return nil
	}

	if c.Data != "" || c.Address != "" {
		return fmt.Errorf("%s: data/address require to (the deployer contract to call)", prefix)
	}

	if err := validateHexBytecode(c.Code); err != nil {
		return fmt.Errorf("%s.code: %w", prefix, err)
	}

	return nil
}

// validateHexAddress checks s is a 0x-prefixed 20-byte hex address.
func validateHexAddress(s string) error {
	h := strings.TrimPrefix(s, "0x")
	if len(h) != 40 {
		return fmt.Errorf("expected a 20-byte (40 hex char) address, got %d hex chars", len(h))
	}

	if _, err := hex.DecodeString(h); err != nil {
		return fmt.Errorf("not valid hex: %w", err)
	}

	return nil
}

// validateHexPrivateKey checks s is a 0x-optional 32-byte hex private key.
func validateHexPrivateKey(s string) error {
	h := strings.TrimPrefix(s, "0x")
	if len(h) != 64 {
		return fmt.Errorf("expected a 32-byte (64 hex char) private key, got %d hex chars", len(h))
	}

	if _, err := hex.DecodeString(h); err != nil {
		return fmt.Errorf("not valid hex: %w", err)
	}

	return nil
}

// validateHexBytecode checks s is non-empty, 0x-optional, even-length hex.
func validateHexBytecode(s string) error {
	h := strings.TrimPrefix(s, "0x")
	if len(h) == 0 {
		return fmt.Errorf("must be non-empty runtime bytecode")
	}

	if len(h)%2 != 0 {
		return fmt.Errorf("odd number of hex digits (%d)", len(h))
	}

	if _, err := hex.DecodeString(h); err != nil {
		return fmt.Errorf("not valid hex: %w", err)
	}

	return nil
}

// validateReplayFrom validates a replay target's replay_from: it must name an
// earlier non-replay pre_runs target, or be an absolute path to a .request file
// or a pre_run_bundle directory.
func validateReplayFrom(
	t *PreRunTarget, prefix string, targetIndex map[string]int, targetIsReplay map[string]bool, i int,
) error {
	if t.FillerClient == "" {
		return fmt.Errorf(
			"%s.filler_client is required (the client to boot and advance by replay)", prefix,
		)
	}

	if srcIdx, isTarget := targetIndex[t.ReplayFrom]; isTarget {
		if targetIsReplay[t.ReplayFrom] {
			return fmt.Errorf(
				"%s.replay_from: %q is itself a replay target; replay from a fill target",
				prefix, t.ReplayFrom,
			)
		}

		if srcIdx >= i {
			return fmt.Errorf(
				"%s.replay_from: target %q must be declared before this target", prefix, t.ReplayFrom,
			)
		}

		return nil
	}

	// Not a declared target name: treat replay_from as a filesystem path.
	if !filepath.IsAbs(t.ReplayFrom) {
		return fmt.Errorf(
			"%s.replay_from %q is neither a declared pre_runs target nor an absolute path "+
				"to a .request file or pre_run_bundle directory",
			prefix, t.ReplayFrom,
		)
	}

	return nil
}

// validatePreRunPaths checks source_dir / output_dir are absolute and
// output_dir is unique. Mirrors validateEESTPayloadPaths.
func validatePreRunPaths(t *PreRunTarget, prefix string, seenOutputs map[string]int, i int) error {
	if t.SourceDir == "" {
		return fmt.Errorf("%s.source_dir is required", prefix)
	}

	if !filepath.IsAbs(t.SourceDir) {
		return fmt.Errorf("%s.source_dir must be an absolute path, got %q", prefix, t.SourceDir)
	}

	if t.BundleDir != "" && !filepath.IsAbs(t.BundleDir) {
		return fmt.Errorf("%s.bundle_dir must be an absolute path, got %q", prefix, t.BundleDir)
	}

	// Every schelk_options field manipulates the schelk volumes, so silently
	// ignoring them under another method would hide a real misconfiguration —
	// notably a promote that the user believes is persisting their datadir.
	if t.SchelkOptions != nil {
		if !t.IsInPlace() {
			return fmt.Errorf(
				"%s.schelk_options requires datadir_method: schelk, got %q", prefix, t.DataDirMethod,
			)
		}

		// promote_post_pre_runs is the runner-side spelling; nothing in the builder
		// reads it, so accepting it here would be a silent no-op.
		if t.SchelkOptions.PromotePostPreRuns {
			return fmt.Errorf(
				"%s.schelk_options.promote_post_pre_runs is a runner datadir option; "+
					"the builder spelling is promote", prefix,
			)
		}
	}

	// An in-place (schelk) target advances source_dir directly; output_dir is
	// unused, so it is neither required nor uniqueness-checked.
	if t.IsInPlace() {
		if t.OutputDir != "" && !filepath.IsAbs(t.OutputDir) {
			return fmt.Errorf("%s.output_dir must be an absolute path, got %q", prefix, t.OutputDir)
		}

		return nil
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

	return nil
}
