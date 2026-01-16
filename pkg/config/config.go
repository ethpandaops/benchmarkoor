package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
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
	Global    GlobalConfig    `yaml:"global"`
	Benchmark BenchmarkConfig `yaml:"benchmark"`
	Client    ClientConfig    `yaml:"client"`
}

// GlobalConfig contains global application settings.
type GlobalConfig struct {
	LogLevel           string            `yaml:"log_level"`
	ClientLogsToStdout bool              `yaml:"client_logs_to_stdout"`
	DockerNetwork      string            `yaml:"docker_network"`
	CleanupOnStart     bool              `yaml:"cleanup_on_start"`
	Directories        DirectoriesConfig `yaml:"directories,omitempty"`
}

// DirectoriesConfig contains directory path configurations.
type DirectoriesConfig struct {
	// TmpDataDir is the directory for temporary datadir copies.
	// If empty, uses the system default temp directory.
	TmpDataDir string `yaml:"tmp_datadir,omitempty"`
	// TmpCacheDir is the directory for executor cache (git clones, etc).
	// If empty, uses ~/.cache/benchmarkoor.
	TmpCacheDir string `yaml:"tmp_cachedir,omitempty"`
}

// BenchmarkConfig contains benchmark-specific settings.
type BenchmarkConfig struct {
	ResultsDir           string      `yaml:"results_dir"`
	GenerateResultsIndex bool        `yaml:"generate_results_index"`
	Tests                TestsConfig `yaml:"tests,omitempty"`
}

// TestsConfig contains test execution settings.
type TestsConfig struct {
	Filter string       `yaml:"filter,omitempty"`
	Source SourceConfig `yaml:"source,omitempty"`
}

// SourceConfig defines where to find test files.
type SourceConfig struct {
	// Local directory options.
	TestsLocalDir       string `yaml:"tests_local_dir,omitempty"`
	WarmupTestsLocalDir string `yaml:"warmup_tests_local_dir,omitempty"`

	// Git repository options.
	TestsGit  *GitSource `yaml:"tests_git,omitempty"`
	WarmupGit *GitSource `yaml:"warmup_git,omitempty"`
}

// GitSource defines a git repository source for tests.
type GitSource struct {
	Repo      string `yaml:"repo"`
	Version   string `yaml:"version"`
	Directory string `yaml:"directory"`
}

// IsConfigured returns true if any test source is configured.
func (s *SourceConfig) IsConfigured() bool {
	return s.TestsLocalDir != "" || s.TestsGit != nil
}

// DefaultContainerDir is the default container mount path for data directories.
const DefaultContainerDir = "/data"

// DataDirConfig configures a pre-populated data directory for a client.
type DataDirConfig struct {
	SourceDir    string `yaml:"source_dir" json:"source_dir"`
	ContainerDir string `yaml:"container_dir,omitempty" json:"container_dir,omitempty"`
	Method       string `yaml:"method,omitempty" json:"method,omitempty"`
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

	if d.Method != "" && d.Method != "copy" {
		return fmt.Errorf("%s: invalid method %q, must be \"copy\"", prefix, d.Method)
	}

	return nil
}

// ClientConfig contains client configuration settings.
type ClientConfig struct {
	Config    ClientDefaults            `yaml:"config"`
	DataDirs  map[string]*DataDirConfig `yaml:"datadirs,omitempty"`
	Instances []ClientInstance          `yaml:"instances"`
}

// ClientDefaults contains default settings for all clients.
type ClientDefaults struct {
	JWT     string            `yaml:"jwt"`
	Genesis map[string]string `yaml:"genesis"`
}

// ClientInstance defines a single client instance to benchmark.
type ClientInstance struct {
	ID          string            `yaml:"id"`
	Client      string            `yaml:"client"`
	Image       string            `yaml:"image,omitempty"`
	Entrypoint  []string          `yaml:"entrypoint,omitempty"`
	Command     []string          `yaml:"command,omitempty"`
	ExtraArgs   []string          `yaml:"extra_args,omitempty"`
	PullPolicy  string            `yaml:"pull_policy,omitempty"`
	Restart     string            `yaml:"restart,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Genesis     string            `yaml:"genesis,omitempty"`
	DataDir     *DataDirConfig    `yaml:"datadir,omitempty"`
}

// Load reads and parses a configuration file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.applyDefaults()

	return &cfg, nil
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

	hasLocal := s.TestsLocalDir != ""
	hasGit := s.TestsGit != nil

	if hasLocal && hasGit {
		return fmt.Errorf("cannot specify both tests_local_dir and tests_git")
	}

	if hasLocal {
		if _, err := os.Stat(s.TestsLocalDir); os.IsNotExist(err) {
			return fmt.Errorf("tests_local_dir %q does not exist", s.TestsLocalDir)
		}

		if s.WarmupTestsLocalDir != "" {
			if _, err := os.Stat(s.WarmupTestsLocalDir); os.IsNotExist(err) {
				return fmt.Errorf("warmup_tests_local_dir %q does not exist", s.WarmupTestsLocalDir)
			}
		}
	}

	if hasGit {
		if s.TestsGit.Repo == "" {
			return fmt.Errorf("tests_git.repo is required")
		}

		if s.TestsGit.Version == "" {
			return fmt.Errorf("tests_git.version is required")
		}

		if s.WarmupGit != nil {
			if s.WarmupGit.Repo == "" {
				return fmt.Errorf("warmup_git.repo is required")
			}

			if s.WarmupGit.Version == "" {
				return fmt.Errorf("warmup_git.version is required")
			}
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
	"nimbus-el":  {},
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
