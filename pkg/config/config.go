package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/go-units"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/spf13/viper"
)

const (
	// DefaultJWT is the default JWT secret used for Engine API authentication.
	DefaultJWT = "5a64f13bfb41a147711492237995b437433bcbec80a7eb2daae11132098d7bae"

	// DefaultDockerNetwork is the default Docker network name.
	DefaultDockerNetwork = "benchmarkoor"

	// DefaultLogLevel is the default logging level.
	DefaultLogLevel = "info"

	// DefaultResultsDir is the default directory for benchmark results.
	DefaultResultsDir = "./results"

	// DefaultPullPolicy is the default image pull policy.
	DefaultPullPolicy = "always"

	// DefaultDropCachesPath is the default path to the Linux drop_caches file.
	DefaultDropCachesPath = "/proc/sys/vm/drop_caches"
)

// Config is the root configuration for benchmarkoor.
type Config struct {
	Global    GlobalConfig    `yaml:"global" mapstructure:"global"`
	Benchmark BenchmarkConfig `yaml:"benchmark" mapstructure:"benchmark"`
	Client    ClientConfig    `yaml:"client" mapstructure:"client"`
}

// GlobalConfig contains global application settings.
type GlobalConfig struct {
	LogLevel           string            `yaml:"log_level" mapstructure:"log_level"`
	ClientLogsToStdout bool              `yaml:"client_logs_to_stdout" mapstructure:"client_logs_to_stdout"`
	DockerNetwork      string            `yaml:"docker_network" mapstructure:"docker_network"`
	CleanupOnStart     bool              `yaml:"cleanup_on_start" mapstructure:"cleanup_on_start"`
	Directories        DirectoriesConfig `yaml:"directories,omitempty" mapstructure:"directories"`
	DropCachesPath     string            `yaml:"drop_caches_path,omitempty" mapstructure:"drop_caches_path"`
	GitHubToken        string            `yaml:"github_token,omitempty" mapstructure:"github_token"`
}

// DirectoriesConfig contains directory path configurations.
type DirectoriesConfig struct {
	// TmpDataDir is the directory for temporary datadir copies.
	// If empty, uses the system default temp directory.
	TmpDataDir string `yaml:"tmp_datadir,omitempty" mapstructure:"tmp_datadir"`
	// TmpCacheDir is the directory for executor cache (git clones, etc).
	// If empty, uses ~/.cache/benchmarkoor.
	TmpCacheDir string `yaml:"tmp_cachedir,omitempty" mapstructure:"tmp_cachedir"`
}

// BenchmarkConfig contains benchmark-specific settings.
type BenchmarkConfig struct {
	ResultsDir                      string      `yaml:"results_dir" mapstructure:"results_dir"`
	ResultsOwner                    string      `yaml:"results_owner,omitempty" mapstructure:"results_owner"`
	SystemResourceCollectionEnabled *bool       `yaml:"system_resource_collection_enabled,omitempty" mapstructure:"system_resource_collection_enabled"`
	GenerateResultsIndex            bool        `yaml:"generate_results_index" mapstructure:"generate_results_index"`
	GenerateSuiteStats              bool        `yaml:"generate_suite_stats" mapstructure:"generate_suite_stats"`
	Tests                           TestsConfig `yaml:"tests,omitempty" mapstructure:"tests"`
}

// TestsConfig contains test execution settings.
type TestsConfig struct {
	Filter string       `yaml:"filter,omitempty" mapstructure:"filter"`
	Source SourceConfig `yaml:"source,omitempty" mapstructure:"source"`
}

// SourceConfig defines where to find test files.
type SourceConfig struct {
	// New unified source options.
	Git          *GitSourceV2        `yaml:"git,omitempty" mapstructure:"git"`
	Local        *LocalSourceV2      `yaml:"local,omitempty" mapstructure:"local"`
	EESTFixtures *EESTFixturesSource `yaml:"eest_fixtures,omitempty" mapstructure:"eest_fixtures"`
}

