package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	ResultsDir           string      `yaml:"results_dir" mapstructure:"results_dir"`
	ResultsOwner         string      `yaml:"results_owner,omitempty" mapstructure:"results_owner"`
	GenerateResultsIndex bool        `yaml:"generate_results_index" mapstructure:"generate_results_index"`
	GenerateSuiteStats   bool        `yaml:"generate_suite_stats" mapstructure:"generate_suite_stats"`
	Tests                TestsConfig `yaml:"tests,omitempty" mapstructure:"tests"`
}

// TestsConfig contains test execution settings.
type TestsConfig struct {
	Filter string       `yaml:"filter,omitempty" mapstructure:"filter"`
	Source SourceConfig `yaml:"source,omitempty" mapstructure:"source"`
}

// SourceConfig defines where to find test files.
type SourceConfig struct {
	// New unified source options.
	Git   *GitSourceV2   `yaml:"git,omitempty" mapstructure:"git"`
	Local *LocalSourceV2 `yaml:"local,omitempty" mapstructure:"local"`
}

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
	return s.Git != nil || s.Local != nil
}

// DefaultContainerDir is the default container mount path for data directories.
const DefaultContainerDir = "/data"

// DataDirConfig configures a pre-populated data directory for a client.
type DataDirConfig struct {
	SourceDir    string `yaml:"source_dir" json:"source_dir" mapstructure:"source_dir"`
	ContainerDir string `yaml:"container_dir,omitempty" json:"container_dir,omitempty" mapstructure:"container_dir"`
	Method       string `yaml:"method,omitempty" json:"method,omitempty" mapstructure:"method"`
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

	validMethods := map[string]bool{"": true, "copy": true, "overlayfs": true, "fuse-overlayfs": true}
	if !validMethods[d.Method] {
		return fmt.Errorf("%s: invalid method %q, must be: copy, overlayfs, fuse-overlayfs", prefix, d.Method)
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
	JWT     string            `yaml:"jwt" mapstructure:"jwt"`
	Genesis map[string]string `yaml:"genesis" mapstructure:"genesis"`
}

// ClientInstance defines a single client instance to benchmark.
type ClientInstance struct {
	ID          string            `yaml:"id" mapstructure:"id"`
	Client      string            `yaml:"client" mapstructure:"client"`
	Image       string            `yaml:"image,omitempty" mapstructure:"image"`
	Entrypoint  []string          `yaml:"entrypoint,omitempty" mapstructure:"entrypoint"`
	Command     []string          `yaml:"command,omitempty" mapstructure:"command"`
	ExtraArgs   []string          `yaml:"extra_args,omitempty" mapstructure:"extra_args"`
	PullPolicy  string            `yaml:"pull_policy,omitempty" mapstructure:"pull_policy"`
	Restart     string            `yaml:"restart,omitempty" mapstructure:"restart"`
	Environment map[string]string `yaml:"environment,omitempty" mapstructure:"environment"`
	Genesis     string            `yaml:"genesis,omitempty" mapstructure:"genesis"`
	DataDir     *DataDirConfig    `yaml:"datadir,omitempty" mapstructure:"datadir"`
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
		// Benchmark settings
		"benchmark.results_dir",
		"benchmark.results_owner",
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

			if dd.ContainerDir == "" {
				dd.ContainerDir = DefaultContainerDir
			}
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

			if c.Client.Instances[i].DataDir.ContainerDir == "" {
				c.Client.Instances[i].DataDir.ContainerDir = DefaultContainerDir
			}
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

		if instance.Genesis == "" {
			if _, ok := c.Client.Config.Genesis[instance.Client]; !ok {
				return fmt.Errorf(
					"instance %q: no genesis URL configured for client %q",
					instance.ID,
					instance.Client,
				)
			}
		}

		// Validate instance-level datadir.
		if instance.DataDir != nil {
			if err := instance.DataDir.Validate(fmt.Sprintf("instance %q datadir", instance.ID)); err != nil {
				return err
			}
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

	return nil
}

// Validate checks the source configuration for errors.
func (s *SourceConfig) Validate() error {
	// No source configured is valid (tests are optional).
	if !s.IsConfigured() {
		return nil
	}

	if s.Git != nil && s.Local != nil {
		return fmt.Errorf("cannot specify both git and local source")
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
