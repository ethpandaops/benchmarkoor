# API Server

The optional `api` section configures a standalone API server for authentication and user management. The API server is started separately from the benchmark runner using the `benchmarkoor api` subcommand.

```bash
benchmarkoor api --config config.yaml
```

When the `api` section is absent from the config, the API server cannot be started. The UI works without the API — it only integrates with the API when `api` is defined in the UI's `config.json`.

## Table of Contents

- [Server Settings](#server-settings)
- [Authentication](#authentication)
- [Database](#database)
- [Storage](#storage)
- [Indexing](#indexing)
- [API Endpoints](#api-endpoints)
- [Environment Variable Overrides](#environment-variable-overrides)
- [UI Integration](#ui-integration)
- [Example](#example)

## Server Settings

```yaml
api:
  server:
    listen: ":9090"
    cors_origins:
      - http://localhost:5173
      - https://benchmarkoor.example.com
    rate_limit:
      enabled: true
      auth:
        requests_per_minute: 10
      public:
        requests_per_minute: 60
      authenticated:
        requests_per_minute: 120
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `listen` | string | `:9090` | Address and port the API server listens on |
| `cors_origins` | []string | `["*"]` | Allowed CORS origins. When using cookies (`credentials: 'include'`), wildcard `*` is not allowed — list specific origins |
| `rate_limit.enabled` | bool | `false` | Enable per-IP rate limiting |
| `rate_limit.auth.requests_per_minute` | int | `10` | Rate limit for auth endpoints (login/logout) |
| `rate_limit.public.requests_per_minute` | int | `60` | Rate limit for public endpoints (health/config) |
| `rate_limit.authenticated.requests_per_minute` | int | `120` | Rate limit for authenticated endpoints (admin) |

## Authentication

At least one authentication provider must be enabled. Two providers are supported: basic (username/password) and GitHub OAuth. Both can be enabled simultaneously.

### General Auth Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `auth.session_ttl` | string | `24h` | Session duration as a Go duration string (e.g., `24h`, `12h`, `30m`) |
| `auth.anonymous_read` | bool | `false` | Allow unauthenticated access to `/files/` endpoints. When `true`, the UI allows browsing without login. When `false`, users must sign in to access file data and the UI redirects to the login page |

Sessions are stored in the database and cleaned up automatically every 15 minutes.

### Basic Authentication

```yaml
api:
  auth:
    basic:
      enabled: true
      users:
        - username: admin
          password: ${ADMIN_PASSWORD}
          role: admin
        - username: viewer
          password: ${VIEWER_PASSWORD}
          role: readonly
```

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `enabled` | bool | Yes | Enable basic authentication |
| `users` | []object | When enabled | List of users |
| `users[].username` | string | Yes | Username (must be unique) |
| `users[].password` | string | Yes | Plaintext password (hashed with bcrypt on startup) |
| `users[].role` | string | Yes | User role: `admin` or `readonly` |

Config-sourced users are seeded into the database on startup. Only users with `source="config"` are updated; users created via the admin API or GitHub OAuth are preserved.

### GitHub OAuth

```yaml
api:
  auth:
    github:
      enabled: true
      client_id: ${GITHUB_CLIENT_ID}
      client_secret: ${GITHUB_CLIENT_SECRET}
      redirect_url: http://localhost:9090/api/v1/auth/github/callback
      org_role_mapping:
        my-org: admin
        another-org: readonly
      user_role_mapping:
        specific-user: admin
```

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `enabled` | bool | Yes | Enable GitHub OAuth |
| `client_id` | string | When enabled | GitHub OAuth App client ID |
| `client_secret` | string | When enabled | GitHub OAuth App client secret |
| `redirect_url` | string | When enabled | OAuth callback URL (must match the GitHub App configuration) |
| `org_role_mapping` | map[string]string | No | Map GitHub organization names to roles |
| `user_role_mapping` | map[string]string | No | Map GitHub usernames to roles (takes precedence over org mapping) |

**Role resolution order:**
1. User-level mapping is checked first (exact username match)
2. Org-level mapping is checked next (highest privilege wins — `admin` > `readonly`)
3. If no mapping matches, the user is rejected

**Setting up a GitHub OAuth App:**
1. Go to GitHub Settings > Developer settings > OAuth Apps > New OAuth App
2. Set the "Authorization callback URL" to your `redirect_url` value
3. Note the Client ID and generate a Client Secret

### Roles

| Role | Permissions |
|------|-------------|
| `admin` | Full access: view data, manage users, manage GitHub mappings |
| `readonly` | View access only |

## Database

The API server uses a database for storing users, sessions, and GitHub role mappings. Two drivers are supported.

### SQLite (default)

```yaml
api:
  database:
    driver: sqlite
    sqlite:
      path: benchmarkoor.db
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `driver` | string | `sqlite` | Database driver |
| `sqlite.path` | string | `benchmarkoor.db` | Path to the SQLite database file |

### PostgreSQL

```yaml
api:
  database:
    driver: postgres
    postgres:
      host: localhost
      port: 5432
      user: benchmarkoor
      password: ${DB_PASSWORD}
      database: benchmarkoor
      ssl_mode: disable
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `driver` | string | `sqlite` | Database driver (`sqlite` or `postgres`) |
| `postgres.host` | string | Required | PostgreSQL host |
| `postgres.port` | int | `5432` | PostgreSQL port |
| `postgres.user` | string | Required | Database user |
| `postgres.password` | string | - | Database password |
| `postgres.database` | string | Required | Database name |
| `postgres.ssl_mode` | string | `disable` | SSL mode: `disable`, `require`, `verify-ca`, `verify-full` |

## Storage

The optional `api.storage` section configures a storage backend for serving benchmark result files via the `/api/v1/files/*` endpoint. Two backends are available — **S3** (presigned URLs) and **local** (direct filesystem serving). Only one backend may be enabled at a time.

Both backends share the concept of **discovery paths**: a list of roots that the UI can browse. Each discovery path should contain an `index.json` and the run/suite sub-directories it references.

### S3 Storage

S3 storage serves files via presigned GET URLs. This is **separate** from `runner.benchmark.results_upload.s3` (which handles uploads during benchmark runs). The API generates presigned URLs so the UI can fetch files directly from S3.

```yaml
api:
  storage:
    s3:
      enabled: true
      endpoint_url: https://s3.us-east-1.amazonaws.com
      region: us-east-1
      bucket: my-benchmark-results
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_SECRET_ACCESS_KEY}
      force_path_style: false
      presigned_urls:
        expiry: 1h
      discovery_paths:
        - results
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `enabled` | bool | Yes | `false` | Enable S3 presigned URL generation |
| `bucket` | string | When enabled | - | S3 bucket name |
| `endpoint_url` | string | No | AWS default | S3 endpoint URL (scheme + host only) |
| `region` | string | No | `us-east-1` | AWS region |
| `access_key_id` | string | No | - | Static AWS access key ID |
| `secret_access_key` | string | No | - | Static AWS secret access key |
| `force_path_style` | bool | No | `false` | Use path-style addressing (required for MinIO/R2) |
| `presigned_urls.expiry` | string | No | `1h` | How long presigned URLs remain valid (Go duration string) |
| `discovery_paths` | []string | When enabled | - | S3 key prefixes the UI can browse. At least one is required. Must not contain `..` |

**How S3 mode works:**

1. The `GET /api/v1/config` endpoint advertises which `discovery_paths` are available and that S3 storage is enabled.
2. The UI uses this to know where to look for `index.json` files in S3.
3. When the UI needs a file, it requests `GET /api/v1/files/{key}` (e.g., `GET /api/v1/files/results/index.json`).
4. The API validates the requested key is under an allowed discovery path, then returns a presigned S3 GET URL.
5. The UI fetches the file directly from S3 using the presigned URL.

### Local Storage

Local storage serves files directly from the local filesystem using `http.ServeFile`. This enables running the API without any S3 infrastructure — files are served through the same `/api/v1/files/*` route with correct Content-Type, range request support, and caching headers handled automatically.

```yaml
api:
  storage:
    local:
      enabled: true
      discovery_paths:
        results: /data/benchmarkoor/results
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `enabled` | bool | Yes | `false` | Enable local file serving |
| `discovery_paths` | map[string]string | When enabled | - | Named prefixes mapping URL path segments to absolute directories. Keys become URL prefixes (must not contain `/` or `..`). Values must be absolute paths and must not contain `..`. At least one entry is required. |

**How local mode works:**

1. The `GET /api/v1/config` endpoint advertises the discovery path names (map keys, sorted) and that local storage is enabled.
2. The UI iterates over each discovery path name, fetching `{name}/index.json` from the API (with auth credentials) — identical to how S3 mode works.
3. When the UI needs a file, it requests `GET /api/v1/files/{name}/{relative_path}` (e.g., `GET /api/v1/files/results/runs/abc/results.json`).
4. The API extracts the first path segment as the prefix name, looks up the corresponding directory, resolves the file on disk, and serves it directly.
5. No presigned URL indirection — the API streams the file content in the response.

### Path Validation

Requested file paths are validated before serving (both S3 and local backends):
- The path must be non-empty and clean (no `..`, no trailing slashes)
- The path must fall under one of the configured `discovery_paths` prefixes
- Partial prefix matches are rejected (e.g., `results_backup/file` does not match prefix `results`)
- For local storage, an additional defense-in-depth check ensures the resolved absolute path stays under the discovery root

## Indexing

The optional `api.indexing` section enables a background indexing service that periodically scans the configured storage backend and maintains a queryable index in a separate database. This replaces the need to manually generate `index.json` and `stats.json` files via CLI commands.

When enabled, the indexer runs an initial pass on startup and then re-scans at the configured interval. Indexing is **incremental** — only new runs and runs that were previously incomplete (no `result.json` at last index time, non-terminal status) are processed. Runs are indexed in parallel using a bounded worker pool.

```yaml
api:
  indexing:
    enabled: true
    interval: "60s"
    concurrency: 4
    database:
      driver: sqlite
      sqlite:
        path: benchmarkoor-index.db
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable the background indexing service |
| `interval` | string | `60s` | How often to re-scan storage for new/updated runs (Go duration string) |
| `concurrency` | int | `4` | Number of runs to index in parallel. Higher values speed up indexing but increase I/O and memory usage. Set to `1` for sequential processing |
| `database.driver` | string | Required | Database driver (`sqlite` or `postgres`). This is a **separate** database from the auth database |
| `database.sqlite.path` | string | When driver=sqlite | Path to the index SQLite database file |
| `database.postgres.*` | - | When driver=postgres | PostgreSQL connection settings (same schema as the [auth database](#postgresql)) |

**Requirements:**
- At least one [storage backend](#storage) (S3 or local) must be configured
- The indexing database is separate from the auth database — use a different file path or database name

**How it works:**

1. On startup, the API server prepares the index database and storage reader, then starts the HTTP server.
2. After the HTTP server is listening, the background indexer starts its first pass asynchronously.
3. Each pass iterates over configured discovery paths and lists all run IDs from storage.
4. New and incomplete runs are indexed in parallel (bounded by `concurrency`):
   - `config.json` and `result.json` are read concurrently per run
   - An index entry is built and upserted into the database
   - If `result.json` is present, per-test durations are bulk-inserted
5. The UI queries the index via dedicated API endpoints instead of reading raw JSON files.

**When to use indexing:**
- You have many runs and generating `index.json` / `stats.json` via CLI is slow
- You want the UI to always show up-to-date data without manual regeneration
- You are running the API server as a long-lived service

## API Endpoints

All endpoints are under the `/api/v1` prefix.

### Public

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check (`{"status":"ok"}`) |
| `GET` | `/config` | Public configuration (auth providers, `anonymous_read`, storage settings, indexing status) |

### Authentication

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/auth/login` | Login with username/password |
| `POST` | `/auth/logout` | Destroy current session |
| `GET` | `/auth/me` | Get current user (requires auth) |
| `GET` | `/auth/github` | Initiate GitHub OAuth flow |
| `GET` | `/auth/github/callback` | GitHub OAuth callback |

### Admin (requires `admin` role)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/users` | List all users |
| `POST` | `/admin/users` | Create a user |
| `PUT` | `/admin/users/{id}` | Update a user |
| `DELETE` | `/admin/users/{id}` | Delete a user |
| `GET` | `/admin/sessions` | List all active sessions |
| `DELETE` | `/admin/sessions/{id}` | Revoke a session |
| `GET` | `/admin/github/org-mappings` | List org role mappings |
| `POST` | `/admin/github/org-mappings` | Create/update org mapping |
| `DELETE` | `/admin/github/org-mappings/{id}` | Delete org mapping |
| `GET` | `/admin/github/user-mappings` | List user role mappings |
| `POST` | `/admin/github/user-mappings` | Create/update user mapping |
| `DELETE` | `/admin/github/user-mappings/{id}` | Delete user mapping |

### Index (requires authentication unless `anonymous_read` is enabled)

Available only when [indexing](#indexing) is enabled.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/index` | List all indexed runs across all discovery paths. Returns the same shape as `index.json` with an additional `discovery_path` field per entry. Sorted by timestamp descending |
| `GET` | `/index/suites/{hash}/stats` | Per-test duration statistics for a suite. Returns the same shape as `stats.json`. Durations are sorted by `time_ns` descending |

### Files (requires authentication unless `anonymous_read` is enabled)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/files/*` | Serve a file from the configured storage backend. With S3, returns `{"url":"..."}` (presigned URL). With local storage, streams the file content directly. Requires [storage](#storage) to be configured. Requires authentication unless `auth.anonymous_read` is `true` |

## Environment Variable Overrides

API configuration values can be overridden via environment variables with the `BENCHMARKOOR_` prefix:

| Config Path | Environment Variable |
|-------------|---------------------|
| `api.server.listen` | `BENCHMARKOOR_API_SERVER_LISTEN` |
| `api.auth.session_ttl` | `BENCHMARKOOR_API_AUTH_SESSION_TTL` |
| `api.auth.github.client_id` | `BENCHMARKOOR_API_AUTH_GITHUB_CLIENT_ID` |
| `api.auth.github.client_secret` | `BENCHMARKOOR_API_AUTH_GITHUB_CLIENT_SECRET` |
| `api.database.driver` | `BENCHMARKOOR_API_DATABASE_DRIVER` |
| `api.database.postgres.host` | `BENCHMARKOOR_API_DATABASE_POSTGRES_HOST` |
| `api.database.postgres.password` | `BENCHMARKOOR_API_DATABASE_POSTGRES_PASSWORD` |
| `api.storage.s3.enabled` | `BENCHMARKOOR_API_STORAGE_S3_ENABLED` |
| `api.storage.s3.bucket` | `BENCHMARKOOR_API_STORAGE_S3_BUCKET` |
| `api.storage.s3.access_key_id` | `BENCHMARKOOR_API_STORAGE_S3_ACCESS_KEY_ID` |
| `api.storage.s3.secret_access_key` | `BENCHMARKOOR_API_STORAGE_S3_SECRET_ACCESS_KEY` |
| `api.storage.local.enabled` | `BENCHMARKOOR_API_STORAGE_LOCAL_ENABLED` |
| `api.indexing.enabled` | `BENCHMARKOOR_API_INDEXING_ENABLED` |
| `api.indexing.interval` | `BENCHMARKOOR_API_INDEXING_INTERVAL` |
| `api.indexing.concurrency` | `BENCHMARKOOR_API_INDEXING_CONCURRENCY` |
| `api.indexing.database.driver` | `BENCHMARKOOR_API_INDEXING_DATABASE_DRIVER` |
| `api.indexing.database.sqlite.path` | `BENCHMARKOOR_API_INDEXING_DATABASE_SQLITE_PATH` |

## UI Integration

The UI conditionally integrates with the API when `api` is defined in the UI's `config.json`. When no API is configured, the UI works exactly as before.

To enable API integration, add the `api` field to the UI's `config.json`:

```json
{
  "dataSource": "/results",
  "api": {
    "baseUrl": "http://localhost:9090"
  }
}
```

When the API is configured, the UI provides:
- **Login page** (`/login`) — username/password form and/or "Sign in with GitHub" button
- **Admin page** (`/admin`) — user management, session management, GitHub org/user role mapping management
- **Header controls** — sign in/out button, username display, admin link (for admins)

When indexing is enabled, the UI automatically detects this via the `/api/v1/config` endpoint and switches to querying the index API endpoints (`/api/v1/index` and `/api/v1/index/suites/{hash}/stats`) instead of reading raw JSON files from storage. This is transparent to the user.

When the API is not configured, none of these features appear and the UI functions as a static results viewer.

## Examples

### With S3 Storage

API server with basic auth, GitHub OAuth, S3 storage, and indexing:

```yaml
api:
  server:
    listen: ":9090"
    cors_origins:
      - https://benchmarkoor.example.com
    rate_limit:
      enabled: true
      auth:
        requests_per_minute: 10
      public:
        requests_per_minute: 60
      authenticated:
        requests_per_minute: 120
  auth:
    session_ttl: 24h
    anonymous_read: false  # Set to true to allow unauthenticated file access
    basic:
      enabled: true
      users:
        - username: admin
          password: ${ADMIN_PASSWORD}
          role: admin
    github:
      enabled: true
      client_id: ${GITHUB_CLIENT_ID}
      client_secret: ${GITHUB_CLIENT_SECRET}
      redirect_url: https://benchmarkoor.example.com/api/v1/auth/github/callback
      org_role_mapping:
        ethpandaops: admin
      user_role_mapping:
        specific-admin: admin
  database:
    driver: sqlite
    sqlite:
      path: /data/benchmarkoor.db
  storage:
    s3:
      enabled: true
      endpoint_url: https://s3.us-east-1.amazonaws.com
      region: us-east-1
      bucket: my-benchmark-results
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_SECRET_ACCESS_KEY}
      presigned_urls:
        expiry: 1h
      discovery_paths:
        - results
  indexing:
    enabled: true
    interval: "60s"
    concurrency: 8
    database:
      driver: sqlite
      sqlite:
        path: /data/benchmarkoor-index.db

# Minimal client config (required by config loader but not used by the API server).
client:
  instances:
    - id: placeholder
      client: geth
```

### With Local Storage

API server with basic auth, local filesystem storage, and indexing:

```yaml
api:
  server:
    listen: ":9090"
    cors_origins:
      - https://benchmarkoor.example.com
  auth:
    session_ttl: 24h
    anonymous_read: true
    basic:
      enabled: true
      users:
        - username: admin
          password: ${ADMIN_PASSWORD}
          role: admin
  database:
    driver: sqlite
    sqlite:
      path: /data/benchmarkoor.db
  storage:
    local:
      enabled: true
      discovery_paths:
        results: /data/benchmarkoor/results
  indexing:
    enabled: true
    interval: "60s"
    database:
      driver: sqlite
      sqlite:
        path: /data/benchmarkoor-index.db

# Minimal client config (required by config loader but not used by the API server).
client:
  instances:
    - id: placeholder
      client: geth
```