// EESTFixturesSource defines an EEST fixtures source from GitHub releases or artifacts.
type EESTFixturesSource struct {
	GitHubRepo     string `yaml:"github_repo" mapstructure:"github_repo"`
	GitHubRelease  string `yaml:"github_release,omitempty" mapstructure:"github_release"`
	FixturesURL    string `yaml:"fixtures_url,omitempty" mapstructure:"fixtures_url"`
	GenesisURL     string `yaml:"genesis_url,omitempty" mapstructure:"genesis_url"`
	FixturesSubdir string `yaml:"fixtures_subdir,omitempty" mapstructure:"fixtures_subdir"`
	// GitHub Actions artifact support (alternative to releases).
	FixturesArtifactName  string `yaml:"fixtures_artifact_name,omitempty" mapstructure:"fixtures_artifact_name"`
	GenesisArtifactName   string `yaml:"genesis_artifact_name,omitempty" mapstructure:"genesis_artifact_name"`
	FixturesArtifactRunID string `yaml:"fixtures_artifact_run_id,omitempty" mapstructure:"fixtures_artifact_run_id"`
	GenesisArtifactRunID  string `yaml:"genesis_artifact_run_id,omitempty" mapstructure:"genesis_artifact_run_id"`
}

// UseArtifacts returns true if the source is configured to use GitHub Actions artifacts.
func (e *EESTFixturesSource) UseArtifacts() bool {
	return e.FixturesArtifactName != "" || e.GenesisArtifactName != ""
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

// StepsConfig defines glob patterns for each step type.
type StepsConfig struct {
	Setup   []string `yaml:"setup,omitempty" mapstructure:"setup"`
	Test    []string `yaml:"test,omitempty" mapstructure:"test"`
	Cleanup []string `yaml:"cleanup,omitempty" mapstructure:"cleanup"`
}

// IsConfigured returns true if any test source is configured.
func (s *SourceConfig) IsConfigured() bool {
	return s.Git != nil || s.Local != nil || s.EESTFixtures != nil
}

// DefaultContainerDir is the default container mount path for data directories.
const DefaultContainerDir = "/data"

// DataDirConfig configures a pre-populated data directory for a client.
type DataDirConfig struct {
	SourceDir    string `yaml:"source_dir" json:"source_dir" mapstructure:"source_dir"`
	ContainerDir string `yaml:"container_dir,omitempty" json:"container_dir,omitempty" mapstructure:"container_dir"`
	Method       string `yaml:"method,omitempty" json:"method,omitempty" mapstructure:"method"`
}

// ResourceLimits configures container resource constraints.
type ResourceLimits struct {
	CpusetCount  *int   `yaml:"cpuset_count,omitempty" mapstructure:"cpuset_count" json:"cpuset_count,omitempty"`
	Cpuset       []int  `yaml:"cpuset,omitempty" mapstructure:"cpuset" json:"cpuset,omitempty"`
	Memory       string `yaml:"memory,omitempty" mapstructure:"memory" json:"memory,omitempty"`
	SwapDisabled bool   `yaml:"swap_disabled,omitempty" mapstructure:"swap_disabled" json:"swap_disabled,omitempty"`
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

	return nil
}

// Validate checks the datadir configuration for errors.
func (d *DataDirConfig) Validate(prefix string) error {
	if d.SourceDir == "" {
		return fmt.Errorf("%s: source_dir is required", prefix)
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

	validMethods := map[string]bool{"": true, "copy": true, "overlayfs": true, "fuse-overlayfs": true, "zfs": true}
	if !validMethods[d.Method] {
		return fmt.Errorf("%s: invalid method %q, must be: copy, overlayfs, fuse-overlayfs, zfs", prefix, d.Method)
	}

	return nil
}

// ClientConfig contains client configuration settings.
type ClientConfig struct {
	Config    ClientDefaults            `yaml:"config" mapstructure:"config"`
	DataDirs  map[string]*DataDirConfig `yaml:"datadirs,omitempty" mapstructure:"datadirs"`
	Instances []ClientInstance          `yaml:"instances" mapstructure:"instances"`
}

// ClientDefaults contains default settings for all clients.
type ClientDefaults struct {
	JWT              string            `yaml:"jwt" mapstructure:"jwt"`
	Genesis          map[string]string `yaml:"genesis" mapstructure:"genesis"`
	DropMemoryCaches string            `yaml:"drop_memory_caches,omitempty" mapstructure:"drop_memory_caches"`
	ResourceLimits   *ResourceLimits   `yaml:"resource_limits,omitempty" mapstructure:"resource_limits"`
}

// ClientInstance defines a single client instance to benchmark.
type ClientInstance struct {
	ID               string            `yaml:"id" mapstructure:"id"`
	Client           string            `yaml:"client" mapstructure:"client"`
	Image            string            `yaml:"image,omitempty" mapstructure:"image"`
	Entrypoint       []string          `yaml:"entrypoint,omitempty" mapstructure:"entrypoint"`
	Command          []string          `yaml:"command,omitempty" mapstructure:"command"`
	ExtraArgs        []string          `yaml:"extra_args,omitempty" mapstructure:"extra_args"`
	PullPolicy       string            `yaml:"pull_policy,omitempty" mapstructure:"pull_policy"`
	Restart          string            `yaml:"restart,omitempty" mapstructure:"restart"`
	Environment      map[string]string `yaml:"environment,omitempty" mapstructure:"environment"`
	Genesis          string            `yaml:"genesis,omitempty" mapstructure:"genesis"`
	DataDir          *DataDirConfig    `yaml:"datadir,omitempty" mapstructure:"datadir"`
	DropMemoryCaches string            `yaml:"drop_memory_caches,omitempty" mapstructure:"drop_memory_caches"`
	ResourceLimits   *ResourceLimits   `yaml:"resource_limits,omitempty" mapstructure:"resource_limits"`
}

// Load reads and parses configuration files from the given paths.
// When multiple paths are provided, configs are merged in order (later values override earlier).
// Environment variables can be substituted in config values using ${VAR} or $VAR syntax.
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

	// Load and merge configs in order.
	for i, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading config file %q: %w", path, err)
		}

		expanded := os.ExpandEnv(string(content))

		if i == 0 {
			if err := v.ReadConfig(strings.NewReader(expanded)); err != nil {
				return nil, fmt.Errorf("parsing config %q: %w", path, err)
			}
		} else {
			if err := v.MergeConfig(strings.NewReader(expanded)); err != nil {
				return nil, fmt.Errorf("merging config %q: %w", path, err)
			}
		}
	}

	// Bind all known configuration keys to allow env var overrides.
	bindEnvKeys(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.applyDefaults()

	return &cfg, nil
}

