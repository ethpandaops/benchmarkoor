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

Tests can be loaded from a local directory or a git repository. Only one source type can be configured.

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
| `resource_limits` | object | - | Container resource constraints (see [Resource Limits](#resource-limits)) |
| `genesis` | map | - | Genesis file URLs keyed by client type |

#### Drop Memory Caches

This Linux-only feature (requires root) drops page cache, dentries, and inodes between benchmark phases for more consistent results.

| Value | Description |
|-------|-------------|
| `disabled` | Do not drop caches (default) |
| `tests` | Drop caches between tests |
| `steps` | Drop caches between all steps (setup, test, cleanup) |

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

Running stateful tests on a geth container with an existing data directoy:

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
