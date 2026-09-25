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
    trusted_proxies:
      - 10.0.0.0/8
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `listen` | string | `:9090` | Address and port the API server listens on |
| `cors_origins` | []string | `["*"]` | Allowed CORS origins. When using cookies (`credentials: 'include'`), wildcard `*` is not allowed — list specific origins |
| `rate_limit.enabled` | bool | `false` | Enable per-IP rate limiting |
| `rate_limit.auth.requests_per_minute` | int | `10` | Rate limit for auth endpoints (login/logout) |
| `rate_limit.public.requests_per_minute` | int | `60` | Rate limit for public endpoints (health/config) |
| `rate_limit.authenticated.requests_per_minute` | int | `120` | Rate limit for authenticated endpoints (admin) |
| `trusted_proxies` | []string | none | IPs or CIDR ranges (e.g. `10.0.0.0/8`) of reverse proxies/load balancers in front of the API. When the direct connection comes from one of these, rate limiting keys on the client address taken from `X-Forwarded-For` instead of the connection's address. Leave unset if the API is reachable directly — an unset or empty list means `X-Forwarded-For` is never trusted, since honoring it from an arbitrary client lets every request claim a different IP and bypass rate limiting entirely |

**List every proxy in the chain.** `X-Forwarded-For` is read right-to-left and the first entry that is *not* itself a trusted proxy is used as the client address. With a chain (say a CDN in front of a load balancer), the right-most entries are hops your own infrastructure appended, so both the load balancer and the CDN egress ranges belong in `trusted_proxies` — otherwise every client is keyed on the CDN's address and shares a single rate limit bucket. Entries that don't parse as an IP or CIDR range are skipped with a warning at startup, and a hop that doesn't parse as an IP is never used as a rate limit key.

