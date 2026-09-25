package config

import "time"

// APIConfig contains all API server configuration.
type APIConfig struct {
	Server   APIServerConfig    `yaml:"server" mapstructure:"server"`
	Auth     APIAuthConfig      `yaml:"auth" mapstructure:"auth"`
	Database APIDatabaseConfig  `yaml:"database" mapstructure:"database"`
	Storage  APIStorageConfig   `yaml:"storage,omitempty" mapstructure:"storage"`
	Indexing *APIIndexingConfig `yaml:"indexing,omitempty" mapstructure:"indexing"`
	Ingest   *APIIngestConfig   `yaml:"ingest,omitempty" mapstructure:"ingest"`
}

// APIIngestConfig configures the live-run ingest endpoint that accepts
// periodic status reports from benchmarkoor runners. When Token is set,
// runs can post to POST /api/v1/ingest/runs using that bearer token.
type APIIngestConfig struct {
	Token          string `yaml:"token" mapstructure:"token"`
	StaleThreshold string `yaml:"stale_threshold,omitempty" mapstructure:"stale_threshold"` // default 5m
}

// DefaultAPIIngestStaleThreshold is the default time after which a live
// run with no recent reports is purged from the live_runs table.
const DefaultAPIIngestStaleThreshold = "5m"

// GetStaleThreshold returns the parsed stale threshold or the default.
func (c *APIIngestConfig) GetStaleThreshold() time.Duration {
	if c == nil || c.StaleThreshold == "" {
		d, _ := time.ParseDuration(DefaultAPIIngestStaleThreshold)
		return d
	}

	d, err := time.ParseDuration(c.StaleThreshold)
	if err != nil {
		d, _ := time.ParseDuration(DefaultAPIIngestStaleThreshold)
		return d
	}

	return d
}

// APIIndexingConfig configures the background indexing service that
// scans storage backends and maintains a queryable index in a database.
type APIIndexingConfig struct {
	Enabled     bool              `yaml:"enabled" mapstructure:"enabled"`
	Interval    string            `yaml:"interval,omitempty" mapstructure:"interval"`
	Concurrency int               `yaml:"concurrency,omitempty" mapstructure:"concurrency"`
	Database    APIDatabaseConfig `yaml:"database" mapstructure:"database"`

	// FailureGracePeriod is how old a run must be before the indexer records
	// it as a failure. A run that is still uploading has no config.json yet,
	// and must not be marked broken for that. Default 6h.
	FailureGracePeriod string `yaml:"failure_grace_period,omitempty" mapstructure:"failure_grace_period"`

	// FailureRetryInterval is how long the indexer skips a recorded failure
	// before trying it again. It keeps thousands of broken runs from costing
	// a storage round-trip every pass, while still letting a run whose upload
	// finished late heal on its own. Default 24h.
	FailureRetryInterval string `yaml:"failure_retry_interval,omitempty" mapstructure:"failure_retry_interval"`
}

const (
	// DefaultAPIIndexingFailureGracePeriod is the default age a run must
	// reach before a failed index is recorded against it.
	DefaultAPIIndexingFailureGracePeriod = "6h"

	// DefaultAPIIndexingFailureRetryInterval is the default time a recorded
	// failure is skipped for before the indexer retries it.
	DefaultAPIIndexingFailureRetryInterval = "24h"
)

// GetFailureGracePeriod returns the parsed grace period or the default.
func (c *APIIndexingConfig) GetFailureGracePeriod() time.Duration {
	return parseDurationOr(
		c.failureGracePeriod(), DefaultAPIIndexingFailureGracePeriod,
	)
}

// GetFailureRetryInterval returns the parsed retry interval or the default.
func (c *APIIndexingConfig) GetFailureRetryInterval() time.Duration {
	return parseDurationOr(
		c.failureRetryInterval(), DefaultAPIIndexingFailureRetryInterval,
	)
}

func (c *APIIndexingConfig) failureGracePeriod() string {
	if c == nil {
		return ""
	}

	return c.FailureGracePeriod
}

func (c *APIIndexingConfig) failureRetryInterval() string {
	if c == nil {
		return ""
	}

	return c.FailureRetryInterval
}

// parseDurationOr parses value, falling back to fallback when value is empty
// or malformed. The fallback is a constant in this package, so it always
// parses.
func parseDurationOr(value, fallback string) time.Duration {
	if d, err := time.ParseDuration(value); err == nil && d > 0 {
		return d
	}

	d, _ := time.ParseDuration(fallback)

	return d
}

// APIStorageConfig contains storage backend settings for serving files.
// Only one backend (S3 or local) may be enabled at a time.
type APIStorageConfig struct {
	S3    *APIS3Config           `yaml:"s3,omitempty" mapstructure:"s3"`
	Local *APILocalStorageConfig `yaml:"local,omitempty" mapstructure:"local"`
}

// APILocalStorageConfig serves benchmark result files directly from the
// local filesystem. Each discovery path maps a URL prefix name to an
// absolute directory that may contain an index.json and run/suite
// sub-directories. The map key serves as a URL path prefix, identical
// to how S3 discovery paths work.
type APILocalStorageConfig struct {
	Enabled        bool              `yaml:"enabled" mapstructure:"enabled"`
	DiscoveryPaths map[string]string `yaml:"discovery_paths,omitempty" mapstructure:"discovery_paths"`
}

