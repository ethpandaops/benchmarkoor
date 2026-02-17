# Configuration Reference

This document describes all configuration options for benchmarkoor. The [config.example.yaml](../config.example.yaml) also has a lot of information.

## Table of Contents

- [Overview](#overview)
- [Environment Variables](#environment-variables)
- [Configuration Merging](#configuration-merging)
- [Global Settings](#global-settings)
- [Benchmark Settings](#benchmark-settings)
  - [Results Upload](#results-upload)
- [Client Settings](#client-settings)
  - [Client Defaults](#client-defaults)
  - [Data Directories](#data-directories)
  - [Client Instances](#client-instances)
- [Resource Limits](#resource-limits)
- [Post-Test RPC Calls](#post-test-rpc-calls)
- [API Server](#api-server)
  - [Server Settings](#server-settings)
  - [Authentication](#authentication)
  - [Database](#database)
  - [Storage](#storage)
  - [UI Integration](#ui-integration)
- [Complete Example](#complete-example)

## Overview

Benchmarkoor uses YAML configuration files to define benchmark settings, client configurations, and test sources. Configuration is loaded from one or more files specified via the `--config` flag.

```bash
benchmarkoor run --config config.yaml
```

## Environment Variables

Environment variables can be used anywhere in the configuration using shell-style syntax:

| Syntax | Description |
|--------|-------------|
| `${VAR}` | Substitute the value of `VAR` |
| `$VAR` | Substitute the value of `VAR` |
| `${VAR:-default}` | Use `default` if `VAR` is unset or empty |

Example:
```yaml
global:
  log_level: ${LOG_LEVEL:-info}
benchmark:
  results_dir: ${RESULTS_DIR:-./results}
```

### Environment Variable Overrides

Configuration values can also be overridden via environment variables with the `BENCHMARKOOR_` prefix. The variable name is derived from the config path using underscores:

| Config Path | Environment Variable |
|-------------|---------------------|
| `global.log_level` | `BENCHMARKOOR_GLOBAL_LOG_LEVEL` |
| `benchmark.results_dir` | `BENCHMARKOOR_BENCHMARK_RESULTS_DIR` |
| `client.config.jwt` | `BENCHMARKOOR_CLIENT_CONFIG_JWT` |

## Configuration Merging

Multiple configuration files can be merged by specifying `--config` multiple times:

```bash
benchmarkoor run --config base.yaml --config overrides.yaml
```

Later files override values from earlier files. This is useful for:
- Separating base configuration from environment-specific overrides
- Keeping secrets in a separate file
- Testing different configurations without modifying the base file

## Global Settings

The `global` section contains application-wide settings.

```yaml
global:
  log_level: info
  client_logs_to_stdout: true
  docker_network: benchmarkoor
  cleanup_on_start: false
  directories:
    tmp_datadir: /tmp/benchmarkoor
    tmp_cachedir: /tmp/benchmarkoor-cache
  drop_caches_path: /proc/sys/vm/drop_caches
  cpu_sysfs_path: /sys/devices/system/cpu
```

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `log_level` | string | `info` | Logging level: `debug`, `info`, `warn`, `error` |
| `client_logs_to_stdout` | bool | `false` | Stream client container logs to stdout |
| `docker_network` | string | `benchmarkoor` | Docker network name for containers |
| `cleanup_on_start` | bool | `false` | Remove leftover containers/networks on startup |
| `directories.tmp_datadir` | string | system temp | Directory for temporary datadir copies |
| `directories.tmp_cachedir` | string | `~/.cache/benchmarkoor` | Directory for executor cache (git clones, etc.) |
| `drop_caches_path` | string | `/proc/sys/vm/drop_caches` | Path to Linux drop_caches file (for containerized environments) |
| `cpu_sysfs_path` | string | `/sys/devices/system/cpu` | Base path for CPU sysfs files (for containerized environments where `/sys` is read-only and the host path is bind-mounted elsewhere, e.g., `/host_sys_cpu`) |
| `github_token` | string | - | GitHub token for downloading Actions artifacts via REST API. Not needed if `gh` CLI is installed and authenticated. Requires `actions:read` scope. Can also be set via `BENCHMARKOOR_GLOBAL_GITHUB_TOKEN` env var |

## Benchmark Settings

The `benchmark` section configures test execution and results output.

```yaml
benchmark:
  results_dir: ./results
  results_owner: "1000:1000"
  system_resource_collection_enabled: true
  generate_results_index: true
  generate_suite_stats: true
  tests:
    filter: "erc20"
    source:
      git:
        repo: https://github.com/example/benchmarks.git
        version: main
```

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `results_dir` | string | `./results` | Directory for benchmark results |
| `results_owner` | string | - | Set ownership (user:group) for results files. Useful when running as root |
| `skip_test_run` | bool | `false` | Skip test execution; only run post-run operations (index/stats generation) |
| `system_resource_collection_enabled` | bool | `true` | Enable CPU/memory/disk metrics collection via cgroups/Docker Stats API |
| `generate_results_index` | bool | `false` | Generate `index.json` aggregating all run metadata |
| `generate_results_index_method` | string | `local` | Method for index generation: `local` (filesystem) or `s3` (read runs from S3, upload index back). Requires `results_upload.s3` when set to `s3` |
| `generate_suite_stats` | bool | `false` | Generate `stats.json` per suite for UI heatmaps |
| `generate_suite_stats_method` | string | `local` | Method for suite stats generation: `local` (filesystem) or `s3` (read runs from S3, upload stats back). Requires `results_upload.s3` when set to `s3` |
| `tests.filter` | string | - | Run only tests matching this pattern |
| `tests.source` | object | - | Test source configuration (see below) |

### Test Sources

Tests can be loaded from a local directory, a git repository, or EEST (Ethereum Execution Spec Tests) fixtures. Only one source type can be configured.

#### Local Source

```yaml
tests:
  source:
    local:
      base_dir: ./benchmark-tests
      pre_run_steps:
        - "warmup/*.txt"
      steps:
        setup:
          - "tests/setup/*.txt"
        test:
          - "tests/test/*.txt"
        cleanup:
          - "tests/cleanup/*.txt"
```

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `base_dir` | string | Yes | Path to the local test directory |
| `pre_run_steps` | []string | No | Glob patterns for steps executed once before all tests |
| `steps.setup` | []string | No | Glob patterns for setup phase files |
| `steps.test` | []string | No | Glob patterns for test phase files |
| `steps.cleanup` | []string | No | Glob patterns for cleanup phase files |

#### Git Source

```yaml
tests:
  source:
    git:
      repo: https://github.com/example/gas-benchmarks.git
      version: main
      pre_run_steps:
        - "funding/*.txt"
      steps:
        setup:
          - "tests/setup/*.txt"
        test:
          - "tests/test/*.txt"
        cleanup:
          - "tests/cleanup/*.txt"
```

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `repo` | string | Yes | Git repository URL |
| `version` | string | Yes | Branch name, tag, or commit hash |
| `pre_run_steps` | []string | No | Glob patterns for steps executed once before all tests |
| `steps.setup` | []string | No | Glob patterns for setup phase files |
| `steps.test` | []string | No | Glob patterns for test phase files |
| `steps.cleanup` | []string | No | Glob patterns for cleanup phase files |

#### EEST Fixtures Source

EEST (Ethereum Execution Spec Tests) fixtures can be loaded from GitHub releases or GitHub Actions artifacts. This source type downloads fixtures from `ethereum/execution-spec-tests` and converts them to Engine API calls automatically.

##### From GitHub Releases

```yaml
tests:
  source:
    eest_fixtures:
      github_repo: ethereum/execution-spec-tests
      github_release: benchmark@v0.0.7
      fixtures_subdir: fixtures/blockchain_tests_engine_x
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `github_repo` | string | Yes | - | GitHub repository (e.g., `ethereum/execution-spec-tests`) |
| `github_release` | string | Yes* | - | Release tag (e.g., `benchmark@v0.0.7`) |
| `fixtures_subdir` | string | No | `fixtures/blockchain_tests_engine_x` | Subdirectory within the fixtures tarball to search |
| `fixtures_url` | string | No | Auto-generated | Override URL for fixtures tarball |
| `genesis_url` | string | No | Auto-generated | Override URL for genesis tarball |

*Either `github_release` or `fixtures_artifact_name` is required.

##### From GitHub Actions Artifacts

As an alternative to releases, you can download fixtures directly from GitHub Actions workflow artifacts. This is useful for testing with fixtures from CI builds before they're released.

**Requirements:** Either the `gh` CLI must be installed and authenticated with GitHub, or `global.github_token` must be set (a token with `actions:read` scope).

```yaml
tests:
  source:
    eest_fixtures:
      github_repo: ethereum/execution-spec-tests
      fixtures_artifact_name: fixtures_benchmark_fast
      genesis_artifact_name: benchmark_genesis
      # Optional: specify a specific workflow run ID (uses latest if not specified)
      # fixtures_artifact_run_id: "12345678901"
      # genesis_artifact_run_id: "12345678901"
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `github_repo` | string | Yes | - | GitHub repository (e.g., `ethereum/execution-spec-tests`) |
| `fixtures_artifact_name` | string | Yes* | - | Name of the fixtures artifact to download |
| `genesis_artifact_name` | string | No | `benchmark_genesis` | Name of the genesis artifact to download |
| `fixtures_artifact_run_id` | string | No | Latest | Specific workflow run ID for fixtures artifact |
| `genesis_artifact_run_id` | string | No | Latest | Specific workflow run ID for genesis artifact |
| `fixtures_subdir` | string | No | `fixtures/blockchain_tests_engine_x` | Subdirectory within the fixtures to search |

*Either `github_release`, `fixtures_artifact_name`, `local_fixtures_dir`/`local_genesis_dir`, or `local_fixtures_tarball`/`local_genesis_tarball` is required. Only one mode can be used at a time.

##### From Local Directories

For local development with already-extracted EEST fixtures (e.g., built locally from the `execution-spec-tests` repository), you can point directly at the directories. No downloading or caching is performed.

```yaml
tests:
  source:
    eest_fixtures:
      local_fixtures_dir: /home/user/eest-output/fixtures
      local_genesis_dir: /home/user/eest-output/genesis
      # Optional: Override the subdirectory within fixtures to search.
      # fixtures_subdir: fixtures/blockchain_tests_engine_x  # default
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `local_fixtures_dir` | string | Yes* | - | Path to extracted fixtures directory |
| `local_genesis_dir` | string | Yes* | - | Path to extracted genesis directory |
| `fixtures_subdir` | string | No | `fixtures/blockchain_tests_engine_x` | Subdirectory within the fixtures directory to search |

*Both `local_fixtures_dir` and `local_genesis_dir` must be set together. Both paths must exist and be directories.

`github_repo` is not required for local modes.

##### From Local Tarballs

If you have locally-built `.tar.gz` tarballs (e.g., `fixtures_benchmark.tar.gz` and `benchmark_genesis.tar.gz`), you can use them directly. The tarballs are extracted to a cache directory keyed by a hash of the tarball paths, so re-extraction is skipped on subsequent runs.

```yaml
tests:
  source:
    eest_fixtures:
      local_fixtures_tarball: /home/user/eest-output/fixtures_benchmark.tar.gz
      local_genesis_tarball: /home/user/eest-output/benchmark_genesis.tar.gz
      # Optional: Override the subdirectory within fixtures to search.
      # fixtures_subdir: fixtures/blockchain_tests_engine_x  # default
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `local_fixtures_tarball` | string | Yes* | - | Path to fixtures `.tar.gz` file |
| `local_genesis_tarball` | string | Yes* | - | Path to genesis `.tar.gz` file |
| `fixtures_subdir` | string | No | `fixtures/blockchain_tests_engine_x` | Subdirectory within the extracted fixtures to search |

*Both `local_fixtures_tarball` and `local_genesis_tarball` must be set together. Both paths must exist and be regular files.

`github_repo` is not required for local modes.

**Key features:**
- Automatically downloads and caches fixtures from GitHub releases or artifacts
- Supports local directories and local `.tar.gz` tarballs for offline/development use
- Converts EEST fixture format to `engine_newPayloadV{1-4}` + `engine_forkchoiceUpdatedV{1,3}` calls
- Only includes fixtures with `fixture-format: blockchain_test_engine_x`
- Auto-resolves genesis files per client type from the release/artifact/local source

**Genesis file resolution:**

When using EEST fixtures, genesis files are automatically resolved based on client type. You don't need to configure `client.config.genesis` unless you want to override the defaults.

| Client | Genesis Path |
|--------|--------------|
| geth, erigon, reth, nimbus | `go-ethereum/genesis.json` |
| nethermind | `nethermind/chainspec.json` |
| besu | `besu/genesis.json` |

**Example with filter:**

```yaml
benchmark:
  tests:
    filter: "bn128"  # Only run tests matching "bn128"
    source:
      eest_fixtures:
        github_repo: ethereum/execution-spec-tests
        github_release: benchmark@v0.0.7
```

### Results Upload

The `benchmark.results_upload` section configures automatic uploading of results to remote storage after each instance run. Currently only S3-compatible storage is supported.

```yaml
benchmark:
  results_upload:
    s3:
      enabled: true
      endpoint_url: https://s3.amazonaws.com
      region: us-east-1
      bucket: my-benchmark-results
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_SECRET_ACCESS_KEY}
      prefix: results
      # storage_class: STANDARD
      # acl: private
      force_path_style: false
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `enabled` | bool | Yes | `false` | Enable S3 upload |
| `bucket` | string | Yes | - | S3 bucket name |
| `endpoint_url` | string | No | AWS default | S3 endpoint URL — scheme and host only, no path (e.g., `https://<id>.r2.cloudflarestorage.com`) |
| `region` | string | No | `us-east-1` | AWS region |
| `access_key_id` | string | No | - | Static AWS access key ID |
| `secret_access_key` | string | No | - | Static AWS secret access key |
| `prefix` | string | No | `results` | Base key prefix. Runs are stored under `prefix/runs/`, suites under `prefix/suites/` |
| `storage_class` | string | No | Bucket default | S3 storage class (e.g., `STANDARD`, `STANDARD_IA`) |
| `acl` | string | No | - | Canned ACL (e.g., `private`, `public-read`) |
| `force_path_style` | bool | No | `false` | Use path-style addressing (required for MinIO and Cloudflare R2) |
| `parallel_uploads` | int | No | `50` | Number of concurrent file uploads |

**Important:** The `endpoint_url` must be the base URL without any path component. Do not include the bucket name in the URL — the SDK handles that separately via the `bucket` field. For example, use `https://<account_id>.r2.cloudflarestorage.com`, not `https://<account_id>.r2.cloudflarestorage.com/my-bucket`.

When enabled, a preflight check runs before any benchmarks to verify S3 connectivity. Each instance's results directory is uploaded after the run completes (including on failure, for partial results).

Results can also be uploaded manually using the `upload-results` subcommand:

```bash
benchmarkoor upload-results --method=s3 --config config.yaml --result-dir=./results/runs/<run_dir>
```

The `generate-index-file` command also supports reading directly from S3. This is useful for regenerating `index.json` from remote data without having all results locally:

```bash
benchmarkoor generate-index-file --method=s3 --config config.yaml
```

When using `--method=s3`, the command reads `config.json` and `result.json` from each run directory in the bucket, builds the index in memory, and uploads `index.json` at `prefix/index.json` (e.g. prefix `demo/results` places `index.json` at `demo/results/index.json`).

The `generate-suite-stats-file` command also supports reading directly from S3:

```bash
benchmarkoor generate-suite-stats-file --method=s3 --config config.yaml
```

When using `--method=s3`, the command reads `config.json` and `result.json` from each run, groups them by suite hash, builds per-suite stats in memory, and uploads `stats.json` to `prefix/suites/{hash}/stats.json`.

## Client Settings

The `client` section configures Ethereum execution clients.

### Supported Clients

| Client | Type | Default Image |
|--------|------|---------------|
| Geth | `geth` | `ethpandaops/geth:performance` |
| Nethermind | `nethermind` | `ethpandaops/nethermind:performance` |
| Besu | `besu` | `ethpandaops/besu:performance` |
| Erigon | `erigon` | `ethpandaops/erigon:performance` |
| Nimbus | `nimbus` | `statusim/nimbus-eth1:performance` |
| Reth | `reth` | `ethpandaops/reth:performance` |

### Client Defaults

The `client.config` section sets defaults applied to all client instances.

```yaml
client:
  config:
    jwt: "5a64f13bfb41a147711492237995b437433bcbec80a7eb2daae11132098d7bae"
    drop_memory_caches: "disabled"
    rollback_strategy: "rpc-debug-setHead"  # or "none"
    resource_limits:
      cpuset_count: 4
      memory: "16g"
      swap_disabled: true
    genesis:
      geth: https://example.com/genesis/geth.json
      nethermind: https://example.com/genesis/nethermind.json
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `jwt` | string | `5a64f1...` | JWT secret for Engine API authentication |
| `drop_memory_caches` | string | `disabled` | When to drop Linux memory caches (see below) |
| `rollback_strategy` | string | `rpc-debug-setHead` | Rollback strategy after each test (see below) |
| `wait_after_rpc_ready` | string | - | Duration to wait after RPC becomes ready (see below) |
| `retry_new_payloads_syncing_state` | object | - | Retry config for SYNCING responses (see below) |
| `resource_limits` | object | - | Container resource constraints (see [Resource Limits](#resource-limits)) |
| `post_test_rpc_calls` | []object | - | Arbitrary RPC calls to execute after each test step (see [Post-Test RPC Calls](#post-test-rpc-calls)) |
| `bootstrap_fcu` | bool/object | - | Send an `engine_forkchoiceUpdatedV3` after RPC is ready to confirm the client is fully synced (see [Bootstrap FCU](#bootstrap-fcu)) |
| `genesis` | map | - | Genesis file URLs keyed by client type |

#### Drop Memory Caches

This Linux-only feature (requires root) drops page cache, dentries, and inodes between benchmark phases for more consistent results.

| Value | Description |
|-------|-------------|
| `disabled` | Do not drop caches (default) |
| `tests` | Drop caches between tests |
| `steps` | Drop caches between all steps (setup, test, cleanup) |

#### Rollback Strategy

Controls whether the client state is rolled back after each test. This is useful for stateful benchmarks where tests modify chain state and you want each test to start from the same block.

| Value | Description |
|-------|-------------|
| `none` | Do not rollback |
| `rpc-debug-setHead` | Capture block info before each test, then rollback via a client-specific debug RPC after the test completes (default) |
| `container-recreate` | Stop and remove the container after each test, then create and start a fresh one |

##### `rpc-debug-setHead`

When `rpc-debug-setHead` is enabled, the following happens for each test:

1. Before the test, `eth_getBlockByNumber("latest", false)` is called to capture the current block number and hash.
2. The test (including setup and cleanup steps) runs normally.
3. After the test, a client-specific rollback RPC call is made.
4. The rollback is verified by calling `eth_getBlockByNumber("latest", false)` again and comparing the block number.

If the rollback fails or the block number doesn't match, a warning is logged but the test is not marked as failed.

##### Client-specific RPC calls

Each client uses a different RPC method and parameter format for rollback:

| Client | RPC Method | Parameter | Example payload |
|--------|------------|-----------|-----------------|
| Geth | `debug_setHead` | Hex block number | `{"method":"debug_setHead","params":["0x5"]}` |
| Besu | `debug_setHead` | Hex block number | `{"method":"debug_setHead","params":["0x5"]}` |
| Reth | `debug_setHead` | Integer block number | `{"method":"debug_setHead","params":[5]}` |
| Nethermind | `debug_resetHead` | Block hash | `{"method":"debug_resetHead","params":["0xabc..."]}` |
| Erigon | N/A | N/A | Not supported |
| Nimbus | N/A | N/A | Not supported |

For clients that don't support rollback (Erigon, Nimbus), a warning is logged and the rollback step is skipped.

##### `container-recreate`

When `container-recreate` is enabled, the runner manages the per-test loop:

1. The first test runs against the original container.
2. After each test, the container is stopped and removed.
3. A new container is created and started with the same configuration. The data volume/datadir persists.
4. The runner waits for the RPC endpoint to become ready and the configured wait period before running the next test.

This strategy works with all clients since it doesn't require any client-specific RPC support.

#### Wait After RPC Ready

Some clients (e.g., Erigon) have internal sync pipelines that continue running after their RPC endpoint becomes available. The `wait_after_rpc_ready` option adds a configurable delay after the RPC health check passes, giving the client time to complete internal initialization before test execution begins.

```yaml
client:
  config:
    wait_after_rpc_ready: 30s
```

The value is a Go duration string (e.g., `30s`, `1m`, `500ms`). If not set, no additional wait is performed.

**When to use:**
- When running benchmarks against clients with staged sync pipelines (Erigon)
- When you observe `SYNCING` responses from Engine API calls despite the RPC being available
- When starting from pre-populated data directories where clients may need time to validate state

#### Retry New Payloads Syncing State

When `engine_newPayload` returns a `SYNCING` status, it indicates the client hasn't fully processed the parent block yet. The `retry_new_payloads_syncing_state` option configures automatic retries with exponential backoff.

```yaml
client:
  config:
    retry_new_payloads_syncing_state:
      enabled: true
      max_retries: 10
      backoff: 1s
```

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `enabled` | bool | Yes | Enable retry behavior |
| `max_retries` | int | Yes | Maximum number of retry attempts (must be ≥ 1) |
| `backoff` | string | Yes | Delay between retries (Go duration string) |

**When to use:**
- When benchmarking clients that return `SYNCING` during normal operation (Erigon)
- When using pre-populated data directories where clients may need time to validate chain state
- Combined with `wait_after_rpc_ready` for clients with complex initialization


#### Bootstrap FCU

Some clients (e.g., Erigon) may still be performing internal initialization or syncing after their RPC endpoint becomes available. The `bootstrap_fcu` option sends an `engine_forkchoiceUpdatedV3` call in a retry loop after RPC is ready, using the latest block hash from `eth_getBlockByNumber("latest")`. The client accepting the FCU with `VALID` status confirms it has finished syncing and is ready for test execution.

**Shorthand** (uses defaults: `max_retries: 30`, `backoff: 1s`):

```yaml
client:
  config:
    bootstrap_fcu: true
```

**Full configuration:**

```yaml
client:
  config:
    bootstrap_fcu:
      enabled: true
      max_retries: 30
      backoff: 1s
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `enabled` | bool | Yes | - | Enable bootstrap FCU |
| `max_retries` | int | Yes | `30` (shorthand) | Maximum number of retry attempts (must be >= 1) |
| `backoff` | string | Yes | `1s` (shorthand) | Delay between retries (Go duration string) |

The FCU call sets `headBlockHash` to the latest block, with `safeBlockHash` and `finalizedBlockHash` set to the zero hash and no payload attributes. The response must have `VALID` status. If the call fails, it is retried up to `max_retries` times with `backoff` between attempts. If all attempts fail, the run is aborted.

When using the `container-recreate` rollback strategy, the bootstrap FCU is sent after each container recreate.

**When to use:**
- When clients may still be performing internal initialization or syncing after RPC becomes available (e.g., Erigon's staged sync)
- When starting from pre-populated data directories where the client needs time to validate state before processing Engine API requests
- When you observe test failures due to the client returning errors or SYNCING responses on the first Engine API calls

### Data Directories

The `client.datadirs` section configures pre-populated data directories per client type. When configured, the init container is skipped and data is mounted directly.

```yaml
client:
  datadirs:
    geth:
      source_dir: ./data/snapshots/geth
      # container_dir defaults to /data (geth's data directory)
      method: copy
    reth:
      source_dir: ./data/snapshots/reth
      # container_dir defaults to /var/lib/reth (reth's data directory)
      method: overlayfs
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `source_dir` | string | Required | Path to the source data directory |
| `container_dir` | string | Client default | Mount path inside the container. If not specified, uses the client's default data directory (e.g., `/var/lib/reth` for reth, `/data` for geth) |
| `method` | string | `copy` | Method for preparing the data directory |

#### Data Directory Methods

| Method | Description | Requirements |
|--------|-------------|--------------|
| `copy` | Parallel Go copy with progress display | None (default, works everywhere) |
| `overlayfs` | Linux overlayfs for near-instant setup | Root access |
| `fuse-overlayfs` | FUSE-based overlayfs | `fuse-overlayfs` package; `user_allow_other` in `/etc/fuse.conf` if Docker runs as root. **Warning:** ~3x slower than native overlayfs |
| `zfs` | ZFS snapshots and clones for copy-on-write setup | Source directory on ZFS filesystem; root access or ZFS delegations configured |

##### ZFS Setup

For ZFS method without root:
```bash
zfs allow -u <user> clone,create,destroy,mount,snapshot <dataset>
```

The dataset is auto-detected from the source directory mount point.

##### Default Container Directories

When `container_dir` is not specified, the client's default data directory is used:

| Client | Default Data Directory |
|--------|----------------------|
| geth | `/data` |
| nethermind | `/data` |
| besu | `/data` |
| erigon | `/data` |
| nimbus | `/data` |
| reth | `/var/lib/reth` |

### Client Instances

The `client.instances` array defines which client configurations to benchmark.

```yaml
client:
  instances:
    - id: geth-latest
      client: geth
      image: ethpandaops/geth:performance
      pull_policy: always
      entrypoint: []
      command: []
      extra_args:
        - --verbosity=5
      restart: never
      environment:
        GOMEMLIMIT: "14GiB"
      genesis: https://example.com/custom-genesis.json
      datadir:
        source_dir: ./snapshots/geth
        # container_dir defaults to client's data directory
        method: overlayfs
      drop_memory_caches: "steps"
      resource_limits:
        cpuset_count: 2
        memory: "8g"
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `id` | string | Yes | - | Unique identifier for this instance |
| `client` | string | Yes | - | Client type (see [Supported Clients](#supported-clients)) |
| `image` | string | No | Per-client default | Docker image to use |
| `pull_policy` | string | No | `always` | Image pull policy: `always`, `never`, `missing` |
| `entrypoint` | []string | No | Client default | Override container entrypoint |
| `command` | []string | No | Client default | Override container command |
| `extra_args` | []string | No | - | Additional arguments appended to command |
| `restart` | string | No | - | Container restart policy |
| `environment` | map | No | - | Additional environment variables |
| `genesis` | string | No | From `client.config.genesis` | Override genesis file URL |
| `datadir` | object | No | From `client.datadirs` | Instance-specific data directory config |
| `drop_memory_caches` | string | No | From `client.config` | Instance-specific cache drop setting |
| `rollback_strategy` | string | No | From `client.config` | Instance-specific rollback strategy |
| `wait_after_rpc_ready` | string | No | From `client.config` | Instance-specific RPC ready wait duration |
| `retry_new_payloads_syncing_state` | object | No | From `client.config` | Instance-specific retry config for SYNCING responses |
| `resource_limits` | object | No | From `client.config` | Instance-specific resource limits |
| `post_test_rpc_calls` | []object | No | From `client.config` | Instance-specific post-test RPC calls (replaces global) |
| `bootstrap_fcu` | bool/object | No | From `client.config` | Instance-specific bootstrap FCU setting |

## Resource Limits

Resource limits can be configured globally (`client.config.resource_limits`) or per-instance (`client.instances[].resource_limits`). Instance-level settings override global defaults.

```yaml
resource_limits:
  cpuset_count: 4
  # OR
  cpuset: [0, 1, 2, 3]
  memory: "16g"
  swap_disabled: true
  blkio_config:
    device_read_bps:
      - path: /dev/sdb
        rate: '12mb'
    device_write_bps:
      - path: /dev/sdb
        rate: '1024k'
    device_read_iops:
      - path: /dev/sdb
        rate: '120'
    device_write_iops:
      - path: /dev/sdb
        rate: '30'
```

| Option | Type | Description |
|--------|------|-------------|
| `cpuset_count` | int | Number of random CPUs to pin to (new selection each run) |
| `cpuset` | []int | Specific CPU IDs to pin to |
| `cpu_freq` | string | Fixed CPU frequency. Supports: `"2000MHz"`, `"2.4GHz"`, `"MAX"` (use system maximum) |
| `cpu_turboboost` | bool | Enable (`true`) or disable (`false`) turbo boost. Omit to leave unchanged |
| `cpu_freq_governor` | string | CPU frequency governor. Common values: `performance`, `powersave`, `schedutil`. Defaults to `performance` when `cpu_freq` is set |
| `memory` | string | Memory limit with unit: `b`, `k`, `m`, `g` (e.g., `"16g"`, `"4096m"`) |
| `swap_disabled` | bool | Disable swap (sets memory-swap equal to memory, swappiness to 0) |
| `blkio_config` | object | Block I/O throttling configuration (see below) |

**Note:** `cpuset_count` and `cpuset` are mutually exclusive. Use one or the other.

### Block I/O Configuration

The `blkio_config` option allows throttling container disk I/O:

| Option | Type | Description |
|--------|------|-------------|
| `device_read_bps` | []object | Device read bandwidth limits |
| `device_read_iops` | []object | Device read IOPS limits |
| `device_write_bps` | []object | Device write bandwidth limits |
| `device_write_iops` | []object | Device write IOPS limits |

Each device entry has:

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Device path (e.g., `/dev/sdb`) |
| `rate` | string | Rate limit. For `*_bps`: string with unit (`b`, `k`, `m`, `g`). For `*_iops`: integer string |

### CPU Frequency Management

CPU frequency settings allow you to lock CPUs to a specific frequency, control turbo boost, and set the CPU frequency governor. This is useful for achieving more consistent benchmark results by eliminating CPU frequency variations.

**Requirements:**
- Linux only
- Root access (requires write access to `/sys/devices/system/cpu/*/cpufreq/`)
- cpufreq subsystem must be available
- When running in Docker, bind-mount `/sys/devices/system/cpu` into the container and set `global.cpu_sysfs_path` to the mount point (e.g., `/host_sys_cpu`)

```yaml
resource_limits:
  cpuset_count: 4
  cpu_freq: "2000MHz"
  cpu_turboboost: false
  cpu_freq_governor: performance
```

**Notes:**
- CPU frequency settings are applied to the CPUs specified by `cpuset` or `cpuset_count`. If neither is specified, settings are applied to all online CPUs.
- Original CPU frequency settings are automatically restored when the benchmark completes or is interrupted.
- If the process is killed, the `benchmarkoor cleanup` command will restore CPU frequency settings from saved state files.

**Turbo Boost:**
- Intel systems: Controls `/sys/devices/system/cpu/intel_pstate/no_turbo`
- AMD systems: Controls `/sys/devices/system/cpu/cpufreq/boost`

**Available Governors:**

Common governors (availability depends on kernel configuration):

| Governor | Description |
|----------|-------------|
| `performance` | Always run at max frequency (best for benchmarks) |
| `powersave` | Always run at min frequency |
| `schedutil` | Scale frequency based on CPU utilization (default on modern kernels) |
| `ondemand` | Scale frequency based on load |
| `conservative` | Like ondemand but more gradual changes |

**Example: Consistent Benchmark Configuration**

For the most consistent benchmark results, lock the CPU frequency and disable turbo boost:

```yaml
client:
  config:
    resource_limits:
      cpuset_count: 4
      cpu_freq: "2000MHz"
      cpu_turboboost: false
      cpu_freq_governor: performance
      memory: "16g"
      swap_disabled: true
```

## Post-Test RPC Calls

Post-test RPC calls allow you to execute arbitrary JSON-RPC calls after each test step completes. These calls are **not timed** and do **not affect test results**. They are useful for collecting debug traces, state snapshots, or other diagnostic data from the client after each test.

Calls are made to the client's regular RPC endpoint (no JWT authentication). If a call fails, a warning is logged and the remaining calls continue.

```yaml
client:
  config:
    post_test_rpc_calls:
      - method: debug_traceBlockByNumber
        params: ["{{.BlockNumberHex}}", {"tracer": "callTracer"}]
        dump:
          enabled: true
          filename: debug_traceBlockByNumber
      - method: debug_traceBlockByHash
        params: ["{{.BlockHash}}"]
        timeout: 2m  # Override default 30s timeout for slow methods
        dump:
          enabled: true
          filename: debug_traceBlockByHash
```

### Call Options

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `method` | string | Yes | JSON-RPC method name |
| `params` | []any | No | Method parameters (supports template variables) |
| `timeout` | string | No | Per-call timeout as a Go duration string (e.g., `30s`, `2m`). Default: `30s` |
| `dump` | object | No | Response dump configuration |
| `dump.enabled` | bool | No | Enable writing the response to a file |
| `dump.filename` | string | When dump enabled | Base filename for the dump (`.json` extension is added automatically) |

### Template Variables

Go `text/template` syntax is supported in all string values within `params`. Templates are applied recursively to strings inside arrays and objects.

| Variable | Description | Example |
|----------|-------------|---------|
| `{{.BlockHash}}` | Hash of the latest block | `"0xabc..."` |
| `{{.BlockNumber}}` | Block number as decimal string | `"1234"` |
| `{{.BlockNumberHex}}` | Block number as hex with `0x` prefix | `"0x4d2"` |

Non-string values (booleans, numbers) pass through unchanged.

### Dump Output

When `dump.enabled` is `true`, the raw JSON-RPC response is written to:

```
{resultsDir}/{testName}/post_test_rpc_calls/{dump.filename}.json
```

The response is pretty-printed if it is valid JSON. File ownership respects the `results_owner` configuration.

### Execution Flow

Post-test RPC calls run after the test step and before the cleanup step:

```
1. Setup step (if present)
2. Test step (timed, results written)
3. Post-test RPC calls              ← runs here
4. Cleanup step (if present)
5. Rollback (if configured)
```

### Instance-Level Override

Instance-level `post_test_rpc_calls` completely replace global defaults (not merged):

```yaml
client:
  config:
    post_test_rpc_calls:
      - method: debug_traceBlockByNumber
        params: ["{{.BlockNumberHex}}"]
        dump:
          enabled: true
          filename: trace_by_number
  instances:
    - id: geth-latest
      client: geth
      # This replaces the global calls entirely:
      post_test_rpc_calls:
        - method: debug_traceBlockByHash
          params: ["{{.BlockHash}}"]
          dump:
            enabled: true
            filename: trace_by_hash
```

## API Server

The optional `api` section configures a standalone API server for authentication and user management. The API server is started separately from the benchmark runner using the `benchmarkoor api` subcommand.

```bash
benchmarkoor api --config config.yaml
```

When the `api` section is absent from the config, the API server cannot be started. The UI works without the API — it only integrates with the API when `api` is defined in the UI's `config.json`.

### Server Settings

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

### Authentication

At least one authentication provider must be enabled. Two providers are supported: basic (username/password) and GitHub OAuth. Both can be enabled simultaneously.

#### General Auth Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `auth.session_ttl` | string | `24h` | Session duration as a Go duration string (e.g., `24h`, `12h`, `30m`) |
| `auth.anonymous_read` | bool | `false` | Allow unauthenticated access to `/files/` endpoints. When `true`, the UI allows browsing without login. When `false`, users must sign in to access file data and the UI redirects to the login page |

Sessions are stored in the database and cleaned up automatically every 15 minutes.

#### Basic Authentication

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

#### GitHub OAuth

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

#### Roles

| Role | Permissions |
|------|-------------|
| `admin` | Full access: view data, manage users, manage GitHub mappings |
| `readonly` | View access only |

### Database

The API server uses a database for storing users, sessions, and GitHub role mappings. Two drivers are supported.

#### SQLite (default)

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

#### PostgreSQL

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

### Storage

The optional `api.storage` section configures S3-compatible storage for serving benchmark result files to authenticated users via presigned URLs. This is **separate** from `benchmark.results_upload.s3` (which handles uploads during benchmark runs). The API uses this to generate presigned GET URLs so the UI can fetch files directly from S3.

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

#### How It Works

1. The `GET /api/v1/config` endpoint advertises which `discovery_paths` are available and whether S3 storage is enabled.
2. The UI uses this to know where to look for `index.json` files in S3.
3. When the UI needs a file, it requests `GET /api/v1/files/{key}` (e.g., `GET /api/v1/files/results/index.json`).
4. The API validates the requested key is under an allowed discovery path, then returns a presigned S3 GET URL.
5. The UI fetches the file directly from S3 using the presigned URL.

#### Path Validation

Requested file paths are validated before generating a presigned URL:
- The path must be non-empty and clean (no `..`, no trailing slashes)
- The path must fall under one of the configured `discovery_paths` prefixes
- Partial prefix matches are rejected (e.g., `results_backup/file` does not match prefix `results`)

### Environment Variable Overrides

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

### API Endpoints

All endpoints are under the `/api/v1` prefix.

#### Public

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check (`{"status":"ok"}`) |
| `GET` | `/config` | Public configuration (auth providers, `anonymous_read`, storage settings) |

#### Authentication

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/auth/login` | Login with username/password |
| `POST` | `/auth/logout` | Destroy current session |
| `GET` | `/auth/me` | Get current user (requires auth) |
| `GET` | `/auth/github` | Initiate GitHub OAuth flow |
| `GET` | `/auth/github/callback` | GitHub OAuth callback |

#### Admin (requires `admin` role)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/users` | List all users |
| `POST` | `/admin/users` | Create a user |
| `PUT` | `/admin/users/{id}` | Update a user |
| `DELETE` | `/admin/users/{id}` | Delete a user |
| `GET` | `/admin/github/org-mappings` | List org role mappings |
| `POST` | `/admin/github/org-mappings` | Create/update org mapping |
| `DELETE` | `/admin/github/org-mappings/{id}` | Delete org mapping |
| `GET` | `/admin/github/user-mappings` | List user role mappings |
| `POST` | `/admin/github/user-mappings` | Create/update user mapping |
| `DELETE` | `/admin/github/user-mappings/{id}` | Delete user mapping |

#### Files (requires authentication unless `anonymous_read` is enabled)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/files/*` | Generate a presigned S3 URL for the given file path. Returns `{"url":"..."}`. Requires [storage](#storage) to be configured. Requires authentication unless `auth.anonymous_read` is `true` |

### UI Integration

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
- **Admin page** (`/admin`) — user management, GitHub org/user role mapping management
- **Header controls** — sign in/out button, username display, admin link (for admins)

When the API is not configured, none of these features appear and the UI functions as a static results viewer.

## Examples

Running stateless tests across all clients:

```yaml
global:
  log_level: info
  client_logs_to_stdout: true
  cleanup_on_start: false

benchmark:
  results_dir: ./results
  generate_results_index: true
  generate_suite_stats: true
  tests:
    filter: "bn128"
    source:
      git:
        repo: https://github.com/NethermindEth/gas-benchmarks.git
        version: main
        pre_run_steps: []
        steps:
          setup:
            - eest_tests/setup/*/*
          test:
            - eest_tests/testing/*/*
          cleanup: []

client:
  config:
    resource_limits:
      cpuset_count: 4
      memory: "16g"
      swap_disabled: true
    genesis:
      besu: https://github.com/nethermindeth/gas-benchmarks/raw/refs/heads/main/scripts/genesisfiles/besu/zkevmgenesis.json
      erigon: https://github.com/nethermindeth/gas-benchmarks/raw/refs/heads/main/scripts/genesisfiles/geth/zkevmgenesis.json
      ethrex: https://github.com/nethermindeth/gas-benchmarks/raw/refs/heads/main/scripts/genesisfiles/geth/zkevmgenesis.json
      geth: https://github.com/nethermindeth/gas-benchmarks/raw/refs/heads/main/scripts/genesisfiles/geth/zkevmgenesis.json
      nethermind: https://github.com/nethermindeth/gas-benchmarks/raw/refs/heads/main/scripts/genesisfiles/nethermind/zkevmgenesis.json
      nimbus: https://github.com/nethermindeth/gas-benchmarks/raw/refs/heads/main/scripts/genesisfiles/geth/zkevmgenesis.json
      reth: https://github.com/nethermindeth/gas-benchmarks/raw/refs/heads/main/scripts/genesisfiles/geth/zkevmgenesis.json

  instances:
    - id: nethermind
      client: nethermind
    - id: geth
      client: geth
    - id: reth
      client: reth
    - id: erigon
      client: erigon
    - id: besu
      client: besu
```

Running EEST fixtures across multiple clients:

```yaml
global:
  log_level: info
  client_logs_to_stdout: true
  cleanup_on_start: true

benchmark:
  results_dir: ./results
  generate_results_index: true
  generate_suite_stats: true
  tests:
    filter: "bn128"  # Optional: filter tests by name
    source:
      eest_fixtures:
        github_repo: ethereum/execution-spec-tests
        github_release: benchmark@v0.0.7

client:
  config:
    resource_limits:
      cpuset_count: 4
      memory: "16g"
      swap_disabled: true
    # Genesis files are auto-resolved from the EEST release.
    # No need to configure genesis URLs unless you want to override.

  instances:
    - id: geth
      client: geth
    - id: nethermind
      client: nethermind
    - id: reth
      client: reth
    - id: besu
      client: besu
    - id: erigon
      client: erigon
```

Running EEST fixtures from a local directory (no GitHub required):

```yaml
global:
  log_level: info
  client_logs_to_stdout: true
  cleanup_on_start: true

benchmark:
  results_dir: ./results
  generate_results_index: true
  generate_suite_stats: true
  tests:
    source:
      eest_fixtures:
        local_fixtures_dir: /home/user/execution-spec-tests/output/fixtures
        local_genesis_dir: /home/user/execution-spec-tests/output/genesis

client:
  config:
    resource_limits:
      cpuset_count: 4
      memory: "16g"
      swap_disabled: true

  instances:
    - id: geth
      client: geth
    - id: reth
      client: reth
```

Running stateful tests on a geth container with an existing data directory:

```yaml
global:
  log_level: info
  client_logs_to_stdout: true
  cleanup_on_start: false

benchmark:
  results_dir: ./results
  results_owner: "${UID}:${GID}"
  generate_results_index: true
  generate_suite_stats: true
  tests:
    source:
      git:
        repo: https://github.com/skylenet/gas-benchmarks.git
        version: order-stateful-tests-subdirs
        pre_run_steps:
          - stateful_tests/gas-bump.txt
          - stateful_tests/funding.txt
        steps:
          setup:
            - stateful_tests/setup/*/*
          test:
            - stateful_tests/testing/*/*
          cleanup:
            - stateful_tests/cleanup/*/*
client:
  config:
    drop_memory_caches: "steps"
  datadirs:
    geth:
      source_dir: ${HOME}/data/clients/perf-devnet-2/23861500/geth
      method: overlayfs
  instances:
    - id: geth
      client: geth
      image: ethpandaops/geth:master
      extra_args:
        - --miner.gaslimit=1000000000
        - --txpool.globalqueue=10000
        - --txpool.globalslots=10000
        - --networkid=12159
        - --override.osaka=1864841831
        - --override.bpo1=1864841831
        - --override.bpo2=1864841831
```

API server with basic auth and GitHub OAuth:

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

# Minimal client config (required by config loader but not used by the API server).
client:
  instances:
    - id: placeholder
      client: geth
```
