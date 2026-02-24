# Configuration Reference

This document describes all configuration options for benchmarkoor. The [config.example.yaml](../config.example.yaml) also has a lot of information.

## Table of Contents

- [Overview](#overview)
- [Environment Variables](#environment-variables)
- [Configuration Merging](#configuration-merging)
- [Global Settings](#global-settings)
- [Runner Settings](#runner-settings)
  - [Container Runtime](#container-runtime)
  - [Metadata Labels](#metadata-labels)
  - [Benchmark Settings](#benchmark-settings)
    - [Results Upload](#results-upload)
  - [Client Settings](#client-settings)
    - [Client Defaults](#client-defaults)
    - [Data Directories](#data-directories)
  - [Client Instances](#client-instances)
- [Resource Limits](#resource-limits)
- [Post-Test RPC Calls](#post-test-rpc-calls)
- [API Server](api.md)
- [Examples](#examples)

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
runner:
  benchmark:
    results_dir: ${RESULTS_DIR:-./results}
```

### Environment Variable Overrides

Configuration values can also be overridden via environment variables with the `BENCHMARKOOR_` prefix. The variable name is derived from the config path using underscores:

| Config Path | Environment Variable |
|-------------|---------------------|
| `global.log_level` | `BENCHMARKOOR_GLOBAL_LOG_LEVEL` |
| `runner.benchmark.results_dir` | `BENCHMARKOOR_RUNNER_BENCHMARK_RESULTS_DIR` |
| `runner.client.config.jwt` | `BENCHMARKOOR_RUNNER_CLIENT_CONFIG_JWT` |

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
```

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `log_level` | string | `info` | Logging level: `debug`, `info`, `warn`, `error` |

## Runner Settings

The `runner` section contains all run-specific settings including benchmark configuration, client settings, and instance definitions.

```yaml
runner:
  container_runtime: docker
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
| `container_runtime` | string | `docker` | Container runtime to use: `docker` or `podman`. See [Container Runtime](#container-runtime) |
| `client_logs_to_stdout` | bool | `false` | Stream client container logs to stdout |
| `docker_network` | string | `benchmarkoor` | Docker network name for containers |
| `cleanup_on_start` | bool | `false` | Remove leftover containers/networks on startup |
| `directories.tmp_datadir` | string | system temp | Directory for temporary datadir copies |
| `directories.tmp_cachedir` | string | `~/.cache/benchmarkoor` | Directory for executor cache (git clones, etc.) |
| `drop_caches_path` | string | `/proc/sys/vm/drop_caches` | Path to Linux drop_caches file (for containerized environments) |
| `cpu_sysfs_path` | string | `/sys/devices/system/cpu` | Base path for CPU sysfs files (for containerized environments where `/sys` is read-only and the host path is bind-mounted elsewhere, e.g., `/host_sys_cpu`) |
| `metadata.labels` | map[string]string | - | Arbitrary key-value labels attached to the run (see [Metadata Labels](#metadata-labels)) |
| `github_token` | string | - | GitHub token for downloading Actions artifacts via REST API. Not needed if `gh` CLI is installed and authenticated. Requires `actions:read` scope. Can also be set via `BENCHMARKOOR_RUNNER_GITHUB_TOKEN` env var |

#### Container Runtime

Benchmarkoor supports both Docker and Podman as container runtimes. The runtime is selected via the `container_runtime` field.

| Value | Description |
|-------|-------------|
| `docker` | Use Docker (default) |
| `podman` | Use Podman. Required for `container-checkpoint-restore` rollback strategy. Connects via `/run/podman/podman.sock` |

When using Podman, ensure the Podman socket is active:

```bash
sudo systemctl start podman.socket
```

#### Metadata Labels

The `runner.metadata.labels` field attaches arbitrary key-value pairs to a benchmark run. Labels are included in the output `config.json` and can be used for filtering and organization (e.g., in the UI or CI pipelines).

```yaml
runner:
  metadata:
    labels:
      env: production
      team: platform
```

Labels can also be set (or overridden) via the CLI flag `--metadata.label`:

```bash
benchmarkoor run --config config.yaml \
  --metadata.label=env=production \
  --metadata.label=team=platform
```

When the same key is set in both the config file and the CLI, the CLI value wins.

### Benchmark Settings

The `runner.benchmark` section configures test execution and results output.

```yaml
runner:
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

#### Options

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

#### Test Sources

Tests can be loaded from a local directory, a git repository, or EEST (Ethereum Execution Spec Tests) fixtures. Only one source type can be configured.

##### Local Source

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

##### Git Source

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

##### EEST Fixtures Source

EEST (Ethereum Execution Spec Tests) fixtures can be loaded from GitHub releases or GitHub Actions artifacts. This source type downloads fixtures from `ethereum/execution-spec-tests` and converts them to Engine API calls automatically.

###### From GitHub Releases

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

###### From GitHub Actions Artifacts

As an alternative to releases, you can download fixtures directly from GitHub Actions workflow artifacts. This is useful for testing with fixtures from CI builds before they're released.

**Requirements:** Either the `gh` CLI must be installed and authenticated with GitHub, or `runner.github_token` must be set (a token with `actions:read` scope).

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

###### From Local Directories

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

###### From Local Tarballs

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

When using EEST fixtures, genesis files are automatically resolved based on client type. You don't need to configure `runner.client.config.genesis` unless you want to override the defaults.

| Client | Genesis Path |
|--------|--------------|
| geth, erigon, reth, nimbus | `go-ethereum/genesis.json` |
| nethermind | `nethermind/chainspec.json` |
| besu | `besu/genesis.json` |

**Example with filter:**

```yaml
runner:
  benchmark:
    tests:
      filter: "bn128"  # Only run tests matching "bn128"
      source:
        eest_fixtures:
          github_repo: ethereum/execution-spec-tests
          github_release: benchmark@v0.0.7
```

#### Results Upload

The `runner.benchmark.results_upload` section configures automatic uploading of results to remote storage after each instance run. Currently only S3-compatible storage is supported.

```yaml
runner:
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

### Client Settings

The `runner.client` section configures Ethereum execution clients.

#### Supported Clients

| Client | Type | Default Image |
|--------|------|---------------|
| Geth | `geth` | `ethpandaops/geth:performance` |
| Nethermind | `nethermind` | `ethpandaops/nethermind:performance` |
| Besu | `besu` | `ethpandaops/besu:performance` |
| Erigon | `erigon` | `ethpandaops/erigon:performance` |
| Nimbus | `nimbus` | `statusim/nimbus-eth1:performance` |
| Reth | `reth` | `ethpandaops/reth:performance` |

#### Client Defaults

The `runner.client.config` section sets defaults applied to all client instances.

```yaml
runner:
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
| `checkpoint_restore_strategy_options` | object | - | Options for the checkpoint-restore rollback strategy (see [Checkpoint Restore Strategy Options](#checkpoint-restore-strategy-options)) |
| `wait_after_rpc_ready` | string | - | Duration to wait after RPC becomes ready (see below) |
| `retry_new_payloads_syncing_state` | object | - | Retry config for SYNCING responses (see below) |
| `resource_limits` | object | - | Container resource constraints (see [Resource Limits](#resource-limits)) |
| `post_test_rpc_calls` | []object | - | Arbitrary RPC calls to execute after each test step (see [Post-Test RPC Calls](#post-test-rpc-calls)) |
| `bootstrap_fcu` | bool/object | - | Send an `engine_forkchoiceUpdatedV3` after RPC is ready to confirm the client is fully synced (see [Bootstrap FCU](#bootstrap-fcu)) |
| `genesis` | map | - | Genesis file URLs keyed by client type |

##### Drop Memory Caches

This Linux-only feature (requires root) drops page cache, dentries, and inodes between benchmark phases for more consistent results.

| Value | Description |
|-------|-------------|
| `disabled` | Do not drop caches (default) |
| `tests` | Drop caches between tests |
| `steps` | Drop caches between all steps (setup, test, cleanup) |

##### Rollback Strategy

Controls whether the client state is rolled back after each test. This is useful for stateful benchmarks where tests modify chain state and you want each test to start from the same block.

| Value | Description |
|-------|-------------|
| `none` | Do not rollback |
| `rpc-debug-setHead` | Capture block info before each test, then rollback via a client-specific debug RPC after the test completes (default) |
| `container-recreate` | Stop and remove the container after each test, then create and start a fresh one |
| `container-checkpoint-restore` | Use Podman's CRIU-based checkpoint/restore to snapshot container memory state and the data directory, then instantly restore both per-test. Requires `container_runtime: "podman"`. When `datadir.method: "zfs"` is configured, uses ZFS snapshots for rollback. Without a datadir, uses copy-based rollback (`cp -a` snapshot, `rsync --delete` restore). Other `datadir.methods` are not supported.|

###### `rpc-debug-setHead`

When `rpc-debug-setHead` is enabled, the following happens for each test:

1. Before the test, `eth_getBlockByNumber("latest", false)` is called to capture the current block number and hash.
2. The test (including setup and cleanup steps) runs normally.
3. After the test, a client-specific rollback RPC call is made.
4. The rollback is verified by calling `eth_getBlockByNumber("latest", false)` again and comparing the block number.

If the rollback fails or the block number doesn't match, a warning is logged but the test is not marked as failed.

###### Client-specific RPC calls

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

###### `container-recreate`

When `container-recreate` is enabled, the runner manages the per-test loop:

1. The first test runs against the original container.
2. After each test, the container is stopped and removed.
3. A new container is created and started with the same configuration. The data volume/datadir persists.
4. The runner waits for the RPC endpoint to become ready and the configured wait period before running the next test.

This strategy works with all clients since it doesn't require any client-specific RPC support.

###### `container-checkpoint-restore`

When `container-checkpoint-restore` is enabled, the runner uses Podman's native CRIU-based checkpoint/restore to eliminate per-test container lifecycle overhead. This is significantly faster than `container-recreate` for large test suites because the client process resumes mid-execution without restart or RPC polling.

Two data-directory rollback modes are supported:
- **ZFS snapshots** (when `datadir.method: "zfs"` is configured): instant copy-on-write rollback.
- **Copy-based** (when no datadir is configured, e.g., EEST tests): `cp -a` snapshot, `rsync --delete` restore. The data directory is bind-mounted from a host temp directory.

**Requirements:**
- `container_runtime: "podman"` must be set
- CRIU must be installed on the host
- Podman must be running as root (rootful mode)
- If a datadir is configured, it must use `method: "zfs"`

**Flow:**

1. The container starts and the runner waits for the RPC endpoint to become ready.
2. After RPC is ready (and any configured wait period), the data directory is snapshotted (ZFS snapshot or file copy) and the container is checkpointed (memory state exported to a file). The container stops.
3. For each test:
   - The data directory is rolled back to the snapshot (ZFS rollback or rsync restore).
   - The container is restored from the checkpoint. The client process resumes at the exact point it was checkpointed — **no startup, no RPC polling**.
   - The test executes.
   - The restored container is stopped and removed.
4. After all tests, the snapshot and checkpoint export file are cleaned up.

**With ZFS datadir:**

```yaml
runner:
  container_runtime: podman
  client:
    config:
      rollback_strategy: container-checkpoint-restore
    datadirs:
      geth:
        source_dir: /tank/data/geth
        method: zfs
  instances:
    - id: geth
      client: geth
```

**Without datadir (e.g., EEST tests):**

```yaml
runner:
  container_runtime: podman
  client:
    config:
      rollback_strategy: container-checkpoint-restore
  instances:
    - id: geth
      client: geth
```

##### Checkpoint Restore Strategy Options

Options for the `container-checkpoint-restore` rollback strategy, nested under `checkpoint_restore_strategy_options`:

| Sub-option | Type | Default | Description |
|-----------|------|---------|-------------|
| `tmpfs_threshold` | string | - | Store checkpoint on tmpfs (RAM) when container memory is under this threshold. Uses the same format as `resource_limits.memory` (Docker go-units): e.g., `"8g"`, `"512m"`, `"1024k"`, or raw bytes. If not set, checkpoints are always stored on disk. |
| `tmpfs_max_size` | string | 2× `tmpfs_threshold` | Maximum size of the tmpfs mount for checkpoint storage. Same format as `tmpfs_threshold` (e.g., `"16g"`, `"1024m"`). When not set, defaults to twice the `tmpfs_threshold` value. |
| `wait_after_tcp_drop_connections` | string | `10s` | How long to wait after dropping TCP connections before checkpointing, giving the process time to close file descriptors (Go duration string). |
| `restart_container` | bool | `false` | Whether to restart the container before taking a CRIU checkpoint. Restarting ensures a clean process state (cold caches, clean DB shutdown). |

```yaml
runner:
  client:
    config:
      rollback_strategy: container-checkpoint-restore
      checkpoint_restore_strategy_options:
        tmpfs_threshold: "8g"
        tmpfs_max_size: "16g"
        wait_after_tcp_drop_connections: "10s"
        restart_container: false
```

##### Wait After RPC Ready

Some clients (e.g., Erigon) have internal sync pipelines that continue running after their RPC endpoint becomes available. The `wait_after_rpc_ready` option adds a configurable delay after the RPC health check passes, giving the client time to complete internal initialization before test execution begins.

```yaml
runner:
  client:
    config:
      wait_after_rpc_ready: 30s
```

The value is a Go duration string (e.g., `30s`, `1m`, `500ms`). If not set, no additional wait is performed.

**When to use:**
- When running benchmarks against clients with staged sync pipelines (Erigon)
- When you observe `SYNCING` responses from Engine API calls despite the RPC being available
- When starting from pre-populated data directories where clients may need time to validate state

##### Retry New Payloads Syncing State

When `engine_newPayload` returns a `SYNCING` status, it indicates the client hasn't fully processed the parent block yet. The `retry_new_payloads_syncing_state` option configures automatic retries with exponential backoff.

```yaml
runner:
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


##### Bootstrap FCU

Some clients (e.g., Erigon) may still be performing internal initialization or syncing after their RPC endpoint becomes available. The `bootstrap_fcu` option sends an `engine_forkchoiceUpdatedV3` call in a retry loop after RPC is ready, using the latest block hash from `eth_getBlockByNumber("latest")`. The client accepting the FCU with `VALID` status confirms it has finished syncing and is ready for test execution.

**Shorthand** (uses defaults: `max_retries: 30`, `backoff: 1s`):

```yaml
runner:
  client:
    config:
      bootstrap_fcu: true
```

**Full configuration:**

```yaml
runner:
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

When using the `container-recreate` rollback strategy, the bootstrap FCU is sent after each container recreate. When using `container-checkpoint-restore`, the bootstrap FCU is sent once before the checkpoint is taken.

**When to use:**
- When clients may still be performing internal initialization or syncing after RPC becomes available (e.g., Erigon's staged sync)
- When starting from pre-populated data directories where the client needs time to validate state before processing Engine API requests
- When you observe test failures due to the client returning errors or SYNCING responses on the first Engine API calls

#### Data Directories

The `runner.client.datadirs` section configures pre-populated data directories per client type. When configured, the init container is skipped and data is mounted directly.

```yaml
runner:
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

##### Data Directory Methods

| Method | Description | Requirements |
|--------|-------------|--------------|
| `copy` | Parallel Go copy with progress display | None (default, works everywhere) |
| `overlayfs` | Linux overlayfs for near-instant setup | Root access |
| `fuse-overlayfs` | FUSE-based overlayfs | `fuse-overlayfs` package; `user_allow_other` in `/etc/fuse.conf` if Docker runs as root. **Warning:** ~3x slower than native overlayfs |
| `zfs` | ZFS snapshots and clones for copy-on-write setup | Source directory on ZFS filesystem; root access or ZFS delegations configured |

###### ZFS Setup

For ZFS method without root:
```bash
zfs allow -u <user> clone,create,destroy,mount,snapshot <dataset>
```

The dataset is auto-detected from the source directory mount point.

###### Default Container Directories

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

The `runner.instances` array defines which client configurations to benchmark.

```yaml
runner:
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
| `genesis` | string | No | From `runner.client.config.genesis` | Override genesis file URL |
| `datadir` | object | No | From `runner.client.datadirs` | Instance-specific data directory config |
| `drop_memory_caches` | string | No | From `runner.client.config` | Instance-specific cache drop setting |
| `rollback_strategy` | string | No | From `runner.client.config` | Instance-specific rollback strategy |
| `checkpoint_restore_strategy_options` | object | No | From `runner.client.config` | Instance-specific checkpoint-restore strategy options (replaces global) |
| `wait_after_rpc_ready` | string | No | From `runner.client.config` | Instance-specific RPC ready wait duration |
| `retry_new_payloads_syncing_state` | object | No | From `runner.client.config` | Instance-specific retry config for SYNCING responses |
| `resource_limits` | object | No | From `runner.client.config` | Instance-specific resource limits |
| `post_test_rpc_calls` | []object | No | From `runner.client.config` | Instance-specific post-test RPC calls (replaces global) |
| `bootstrap_fcu` | bool/object | No | From `runner.client.config` | Instance-specific bootstrap FCU setting |

## Resource Limits

Resource limits can be configured globally (`runner.client.config.resource_limits`) or per-instance (`runner.instances[].resource_limits`). Instance-level settings override global defaults.

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
- When running in Docker, bind-mount `/sys/devices/system/cpu` into the container and set `runner.cpu_sysfs_path` to the mount point (e.g., `/host_sys_cpu`)

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
runner:
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
runner:
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
runner:
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

See [API Server documentation](api.md) for the full reference on the `api` config section, including server settings, authentication, database, storage, endpoints, and UI integration.

## Examples

Running stateless tests across all clients:

```yaml
global:
  log_level: info

runner:
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

runner:
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

runner:
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

runner:
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

For API server examples, see the [API Server documentation](api.md#example).
