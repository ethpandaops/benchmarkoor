package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/ethpandaops/benchmarkoor/pkg/cpufreq"
	"github.com/mitchellh/mapstructure"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/spf13/viper"
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
	Global GlobalConfig `yaml:"global" mapstructure:"global"`
	Runner RunnerConfig `yaml:"runner" mapstructure:"runner"`
	API    *APIConfig   `yaml:"api,omitempty" mapstructure:"api"`
}

// RunnerConfig contains all run-specific configuration settings.
type RunnerConfig struct {
	ContainerRuntime   string            `yaml:"container_runtime,omitempty" mapstructure:"container_runtime"`
	ClientLogsToStdout bool              `yaml:"client_logs_to_stdout" mapstructure:"client_logs_to_stdout"`
	DockerNetwork      string            `yaml:"docker_network" mapstructure:"docker_network"`
	CleanupOnStart     bool              `yaml:"cleanup_on_start" mapstructure:"cleanup_on_start"`
	Directories        DirectoriesConfig `yaml:"directories,omitempty" mapstructure:"directories"`
	DropCachesPath     string            `yaml:"drop_caches_path,omitempty" mapstructure:"drop_caches_path"`
	CPUSysfsPath       string            `yaml:"cpu_sysfs_path,omitempty" mapstructure:"cpu_sysfs_path"`
	GitHubToken        string            `yaml:"github_token,omitempty" mapstructure:"github_token"`
	Metadata           MetadataConfig    `yaml:"metadata,omitempty" mapstructure:"metadata"`
	Benchmark          BenchmarkConfig   `yaml:"benchmark" mapstructure:"benchmark"`
	Client             ClientConfig      `yaml:"client" mapstructure:"client"`
	Instances          []ClientInstance  `yaml:"instances" mapstructure:"instances"`
}

// MetadataConfig contains arbitrary metadata labels for a benchmark run.
type MetadataConfig struct {
	Labels map[string]string `yaml:"labels,omitempty" mapstructure:"labels" json:"labels,omitempty"`
}

// GlobalConfig contains global application settings.
type GlobalConfig struct {
	LogLevel string `yaml:"log_level" mapstructure:"log_level"`
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
	Filter   string         `yaml:"filter,omitempty" mapstructure:"filter"`
	Metadata MetadataConfig `yaml:"metadata,omitempty" mapstructure:"metadata"`
	Source   SourceConfig   `yaml:"source,omitempty" mapstructure:"source"`
}

// SourceConfig defines where to find test files.
type SourceConfig struct {
	// New unified source options.
	Git          *GitSourceV2        `yaml:"git,omitempty" mapstructure:"git"`
	Local        *LocalSourceV2      `yaml:"local,omitempty" mapstructure:"local"`
	EESTFixtures *EESTFixturesSource `yaml:"eest_fixtures,omitempty" mapstructure:"eest_fixtures"`
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
}

// UseArtifacts returns true if the source is configured to use GitHub Actions artifacts.
func (e *EESTFixturesSource) UseArtifacts() bool {
	return e.FixturesArtifactName != "" || e.GenesisArtifactName != ""
}

// UseLocalDir returns true if the source is configured to use local directories.
func (e *EESTFixturesSource) UseLocalDir() bool {
	return e.LocalFixturesDir != "" || e.LocalGenesisDir != ""
}

// UseLocalTarball returns true if the source is configured to use local tarballs.
func (e *EESTFixturesSource) UseLocalTarball() bool {
	return e.LocalFixturesTarball != "" || e.LocalGenesisTarball != ""
}

