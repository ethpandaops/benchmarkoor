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
	LogLevel           string `yaml:"log_level"`
	ClientLogsToStdout bool   `yaml:"client_logs_to_stdout"`
	DockerNetwork      string `yaml:"docker_network"`
	CleanupOnStart     bool   `yaml:"cleanup_on_start"`
}

// BenchmarkConfig contains benchmark-specific settings.
type BenchmarkConfig struct {
	ResultsDir string `yaml:"results_dir"`
}

// ClientConfig contains client configuration settings.
type ClientConfig struct {
	Config    ClientDefaults   `yaml:"config"`
	Instances []ClientInstance `yaml:"instances"`
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
	PullPolicy  string            `yaml:"pull_policy,omitempty"`
	Restart     string            `yaml:"restart,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Genesis     string            `yaml:"genesis,omitempty"`
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

	for i := range c.Client.Instances {
		if c.Client.Instances[i].PullPolicy == "" {
			c.Client.Instances[i].PullPolicy = DefaultPullPolicy
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
	}

	if c.Benchmark.ResultsDir != "" {
		dir := filepath.Dir(c.Benchmark.ResultsDir)
		if dir != "." && dir != ".." {
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				return fmt.Errorf("results directory parent %q does not exist", dir)
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
