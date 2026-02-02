# Configuration Reference

This document describes all configuration options for benchmarkoor. The [config.example.yaml](../config.example.yaml) also has a lot of information.

## Table of Contents

- [Overview](#overview)
- [Environment Variables](#environment-variables)
- [Configuration Merging](#configuration-merging)
- [Global Settings](#global-settings)
- [Benchmark Settings](#benchmark-settings)
- [Client Settings](#client-settings)
  - [Client Defaults](#client-defaults)
  - [Data Directories](#data-directories)
  - [Client Instances](#client-instances)
- [Resource Limits](#resource-limits)
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
| `system_resource_collection_enabled` | bool | `true` | Enable CPU/memory/disk metrics collection via cgroups/Docker Stats API |
| `generate_results_index` | bool | `false` | Generate `index.json` aggregating all run metadata |
| `generate_suite_stats` | bool | `false` | Generate `stats.json` per suite for UI heatmaps |
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
      github_release: benchmark@v0.0.6
      fixtures_subdir: fixtures/blockchain_tests_engine_x
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `github_repo` | string | Yes | - | GitHub repository (e.g., `ethereum/execution-spec-tests`) |
| `github_release` | string | Yes* | - | Release tag (e.g., `benchmark@v0.0.6`) |
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

*Either `github_release` or `fixtures_artifact_name` is required.

**Key features:**
- Automatically downloads and caches fixtures from GitHub releases or artifacts
- Converts EEST fixture format to `engine_newPayloadV{1-4}` + `engine_forkchoiceUpdatedV{1,3}` calls
- Only includes fixtures with `fixture-format: blockchain_test_engine_x`
- Auto-resolves genesis files per client type from the release/artifact

**Genesis file resolution:**

When using EEST fixtures, genesis files are automatically resolved from the release/artifact based on client type. You don't need to configure `client.config.genesis` unless you want to override the defaults.

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
        github_release: benchmark@v0.0.6
```

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
    rollback_strategy: "none"  # or "rpc-debug-setHead"
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
| `rollback_strategy` | string | `none` | Rollback strategy after each test (see below) |
| `resource_limits` | object | - | Container resource constraints (see [Resource Limits](#resource-limits)) |
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
| `none` | Do not rollback (default) |
| `rpc-debug-setHead` | Capture block info before each test, then rollback via a client-specific debug RPC after the test completes |
| `container-recreate` | Stop and remove the container after each test, then create and start a fresh one. The data volume/datadir persists between tests |
| `container-checkpoint` | Create a CRIU checkpoint after initial RPC readiness, then restore from it before each test. Requires Docker experimental mode and CRIU installed on the host |

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

##### `container-checkpoint`

When `container-checkpoint` is enabled:

1. After the initial RPC readiness and wait period, a CRIU checkpoint is created (this stops the container).
2. Before each test (including the first), the container is restored from the checkpoint.
3. The runner waits for the RPC endpoint to become ready before running the test.
4. After all tests, the checkpoint is cleaned up.

**Requirements:**
- Docker must be running with experimental mode enabled (`"experimental": true` in `/etc/docker/daemon.json`)
- CRIU must be installed on the host system (`apt install criu` on Debian/Ubuntu)

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
| `resource_limits` | object | No | From `client.config` | Instance-specific resource limits |

## Resource Limits

Resource limits can be configured globally (`client.config.resource_limits`) or per-instance (`client.instances[].resource_limits`). Instance-level settings override global defaults.

```yaml
resource_limits:
  cpuset_count: 4
  # OR
  cpuset: [0, 1, 2, 3]
  memory: "16g"
  swap_disabled: true
```

| Option | Type | Description |
|--------|------|-------------|
| `cpuset_count` | int | Number of random CPUs to pin to (new selection each run) |
| `cpuset` | []int | Specific CPU IDs to pin to |
| `memory` | string | Memory limit with unit: `b`, `k`, `m`, `g` (e.g., `"16g"`, `"4096m"`) |
| `swap_disabled` | bool | Disable swap (sets memory-swap equal to memory, swappiness to 0) |

**Note:** `cpuset_count` and `cpuset` are mutually exclusive. Use one or the other.

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
        github_release: benchmark@v0.0.6

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