// validate checks the EEST fixtures source configuration for errors.
// Exactly one mode must be specified: release, artifact, local_dir, or local_tarball.
func (e *EESTFixturesSource) validate() error {
	hasRelease := e.GitHubRelease != ""
	hasArtifacts := e.UseArtifacts()
	hasLocalDir := e.UseLocalDir()
	hasLocalTarball := e.UseLocalTarball()

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

	if modeCount == 0 {
		return fmt.Errorf(
			"eest_fixtures: must specify one of: github_release, " +
				"fixtures_artifact_name, local_fixtures_dir/local_genesis_dir, " +
				"or local_fixtures_tarball/local_genesis_tarball",
		)
	}

	if modeCount > 1 {
		return fmt.Errorf(
			"eest_fixtures: cannot combine modes (release, artifact, " +
				"local_dir, local_tarball are mutually exclusive)",
		)
	}

	// Validate remote modes require github_repo.
	if (hasRelease || hasArtifacts) && e.GitHubRepo == "" {
		return fmt.Errorf("eest_fixtures.github_repo is required for release/artifact modes")
	}

	// Validate local dir mode.
	if hasLocalDir {
		if e.LocalFixturesDir == "" {
			return fmt.Errorf("eest_fixtures: local_fixtures_dir is required when local_genesis_dir is set")
		}

		if e.LocalGenesisDir == "" {
			return fmt.Errorf("eest_fixtures: local_genesis_dir is required when local_fixtures_dir is set")
		}

		if err := validateDirExists(e.LocalFixturesDir, "eest_fixtures.local_fixtures_dir"); err != nil {
			return err
		}

		if err := validateDirExists(e.LocalGenesisDir, "eest_fixtures.local_genesis_dir"); err != nil {
			return err
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

// RetryNewPayloadsSyncingConfig configures retry behavior when engine_newPayload returns SYNCING.
type RetryNewPayloadsSyncingConfig struct {
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
	WaitAfterRPCReady                string                            `yaml:"wait_after_rpc_ready,omitempty" mapstructure:"wait_after_rpc_ready"`
	PostTestRPCCalls                 []PostTestRPCCall                 `yaml:"post_test_rpc_calls,omitempty" mapstructure:"post_test_rpc_calls"`
	BootstrapFCU                     *BootstrapFCUConfig               `yaml:"bootstrap_fcu,omitempty" mapstructure:"bootstrap_fcu"`
	CheckpointRestoreStrategyOptions *CheckpointRestoreStrategyOptions `yaml:"checkpoint_restore_strategy_options,omitempty" mapstructure:"checkpoint_restore_strategy_options"`
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
	DataDir                          *DataDirConfig                    `yaml:"datadir,omitempty" mapstructure:"datadir"`
	DropMemoryCaches                 string                            `yaml:"drop_memory_caches,omitempty" mapstructure:"drop_memory_caches"`
	RollbackStrategy                 string                            `yaml:"rollback_strategy,omitempty" mapstructure:"rollback_strategy"`
	ResourceLimits                   *ResourceLimits                   `yaml:"resource_limits,omitempty" mapstructure:"resource_limits"`
	RetryNewPayloadsSyncingState     *RetryNewPayloadsSyncingConfig    `yaml:"retry_new_payloads_syncing_state,omitempty" mapstructure:"retry_new_payloads_syncing_state"`
	WaitAfterRPCReady                string                            `yaml:"wait_after_rpc_ready,omitempty" mapstructure:"wait_after_rpc_ready"`
	PostTestRPCCalls                 []PostTestRPCCall                 `yaml:"post_test_rpc_calls,omitempty" mapstructure:"post_test_rpc_calls"`
	BootstrapFCU                     *BootstrapFCUConfig               `yaml:"bootstrap_fcu,omitempty" mapstructure:"bootstrap_fcu"`
	CheckpointRestoreStrategyOptions *CheckpointRestoreStrategyOptions `yaml:"checkpoint_restore_strategy_options,omitempty" mapstructure:"checkpoint_restore_strategy_options"`
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

	// Load and merge configs in order, collecting expanded YAML for
	// post-processing (Viper lowercases map keys, so we re-parse to
	// restore original casing for environment variables).
	rawYAMLs := make([]string, 0, len(paths))

	for i, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading config file %q: %w", path, err)
		}

		expanded := os.Expand(string(content), expandEnvWithDefaults)
		rawYAMLs = append(rawYAMLs, expanded)

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

	cfg.applyDefaults()

	return &cfg, nil
}

// bindEnvKeys explicitly binds configuration keys to environment variables.
// This is required for Viper to recognize env vars for keys not present in the config file.
func bindEnvKeys(v *viper.Viper) {
	keys := []string{
		// Global settings
		"global.log_level",
		// Runner settings
		"runner.container_runtime",
		"runner.client_logs_to_stdout",
		"runner.docker_network",
		"runner.cleanup_on_start",
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

	if c.Runner.DockerNetwork == "" {
		c.Runner.DockerNetwork = DefaultDockerNetwork
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

	// Validate wait_after_rpc_ready settings.
	if err := c.validateWaitAfterRPCReady(); err != nil {
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

	// Validate results_upload settings.
	if err := c.validateResultsUpload(); err != nil {
		return err
	}

	// Validate API settings.
	if err := c.ValidateAPI(); err != nil {
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

	return nil
}

// validateAPIStorage validates the API storage configuration.
func (c *Config) validateAPIStorage() error {
	if c.API.Storage.S3 == nil || !c.API.Storage.S3.Enabled {
		return nil
	}

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