If the API sits behind a proxy and `trusted_proxies` is left unset, rate limiting keys every request on the proxy's address, so all clients behind it share one bucket. The server logs a warning once when it sees `X-Forwarded-For` on a request with no trusted proxies configured.

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
    interval: "10m"
    concurrency: 4
    failure_grace_period: "6h"
    failure_retry_interval: "24h"
    database:
      driver: sqlite
      sqlite:
        path: benchmarkoor-index.db
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable the background indexing service |
| `interval` | string | `10m` | How often to re-scan storage for new/updated runs (Go duration string) |
| `concurrency` | int | `4` | Number of runs to index in parallel. Higher values speed up indexing but increase I/O and memory usage. Set to `1` for sequential processing |
| `failure_grace_period` | string | `6h` | How old a run must be before a failed index is recorded against it. See [Failed runs](#failed-runs) |
| `failure_retry_interval` | string | `24h` | How long a recorded failure is skipped for before the indexer tries it again |
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

### Run deletion

Admins delete runs through `POST /admin/runs/delete`. The endpoint does not delete anything itself. It marks each run as queued for deletion and returns `202 Accepted` at once. A background worker in the API server then drains the queue:

1. The worker takes the runs in the order they were queued.
2. For each run, it deletes the files from storage (S3 or local) first.
3. Then it deletes the index rows (`test_stats`, `test_stats_block_logs`, `runs`, and the suite if no other run uses it) in one transaction.

The queue is stored in the index database, so it survives a restart of the API server. A run whose deletion fails keeps its place in the queue and is retried on the next pass (every 30 seconds). The last error is stored on the run.

While a run is queued, it stays visible:

- `GET /index` and `GET /index/query/runs` set `deletion_requested_at` (and `deletion_error` after a failed attempt) on the entry.
- `GET /admin/runs/deletion-queue` lists the queued runs in deletion order.
- The UI marks the run as queued for deletion and does not let you select it again.

Queuing a run that is already queued is a no-op. A run whose status is `running` is refused with a per-run error, because deleting it would race with the active runner. Deletion needs a storage backend that supports deletion (both S3 and local do).

To take a run out of the queue, call `POST /admin/runs/delete/cancel` with the same `run_ids` body. This clears the mark and the recorded error. Use it when a run fails to delete again and again, for example when its discovery path was removed from the storage config. The cancel is best-effort: a run the worker is deleting at that moment is still removed.

#### Deleting a whole suite

`POST /admin/runs/delete` also takes `suite_hashes`, which queues every run of the named suites. A suite can hold thousands of runs, so the server resolves them rather than making the client send every ID.

```json
{"suite_hashes": ["22c2404b9ce3f47c"]}
```

The same guard applies per run: a run that is still `running` is left alone, and the response says how many were skipped for that reason. Runs that were already queued are reported too, so a repeat request explains why it queued nothing rather than returning a bare `queued: 0`. Deleting the last run of a suite also removes the suite row, because `DeleteRunCascade` cleans up an orphaned suite.

At most 200 suites per request. Each one costs a transaction on the SQLite writer, which is a single connection, so an unbounded list would hold that connection — and the indexer behind it — for as long as the list.

In the UI this is the trash button on the **Suites** page. It turns on a checkbox per suite and a bar showing how many runs the selection will queue.

### Database report

`GET /admin/database` reports the size and contents of the index database, so an admin can tell whether a cleanup is due before the volume fills up. It backs the **Admin → Database** tab.

It returns:

- The database file size, its write-ahead log, and the page count.
- `free_pages * page_size`, which is what a `VACUUM` would hand back. Note that a `VACUUM` needs as much free space again as the database occupies, so it is not a way out of a nearly full volume.
- The size of the filesystem holding the database, what files occupy on it, and what is still available. Used and available add up to less than the total, because a filesystem reserves a slice for root: a usage share is `used/(used+available)`, the way `df` computes it. Dividing by the total instead would read the deployed volume as 88% full where `df` says 93%, and an empty ext4 volume as 5% full.
- A row count per table.
- The suites with the most runs, with the test-stat and block-log rows that deleting each would take with it. This pairs with deleting a suite's runs above.

Gathering the report counts every row in every table, which on a large database is seconds of work, so it is cached for five minutes and only one gather runs at a time. Without that, two open admin tabs would each start their own scan on a four-connection read pool and starve `/index` of readers. `gathered_at` says when the figures were taken.

Per-table byte sizes are **not** reported. The SQLite build used here has no `dbstat` virtual table, and an estimate would be worse than an honest omission.

### Pass history

The indexer writes a row when a pass ends, and `GET /admin/indexer/stats` returns the most recent ones, newest first. It backs the charts under **Admin → Indexer**, which is where a pass getting slower shows up before it gets slow enough to notice.

Each row carries how long the pass took, what started it (`startup`, `schedule` or `manual`), and what it did: runs added, incomplete runs read again, runs it could not index, and runs it skipped because their failure record is still muted. It also carries what storage held and how much of that the index already had, so the backlog a pass started with is visible. A pass a shutdown cut short has status `cancelled`, whether the shutdown landed between two discovery paths or inside one, and its counters cover only the work it got through. Runs whose reads the shutdown cancelled are left out of `runs_failed` and earn no failure record, because the shutdown says nothing about the run.

The table keeps the last 500 passes. A deployment on a one-minute interval writes a row a minute, so the oldest rows are pruned on every write.

A pass in flight has no row yet. The same endpoint reports it separately:

```json
{
  "running": true,
  "current_pass": {
    "started_at": "2026-09-25T18:20:00Z",
    "trigger": "manual",
    "elapsed_ms": 137000
  },
  "interval": "10m0s"
}
```

`elapsed_ms` is measured on the server, so a browser with a skewed clock still shows the right elapsed time. The UI uses it to say how long the pass has been going, and to leave the **Run Indexer** button disabled while it is — starting a second pass would only be refused with 409.

### Failed runs

A run directory that storage holds but the indexer cannot read leaves nothing behind on its own. Most of them never got their `config.json`, because the upload died part-way. Without a record the indexer re-reads every one of them on every pass: on one deployment that was 9779 storage round-trips and 9779 log lines per pass, and it was most of why a pass took 26 minutes.

The indexer therefore records these runs in an `index_failures` table.

1. A run that fails to index is only recorded once it is older than `failure_grace_period` (default 6h). Below that age a missing `config.json` means the upload is still in flight, not that the run is broken. A run's age comes from its ID, which is named `{timestamp}_{shortID}_{instance}` — the run has no `config.json`, so the ID is the only place a timestamp survives.
2. A recorded run is skipped by later passes until `failure_retry_interval` (default 24h) lapses. The pass does not read it from storage at all.
3. When the interval lapses the run is tried again. A run whose upload finally landed is indexed and loses its record, so a late upload heals without anyone intervening.
4. A record whose run has left storage is dropped on the next pass.

The first failure is logged at `warn`. Repeats drop to `debug`, because the record is the durable signal.

Admins see the records under **Admin → Indexer** in the UI, or through the API:

- `GET /admin/indexer/failures` lists them, newest run first, paged.
- `POST /admin/indexer/failures/delete` queues runs so their stored data is deleted. Pass `run_ids` for a chosen set or `all` for every record.
- `POST /admin/indexer/failures/delete/cancel` takes them back out of the queue.

Deletion uses the same background worker as run deletion. The files go from storage first; the record is dropped only once that succeeds. A record whose delete fails keeps its place in the queue with the reason attached, and is retried on the next pass.

## Ingest (live run reporting)

The optional `api.ingest` section enables an authenticated endpoint that benchmarkoor runners use to stream live run-status snapshots to the API. Live entries land in a separate `live_runs` table so they never interfere with the canonical `runs` table populated by the indexer; the UI merges both views.

```yaml
api:
  ingest:
    token: my-shared-secret
    stale_threshold: 5m
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `token` | string | - | Shared bearer token. Runners must send `Authorization: Bearer <token>`. The ingest endpoint is only registered when this is set |
| `stale_threshold` | string | `5m` | A live-run row is removed when no new report has arrived within this window (Go duration) |

A background goroutine on the API scans every 30s and deletes live rows whose `last_reported_at` is older than `stale_threshold`. When the on-disk indexer later picks up a real run with the same `(discovery_path, run_id)`, the live row is removed immediately so the UI doesn't show duplicate rows.

The endpoint caps the raw request body at 10 MiB, and the decompressed size of a gzip-encoded body at 50 MiB. A report exceeding either limit is rejected with `413 Payload Too Large`.

## API Endpoints

All endpoints are under the `/api/v1` prefix.

### Request body limits

| Endpoints | Limit |
|-----------|-------|
| `/auth/*`, `/admin/*` | 1 MiB |
| `/ingest/*` | 10 MiB raw, 50 MiB decompressed (gzip) |

A request whose `Content-Length` exceeds the limit is rejected with `413 Payload Too Large` before the body is read. The limits are not configurable.

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
| `POST` | `/admin/indexer/run` | Trigger an immediate indexing pass. Returns 409 if already running. Requires [indexing](#indexing) to be enabled |
| `GET` | `/admin/indexer/stats` | The recent indexing passes and the pass running right now. Requires [indexing](#indexing) to be enabled. See [Pass history](#pass-history) |
| `POST` | `/admin/runs/delete` | Queue runs for deletion. Returns 202 as soon as the runs are marked. Requires [indexing](#indexing) to be enabled. See [Run deletion](#run-deletion) |
| `POST` | `/admin/runs/delete/cancel` | Take runs out of the deletion queue. Requires [indexing](#indexing) to be enabled |
| `GET` | `/admin/runs/deletion-queue` | List the runs queued for deletion, in deletion order. Requires [indexing](#indexing) to be enabled |

### Index (requires authentication unless `anonymous_read` is enabled)

Available only when [indexing](#indexing) is enabled.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/index` | List all indexed runs across all discovery paths. Returns the same shape as `index.json` with an additional `discovery_path` field per entry. Sorted by timestamp descending |
| `GET` | `/index/suites/{hash}/stats` | Per-test duration statistics for a suite. Returns the same shape as `stats.json`. Durations are sorted by `time_ns` descending |
| `GET` | `/index/query/runs` | Query indexed runs with PostgREST-style filtering, sorting, and pagination |
| `GET` | `/index/query/test_stats` | Query test stat data with PostgREST-style filtering, sorting, and pagination |
| `GET` | `/index/query/test_stats_block_logs` | Query per-block log data with PostgREST-style filtering, sorting, and pagination |
| `GET` | `/index/query/suites` | Query suite data with PostgREST-style filtering, sorting, and pagination |
| `GET` | `/index/live_runs` | List all live (in-progress) runs reported by runners via the ingest endpoint. Returns an array of `LiveRunResponse` objects |

### Ingest (requires `Authorization: Bearer <api.ingest.token>`)

Available only when [ingest](#ingest-live-run-reporting) is configured.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/ingest/runs` | Upsert a `LiveRunReport` for an in-progress run. Returns 204 on success, 401 on bad token, 400 on missing required fields |

#### Row count

By default, query endpoints skip the `SELECT count(*)` for performance — the `total` field is omitted from the response. To request an exact row count, send the `Prefer: count=exact` header:

```
Prefer: count=exact
```

When present, the response includes `"total": <n>`. This follows the [PostgREST](https://docs.postgrest.org/en/stable/references/api/preferences.html#exact-count) convention. On large tables (e.g. `test_stats`) the count query can take several seconds, so only request it when needed (e.g. for pagination totals).

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
| `api.indexing.failure_grace_period` | `BENCHMARKOOR_API_INDEXING_FAILURE_GRACE_PERIOD` |
| `api.indexing.failure_retry_interval` | `BENCHMARKOOR_API_INDEXING_FAILURE_RETRY_INTERVAL` |
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
    interval: "10m"
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
    interval: "10m"
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

## SuiteTest

### Engine payload sizes

Each `SuiteTest` entry includes three suite-level payload-size fields, computed
once per suite and identical across clients:

- `payload_size_bytes` — SSZ-encoded executionPayload length (with the inline
  `BlockAccessList` included as one field of the payload), summed across all
  `engine_newPayload*` calls in the test step.
- `payload_size_bytes_snappy` — snappy compression of those same SSZ bytes
  (matches consensus-layer gossip transport).
- `bal_size_bytes` — uncompressed byte length of the `BlockAccessList` field
  (already SSZ-serialized in the source fixture as a hex string).

Fields are omitted from the JSON when zero, so consumers should treat
"missing" and "0" equivalently. Tests with no `engine_newPayload*` lines, or
whose payload version is unsupported, leave all three fields at zero.