// APIS3Config contains S3 settings for presigned URL generation.
type APIS3Config struct {
	Enabled         bool                    `yaml:"enabled" mapstructure:"enabled"`
	EndpointURL     string                  `yaml:"endpoint_url,omitempty" mapstructure:"endpoint_url"`
	Region          string                  `yaml:"region,omitempty" mapstructure:"region"`
	Bucket          string                  `yaml:"bucket" mapstructure:"bucket"`
	AccessKeyID     string                  `yaml:"access_key_id,omitempty" mapstructure:"access_key_id"`
	SecretAccessKey string                  `yaml:"secret_access_key,omitempty" mapstructure:"secret_access_key"`
	ForcePathStyle  bool                    `yaml:"force_path_style" mapstructure:"force_path_style"`
	PresignedURLs   APIS3PresignedURLConfig `yaml:"presigned_urls,omitempty" mapstructure:"presigned_urls"`
	DiscoveryPaths  []string                `yaml:"discovery_paths,omitempty" mapstructure:"discovery_paths"`
}

// APIS3PresignedURLConfig contains presigned URL generation settings.
type APIS3PresignedURLConfig struct {
	Expiry string `yaml:"expiry,omitempty" mapstructure:"expiry"`
}

// APIServerConfig contains HTTP server settings.
type APIServerConfig struct {
	Listen         string          `yaml:"listen" mapstructure:"listen"`
	CORSOrigins    []string        `yaml:"cors_origins,omitempty" mapstructure:"cors_origins"`
	RateLimit      RateLimitConfig `yaml:"rate_limit,omitempty" mapstructure:"rate_limit"`
	TrustedProxies []string        `yaml:"trusted_proxies,omitempty" mapstructure:"trusted_proxies"`
}

// RateLimitConfig configures per-IP rate limiting.
type RateLimitConfig struct {
	Enabled       bool          `yaml:"enabled" mapstructure:"enabled"`
	Auth          RateLimitTier `yaml:"auth,omitempty" mapstructure:"auth"`
	Public        RateLimitTier `yaml:"public,omitempty" mapstructure:"public"`
	Authenticated RateLimitTier `yaml:"authenticated,omitempty" mapstructure:"authenticated"`
}

// RateLimitTier defines request limits for a specific tier.
type RateLimitTier struct {
	RequestsPerMinute int `yaml:"requests_per_minute" mapstructure:"requests_per_minute"`
}

// APIAuthConfig contains authentication settings.
type APIAuthConfig struct {
	SessionTTL    string           `yaml:"session_ttl" mapstructure:"session_ttl"`
	AnonymousRead bool             `yaml:"anonymous_read" mapstructure:"anonymous_read"`
	Basic         BasicAuthConfig  `yaml:"basic,omitempty" mapstructure:"basic"`
	GitHub        GitHubAuthConfig `yaml:"github,omitempty" mapstructure:"github"`
}

// BasicAuthConfig configures username/password authentication.
type BasicAuthConfig struct {
	Enabled bool            `yaml:"enabled" mapstructure:"enabled"`
	Users   []BasicAuthUser `yaml:"users,omitempty" mapstructure:"users"`
}

// BasicAuthUser defines a basic auth user from config.
type BasicAuthUser struct {
	Username string `yaml:"username" mapstructure:"username"`
	Password string `yaml:"password" mapstructure:"password"`
	Role     string `yaml:"role" mapstructure:"role"`
}

// GitHubAuthConfig configures GitHub OAuth authentication.
type GitHubAuthConfig struct {
	Enabled         bool              `yaml:"enabled" mapstructure:"enabled"`
	ClientID        string            `yaml:"client_id,omitempty" mapstructure:"client_id"`
	ClientSecret    string            `yaml:"client_secret,omitempty" mapstructure:"client_secret"`
	RedirectURL     string            `yaml:"redirect_url,omitempty" mapstructure:"redirect_url"`
	OrgRoleMapping  map[string]string `yaml:"org_role_mapping,omitempty" mapstructure:"org_role_mapping"`
	UserRoleMapping map[string]string `yaml:"user_role_mapping,omitempty" mapstructure:"user_role_mapping"`
}

// APIDatabaseConfig contains database connection settings.
type APIDatabaseConfig struct {
	Driver   string               `yaml:"driver" mapstructure:"driver"`
	SQLite   SQLiteDatabaseConfig `yaml:"sqlite,omitempty" mapstructure:"sqlite"`
	Postgres PostgresConfig       `yaml:"postgres,omitempty" mapstructure:"postgres"`
}

// SQLiteDatabaseConfig contains SQLite-specific settings.
type SQLiteDatabaseConfig struct {
	Path string `yaml:"path" mapstructure:"path"`
}

// PostgresConfig contains PostgreSQL connection settings.
type PostgresConfig struct {
	Host     string `yaml:"host" mapstructure:"host"`
	Port     int    `yaml:"port" mapstructure:"port"`
	User     string `yaml:"user" mapstructure:"user"`
	Password string `yaml:"password" mapstructure:"password"`
	Database string `yaml:"database" mapstructure:"database"`
	SSLMode  string `yaml:"ssl_mode,omitempty" mapstructure:"ssl_mode"`
}
