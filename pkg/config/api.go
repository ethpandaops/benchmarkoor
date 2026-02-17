package config

// APIConfig contains all API server configuration.
type APIConfig struct {
	Server   APIServerConfig   `yaml:"server" mapstructure:"server"`
	Auth     APIAuthConfig     `yaml:"auth" mapstructure:"auth"`
	Database APIDatabaseConfig `yaml:"database" mapstructure:"database"`
	Storage  APIStorageConfig  `yaml:"storage,omitempty" mapstructure:"storage"`
}

// APIStorageConfig contains storage backend settings for serving files.
type APIStorageConfig struct {
	S3 *APIS3Config `yaml:"s3,omitempty" mapstructure:"s3"`
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
	Listen      string          `yaml:"listen" mapstructure:"listen"`
	CORSOrigins []string        `yaml:"cors_origins,omitempty" mapstructure:"cors_origins"`
	RateLimit   RateLimitConfig `yaml:"rate_limit,omitempty" mapstructure:"rate_limit"`
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