// bindEnvKeys explicitly binds configuration keys to environment variables.
// This is required for Viper to recognize env vars for keys not present in the config file.
func bindEnvKeys(v *viper.Viper) {
	keys := []string{
		// Global settings
		"global.log_level",
		"global.client_logs_to_stdout",
		"global.docker_network",
		"global.cleanup_on_start",
		"global.directories.tmp_datadir",
		"global.directories.tmp_cachedir",
		"global.github_token",
		// Benchmark settings
		"benchmark.results_dir",
		"benchmark.results_owner",
		"benchmark.system_resource_collection_enabled",
		"benchmark.generate_results_index",
		"benchmark.generate_suite_stats",
		"benchmark.tests.filter",
		// Client settings
		"client.config.jwt",
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

	if c.Global.DockerNetwork == "" {
		c.Global.DockerNetwork = DefaultDockerNetwork
	}

	if c.Benchmark.ResultsDir == "" {
		c.Benchmark.ResultsDir = DefaultResultsDir
	}

	if c.Benchmark.SystemResourceCollectionEnabled == nil {
		enabled := true
		c.Benchmark.SystemResourceCollectionEnabled = &enabled
	}

	if c.Client.Config.JWT == "" {
		c.Client.Config.JWT = DefaultJWT
	}

	if c.Client.Config.Genesis == nil {
		c.Client.Config.Genesis = make(map[string]string, 6)
	}

	// Apply defaults to global datadirs.
	for _, dd := range c.Client.DataDirs {
		if dd != nil {
			if dd.Method == "" {
				dd.Method = "copy"
			}
			// Note: ContainerDir is intentionally not defaulted here.
			// If empty, the runner will use the client's spec.DataDir() at runtime.
		}
	}

	for i := range c.Client.Instances {
		if c.Client.Instances[i].PullPolicy == "" {
			c.Client.Instances[i].PullPolicy = DefaultPullPolicy
		}

		// Apply defaults to instance-level datadir.
		if c.Client.Instances[i].DataDir != nil {
			if c.Client.Instances[i].DataDir.Method == "" {
				c.Client.Instances[i].DataDir.Method = "copy"
			}
			// Note: ContainerDir is intentionally not defaulted here.
			// If empty, the runner will use the client's spec.DataDir() at runtime.
		}
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if len(c.Client.Instances) == 0 {
		return fmt.Errorf("at least one client instance must be configured")
	}

	seenIDs := make(map[string]struct{}, len(c.Client.Instances))

	for i, instance := range c.Client.Instances {
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

		// Validate instance-level datadir.
		if instance.DataDir != nil {
			if err := instance.DataDir.Validate(fmt.Sprintf("instance %q datadir", instance.ID)); err != nil {
				return err
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
	if c.Client.Config.ResourceLimits != nil {
		if err := c.Client.Config.ResourceLimits.Validate("client.config.resource_limits"); err != nil {
			return err
		}
	}

	// Validate global datadirs.
	for client, dd := range c.Client.DataDirs {
		if dd != nil {
			if err := dd.Validate(fmt.Sprintf("client.datadirs.%s", client)); err != nil {
				return err
			}
		}
	}

	if c.Benchmark.ResultsDir != "" {
		dir := filepath.Dir(c.Benchmark.ResultsDir)
		if dir != "." && dir != ".." {
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				return fmt.Errorf("results directory parent %q does not exist", dir)
			}
		}
	}

	// Validate test source configuration.
	if err := c.Benchmark.Tests.Source.Validate(); err != nil {
		return fmt.Errorf("tests config: %w", err)
	}

	// Validate drop_memory_caches settings.
	if err := c.validateDropMemoryCaches(); err != nil {
		return err
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

	if s.EESTFixtures != nil {
		count++
	}

	if count > 1 {
		return fmt.Errorf("cannot specify multiple sources (git, local, eest_fixtures)")
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

	if s.EESTFixtures != nil {
		if s.EESTFixtures.GitHubRepo == "" {
			return fmt.Errorf("eest_fixtures.github_repo is required")
		}

		// Must have either release or artifact configuration.
		hasRelease := s.EESTFixtures.GitHubRelease != ""
		hasArtifacts := s.EESTFixtures.FixturesArtifactName != ""

		if !hasRelease && !hasArtifacts {
			return fmt.Errorf("eest_fixtures: must specify either github_release or fixtures_artifact_name")
		}

		if hasRelease && hasArtifacts {
			return fmt.Errorf("eest_fixtures: cannot specify both github_release and fixtures_artifact_name")
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

	return c.Client.Config.Genesis[instance.Client]
}

// GetDropMemoryCaches returns the drop_memory_caches setting for an instance.
// Instance-level setting takes precedence over global default.
// Returns empty string if neither is set (disabled).
func (c *Config) GetDropMemoryCaches(instance *ClientInstance) string {
	if instance.DropMemoryCaches != "" {
		return instance.DropMemoryCaches
	}

	return c.Client.Config.DropMemoryCaches
}

// GetDropCachesPath returns the path to the drop_caches file.
// Returns the configured path or the default (/proc/sys/vm/drop_caches).
func (c *Config) GetDropCachesPath() string {
	if c.Global.DropCachesPath != "" {
		return c.Global.DropCachesPath
	}

	return DefaultDropCachesPath
}

// GetResourceLimits returns the resource limits for an instance.
// Instance-level limits take precedence over global defaults.
// Returns nil if no limits are configured.
func (c *Config) GetResourceLimits(instance *ClientInstance) *ResourceLimits {
	if instance.ResourceLimits != nil {
		return instance.ResourceLimits
	}

	return c.Client.Config.ResourceLimits
}

// validateDropMemoryCaches validates drop_memory_caches settings and checks permissions.
func (c *Config) validateDropMemoryCaches() error {
	// Check all instances for valid values and if feature is enabled.
	enabled := false

	for _, instance := range c.Client.Instances {
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
	if c.Global.DropCachesPath == "" && runtime.GOOS != "linux" {
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
