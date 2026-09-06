# Configuration Reference

This document describes all configuration options for benchmarkoor. The [config.example.yaml](../config.example.yaml) also has a lot of information.

## Table of Contents

- [Overview](#overview)
- [Environment Variables](#environment-variables)
  - [Config-local variables (`global.env`)](#config-local-variables-globalenv)
  - [Environment Variable Overrides](#environment-variable-overrides)
- [Configuration Merging](#configuration-merging)
- [Global Settings](#global-settings)
- [Runner Settings](#runner-settings)
  - [Container Runtime](#container-runtime)
  - [Metadata Labels](#metadata-labels)
  - [Runner Run Timeout](#runner-run-timeout)
  - [Benchmark Settings](#benchmark-settings)
    - [Suite Metadata Labels](#suite-metadata-labels)
    - [Results Upload](#results-upload)
  - [Client Settings](#client-settings)
    - [Client Defaults](#client-defaults)
    - [Data Directories](#data-directories)
  - [Client Instances](#client-instances)
- [Resource Limits](#resource-limits)
- [Post-Test RPC Calls](#post-test-rpc-calls)
- [Builder](#builder)
  - [`builder.pre_runs` options](#builderpre_runs-options)
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

### Config-local variables (`global.env`)

`global.env` declares variables inside the config itself, available to the same `${VAR}` / `${VAR:-default}` substitution everywhere in the file. This keeps a config self-contained — no need to `export` a value before running — while preserving the single-point-of-edit indirection:

```yaml
global:
  env:
    STATE_DIR: /tmp/benchmarkoor/state-actor/simple-amsterdam-compute
builder:
  state_actor:
    targets:
      - client: geth
        output_dir: ${STATE_DIR}/geth   # → /tmp/benchmarkoor/state-actor/simple-amsterdam-compute/geth
```

Resolution order for any `${VAR}` is **shell environment → `global.env` → inline `:-default`**. A real environment variable of the same name therefore still wins, so `global.env` acts as a per-config default that CI or an ad-hoc `VAR=… benchmarkoor …` invocation can override. A `global.env` value may itself reference the shell environment (e.g. `${BASE:-/tmp}/state-actor`); values do not reference one another.

### Environment Variable Overrides

Configuration values can also be overridden via environment variables with the `BENCHMARKOOR_` prefix. The variable name is derived from the config path using underscores:

| Config Path | Environment Variable |
|-------------|---------------------|
| `global.log_level` | `BENCHMARKOOR_GLOBAL_LOG_LEVEL` |
| `builder.run_timeout` | `BENCHMARKOOR_BUILDER_RUN_TIMEOUT` |
| `builder.cleanup_on_start` | `BENCHMARKOOR_BUILDER_CLEANUP_ON_START` |
| `runner.run_timeout` | `BENCHMARKOOR_RUNNER_RUN_TIMEOUT` |
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
  env:
    STATE_DIR: /tmp/benchmarkoor/state-actor/my-config
  directories:
    cachedir: ~/.cache/benchmarkoor
```

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `log_level` | string | `info` | Logging level: `debug`, `info`, `warn`, `error` |
| `env` | map[string]string | – | Config-local variables for `${VAR}` substitution; a per-config default that a shell env var of the same name still overrides. See [Config-local variables](#config-local-variables-globalenv). |
| `directories.cachedir` | string | `~/.cache/benchmarkoor` | On-disk cache shared by both commands: executor git/archive clones (`run`) and the EEST repo clone (`build`). |

## Runner Settings

The `runner` section contains all run-specific settings including benchmark configuration, client settings, and instance definitions.

```yaml
runner:
  container_runtime: docker
  client_logs_to_stdout: true
  container_network: benchmarkoor
  cleanup_on_start: false
  run_timeout: 4h
  directories:
    tmp_datadir: /tmp/benchmarkoor
  drop_caches_path: /proc/sys/vm/drop_caches
  cpu_sysfs_path: /sys/devices/system/cpu
```

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `container_runtime` | string | `docker` | Container runtime to use: `docker` or `podman`. See [Container Runtime](#container-runtime) |
| `client_logs_to_stdout` | bool | `false` | Stream client container logs to stdout |
| `container_network` | string | `benchmarkoor` | Container network name |
| `cleanup_on_start` | bool | `false` | Remove leftover containers/networks on startup |
| `run_timeout` | string | - | Global timeout for the entire run covering all instances, setup, and teardown. Uses Go duration format (e.g., `4h`, `30m`). See [Runner Run Timeout](#runner-run-timeout) |
| `directories.tmp_datadir` | string | system temp | Directory for temporary datadir copies. (The shared cache dir is `global.directories.cachedir`.) |
| `drop_caches_path` | string | `/proc/sys/vm/drop_caches` | Path to Linux drop_caches file (for containerized environments) |
| `cpu_sysfs_path` | string | `/sys/devices/system/cpu` | Base path for CPU sysfs files (for containerized environments where `/sys` is read-only and the host path is bind-mounted elsewhere, e.g., `/host_sys_cpu`) |
| `metadata.labels` | map[string]string | - | Arbitrary key-value labels attached to the run (see [Metadata Labels](#metadata-labels)) |
| `github_token` | string | - | GitHub token for downloading Actions artifacts via REST API. Not needed if `gh` CLI is installed and authenticated. Requires `actions:read` scope. Can also be set via `BENCHMARKOOR_RUNNER_GITHUB_TOKEN` env var |
| `live_reporting` | object | - | Stream periodic run-status reports to a benchmarkoor API instance so the UI can display in-progress runs. See [Live Reporting](#live-reporting) |

#### Live Reporting

When `live_reporting` is enabled, every run posts a snapshot of its current state (status, test counts, metadata labels) to the configured benchmarkoor API at a jittered interval. The API stores these in a separate `live_runs` table and the UI merges them into the runs view as ephemeral rows. Once the on-disk indexer picks up the same run from storage, the live entry is removed automatically.

```yaml
runner:
  live_reporting:
    enabled: true
    endpoint: https://benchmarkoor.example.com
    token: my-shared-secret
    discovery_path: my-host/benchmarks  # must match a discovery path on the API side
    interval: 1m
    jitter_fraction: 0.2
    timeout: 10s
    logs_enabled: true     # default true; set false to disable the log streamer
    logs_interval: 200ms   # file-tail push cadence while streaming
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable live reporting |
| `endpoint` | string | - | Base URL of the benchmarkoor API (no trailing path), e.g. `https://api.example.com` |
| `token` | string | - | Shared bearer token; must match `api.ingest.token` on the API side |
| `discovery_path` | string | - | Discovery path the runner's results will be published under. Used as part of the unique key on the API |
| `interval` | string | `1m` | Base reporting interval (Go duration). Each tick adds random jitter |
| `jitter_fraction` | float | `0.2` | Random jitter as a fraction of `interval`. `0` uses the default; negative disables jitter entirely |
| `timeout` | string | `10s` | Per-request HTTP timeout |
| `logs_enabled` | bool | `true` | Enable live log streaming. When enabled, the runner opens a WebSocket to the API for the lifetime of the run. Log bytes only flow while at least one UI client has the log panel open — zero traffic otherwise |
| `logs_interval` | string | `200ms` | How often the runner reads new bytes from `benchmarkoor.log` and pushes them over the WebSocket while streaming is active. Lower values feel smoother; the overhead is negligible since empty ticks don't send a message |

Reports are best-effort: HTTP failures are logged at WARN and dropped. The next tick will retry with the latest snapshot. On `Stop()`, the runner sends one final synchronous report so the terminal status reaches the API.

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

The `runner.client.config.metadata.labels` field attaches arbitrary key-value pairs to benchmark runs. Labels are included in each run's output `config.json` and can be used for filtering and organization (e.g., in the UI or CI pipelines).

Labels can be set at the client level (defaults for all instances) and overridden per instance. Instance-level labels are merged with client-level labels, with instance values taking precedence on conflict.

```yaml
runner:
  client:
    config:
      metadata:
        labels:
          env: production
          team: platform
  instances:
    - id: geth-latest
      client: geth
      metadata:
        labels:
          env: staging      # overrides client-level "env"
          variant: snap-sync  # additional instance-specific label
```

In this example, `geth-latest` runs will have labels `env=staging`, `team=platform`, and `variant=snap-sync`.

Labels can also be set (or overridden) at the client level via the CLI flag `--metadata.label`:

```bash
benchmarkoor run --config config.yaml \
  --metadata.label=env=production \
  --metadata.label=team=platform
```

When the same key is set in both the config file and the CLI, the CLI value wins.

#### Runner Run Timeout

The `runner.run_timeout` option sets a global timeout for the entire benchmark run. Unlike the per-instance `runner.client.config.run_timeout` which only applies to individual instance execution, this timeout caps everything — all instances, setup, and teardown — starting from when the run begins.

```yaml
runner:
  run_timeout: 4h
```

When the timeout is reached, the run context is cancelled and no further instances will be started. Per-instance S3 uploads use an independent context and will still complete. Results collected before the timeout are preserved on disk.

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
| `tests.filter` | string | - | Run only tests whose name (or file path) matches this pattern. Plain values match by substring; values prefixed with `regex:` match the trailing expression as a Go regular expression. See [Test Filter](#test-filter) |
| `tests.metadata.labels` | map[string]string | - | Arbitrary key-value labels for the test suite (see [Suite Metadata Labels](#suite-metadata-labels)) |
| `tests.source` | object | - | Test source configuration (see below) |

#### Suite Metadata Labels

The `runner.benchmark.tests.metadata.labels` field attaches arbitrary key-value pairs to a test suite. Labels are written to the suite's `summary.json` and displayed in the UI.

The special `name` label is used as the display name for the suite throughout the UI (breadcrumbs, tables, detail pages) instead of the suite hash.

```yaml
runner:
  benchmark:
    tests:
      metadata:
        labels:
          name: "EIP-7934 BN128 Benchmarks"
          category: precompile
      source:
        # ...
```

> **Note:** Labels do not affect the suite hash. The hash is computed from test file contents only, so changing labels does not create a new suite.

#### Test Sources

Tests can be loaded from a local directory, a git repository, an archive file, or EEST (Ethereum Execution Spec Tests) fixtures. Only one source type can be configured.

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

##### Archive Source

Tests can be loaded from a ZIP or tar.gz archive file, either from a local path or a URL (including GitHub Actions artifacts).

```yaml
tests:
  source:
    archive:
      file: https://github.com/NethermindEth/gas-benchmarks/actions/runs/23847558369/artifacts/6222084759
      pre_run_steps:
        - "perf-devnet-3/gas-bump.txt"
        - "perf-devnet-3/funding.txt"
      steps:
        setup:
          - "perf-devnet-3/setup/*.txt"
        test:
          - "perf-devnet-3/testing/*.txt"
    # Optional: External opcode metadata for the test suite.
    # A JSON file mapping test names to opcode counts.
    # Can be a local path or URL.
    opcode_source:
      file: opcodes_tracing.json
```

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `file` | string | One of `file`/`parts` | Local path or URL to a ZIP or tar.gz archive. GitHub Actions artifact URLs are auto-converted to API endpoints |
| `parts` | []string | One of `file`/`parts` | Ordered list of local paths or URLs to concatenate into the final archive. Useful when the archive is split because of per-asset size limits. Mutually exclusive with `file` |
| `pre_run_steps` | []string | No | Glob patterns for steps executed once before all tests |
| `steps.setup` | []string | No | Glob patterns for setup phase files |
| `steps.test` | []string | No | Glob patterns for test phase files |
| `steps.cleanup` | []string | No | Glob patterns for cleanup phase files |

**Multi-part archives:** when an archive is too large for a single asset upload, `parts` accepts an ordered list of URLs or local paths. All parts are downloaded (with caching) and concatenated into a single file before extraction:

```yaml
tests:
  source:
    archive:
      parts:
        - https://github.com/org/repo/releases/download/v1.0.0/tests.tar.gz.00.part
        - https://github.com/org/repo/releases/download/v1.0.0/tests.tar.gz.01.part
      steps:
        test:
          - "testing/*.txt"
```

##### Opcode Source

Optional external opcode metadata can be configured alongside the test source. Two modes are supported.

**Direct JSON file** — `file` is a local path or URL to the JSON file:

```yaml
runner:
  benchmark:
    tests:
      opcode_source:
        file: opcodes_tracing.json  # Local path or URL to a JSON file
```

**Archive mode** — `archive` is a `.zip` / `.tar.gz` (or a GitHub Actions artifact URL) that contains the JSON file; `file` is the filename to look up inside the extracted archive:

```yaml
runner:
  benchmark:
    tests:
      opcode_source:
        archive: https://github.com/NethermindEth/gas-benchmarks/actions/runs/24460911828/artifacts/6456466898
        file: opcodes_tracing.json  # Filename inside the archive
```

`archive` can also be a plain URL to a `.zip` / `.tar.gz`, or a local path to one. When `archive` is set, `file` is interpreted as a filename inside the extracted tree (matched by basename, so nested folders are walked automatically).

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `file` | string | Yes | When `archive` is unset: local path or URL to the JSON file. When `archive` is set: filename to look up inside the extracted archive |
| `archive` | string | No | Optional local path or URL to a `.zip` / `.tar.gz` / GitHub Actions artifact containing the opcode JSON file. When set, `file` names the entry inside the archive |

**GitHub Actions artifacts:** Browser URLs like `https://github.com/{owner}/{repo}/actions/runs/{run_id}/artifacts/{artifact_id}` are automatically converted to the GitHub API download endpoint. A GitHub token is required for artifact downloads (set via `runner.github_token` or `BENCHMARKOOR_RUNNER_GITHUB_TOKEN`).

**Archive extraction:** ZIP archives are extracted and any inner tarballs (common in GitHub Actions artifacts) are automatically extracted as well. Both direct-file and archive downloads are cache-validated on each run via HTTP `ETag` / `Last-Modified` — the archive (and its extraction) is refreshed automatically when the origin changes.

##### EEST Fixtures Source

EEST (Ethereum Execution Spec Tests) fixtures can be loaded from GitHub releases or GitHub Actions artifacts. This source type downloads fixtures from `ethereum/execution-spec-tests` and converts them to Engine API calls automatically.

###### From GitHub Releases

```yaml
tests:
  source:
      eest_fixtures:
        github_repo: ethereum/execution-specs
        github_release: tests-benchmark@v0.0.9
      fixtures_subdir: fixtures/blockchain_tests_engine_x
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `github_repo` | string | Yes* | - | GitHub repository (e.g., `ethereum/execution-specs`). Required for release/artifact modes; not needed for `fixtures_url`. |
| `github_release` | string | Yes* | - | Release tag (e.g., `test-benchmark@v0.0.9`) |
| `fixtures_subdir` | string | No | `fixtures/blockchain_tests_engine_x` | Subdirectory within the extracted fixtures to search |
| `fixtures_url` | string | No | Auto-generated | **Standalone source** (when `github_release` is unset): a release/plain `.tar.gz` URL, **or** a GitHub Actions artifact URL (`github.com/<owner>/<repo>/actions/runs/<run>/artifacts/<id>` — needs `runner.github_token`; its inner `.tar.gz` is auto-extracted). No genesis is fetched. Combine with `fixtures_subdir`. When `github_release` is set, it instead overrides the release tarball URL. |
| `genesis_url` | string | No | Auto-generated | Override URL for genesis tarball (release mode) |

*One of `github_release`, `fixtures_url`, or `fixtures_artifact_name` is required.

**Standalone URL example** (e.g. reusing a benchmarkoor build artifact):

```yaml
tests:
  source:
    eest_fixtures:
      fixtures_url: https://github.com/ethpandaops/benchmarkoor/actions/runs/28947560261/artifacts/8170387928
      fixtures_subdir: benchmarkoor-build-artifacts/eest-payloads/geth/blockchain_tests_stateful_engine
# runner.github_token (or BENCHMARKOOR_RUNNER_GITHUB_TOKEN) is required for artifact URLs.
```

###### Replaying a `builder.pre_runs` bundle

A `pre_runs` build records a replayable payload bundle. Point the runner at it and every instance replays it **before** the fixtures, so a raw snapshot is advanced to the setup head at run time:

```yaml
tests:
  source:
    eest_fixtures:
      local_fixtures_dir: /artifacts/eest-payloads/geth
      fixtures_subdir: blockchain_tests_stateful_engine
      pre_runs:
        local_fixtures_dir: /artifacts/pre-runs/geth   # the dir CONTAINING pre_run_bundle/
        # fixtures_subdir: pre_run_bundle              # default
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `local_fixtures_dir` | string | Yes | - | Directory containing the bundle's subdirectory |
| `fixtures_subdir` | string | No | `pre_run_bundle` | Subdirectory holding the `.request` bundle |

Before replaying, the runner compares the datadir's head against the range recorded in the bundle's `pre-run.meta.json` sidecar — **block number and hash**:

- head **at the bundle's end block** → already applied, the replay is skipped outright (not streamed-and-skipped: on a 10 GB bundle that is the difference between ~2s and ~2m40s)
- head **at its start block** → replayed in full
- head **inside the range** → the replay resumes from there
- **a hash mismatch** at either named block, or a head outside the range → the run fails, rather than benchmarking a datadir that is on a different chain

Bundles written before the sidecar existed still get the fast path: the end block is recovered by scanning the tail of the `.request` file, so no multi-GB read is needed. Only the end-block comparison is available in that case.

**Omit the `pre_runs` block** when the datadir is already advanced — e.g. the `pre_runs` builder ran in place on it, or the schelk baseline was promoted. Replaying onto a chain that already contains those blocks is wasted work at best.

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
          github_repo: ethereum/execution-specs
          github_release: tests-benchmark@v0.0.9
```

#### Test Filter

The `runner.benchmark.tests.filter` selects which tests run. Two modes:

| Mode | Syntax | Behavior |
|------|--------|----------|
| Substring (default) | `filter: "bn128"` | Test/file path must contain the literal string. Regex metacharacters are matched literally (e.g. `filter: "test.*name"` only matches paths that contain the seven-character string `test.*name`) |
| Regex | `filter: "regex:<expr>"` | The trailing expression is compiled as a [Go regular expression](https://pkg.go.dev/regexp/syntax) and tested with `MatchString`. Anchor with `^` / `$` if you need full-string matches; flags like `(?i)` for case-insensitive are supported |

Examples:

```yaml
# Substring — matches any test path containing "keccak"
filter: "keccak"

# Regex — matches "test_sstore_bloated…benchmark_300M" anywhere in the path
filter: "regex:test_sstore_bloated.*benchmark_300M"

# Regex with case-insensitive flag
filter: "regex:(?i)KECCAK|sha256"

# Regex anchored to end of path
filter: "regex:bn128_pairing\\.txt$"
```

The filter is applied to:
- file paths returned from glob expansion (substring match against the absolute path),
- EEST fixture test names,
- opcode source entries.

A bad regex (e.g. unclosed character class) is rejected at config-load time with a `runner.benchmark.tests.filter: invalid regex …` error.

#### Results Upload

The `runner.benchmark.results_upload` section configures automatic uploading of results to remote storage after each instance run. Currently only S3-compatible storage is supported.

```yaml
runner:
  benchmark:
    results_upload:
      # max_pre_run_upload_size: 512MB
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
| `max_pre_run_upload_size` | string | No | `512MB` | Cap on pre-run bundles uploaded with a suite (see [Pre-Run Upload Size](#pre-run-upload-size)). `0` uploads every bundle |

**`s3` options:**

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

#### Pre-Run Upload Size

A suite directory holds a copy of every step file so the UI can display it, and that includes the pre-run bundle. Those bundles can be enormous — a bloatnet-style setup that deploys 100k contracts produces a single `pre-run.request` of around 9.4 GiB — and nothing reads them from there: the runner replays pre-runs from its fixtures cache, not from the suite.

`max_pre_run_upload_size` caps what gets kept, and therefore what gets uploaded. A bundle over the limit is still described in `summary.json`, so the suite stays honest about what the run replayed:

```json
"pre_run_steps": [
  { "og_path": "pre_run/pre-run.request", "size_bytes": 10062313486, "omitted": true }
]
```

The size is checked before the copy, so an oversized bundle costs neither the local write nor the transfer. The bytes remain in the fixtures artifact the step came from, which is where to look if you need to inspect one.

Set `0` to upload every bundle regardless of size.

```yaml
runner:
  benchmark:
    results_upload:
      max_pre_run_upload_size: 512MB
```

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
| `run_timeout` | string | - | Maximum duration for test execution before the run is timed out (see below) |
| `retry_new_payloads_syncing_state` | object | - | Retry config for SYNCING responses (see below) |
| `retry_new_payloads_failed_state` | object | - | Retry config for any non-SYNCING `engine_newPayload*` failure (see below) |
| `resource_limits` | object | - | Container resource constraints (see [Resource Limits](#resource-limits)) |
| `post_test_rpc_calls` | []object | - | Arbitrary RPC calls to execute after each test step (see [Post-Test RPC Calls](#post-test-rpc-calls)) |
| `post_test_sleep_duration` | string | - | Sleep duration after each test, e.g. `200ms`, `1s` (see below) |
| `bootstrap_fcu` | bool/object | - | Send an `engine_forkchoiceUpdatedV3` after RPC is ready to confirm the client is fully synced (see [Bootstrap FCU](#bootstrap-fcu)) |
| `opcode_extraction` | object | - | Extract per-test opcode counts via `debug_traceBlockByNumber` after each test step (see [Opcode Extraction](#opcode-extraction)) |
| `db_compaction` | object | - | Compact the client database before the run measures anything (see [Database Compaction](#database-compaction)) |
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
| `restore_in_place` | bool | `false` | Restore the checkpointed container in place between tests instead of importing the checkpoint archive into a fresh container. The checkpoint data is kept in the container's storage directory and the same container (same name, mounts, and IP) is restored repeatedly. This skips the per-test archive extraction and container creation of the export/import path, cutting the restore latency roughly to CRIU's page-restore time (e.g. ~15s → ~3s for an ~8g checkpoint). `tmpfs_threshold` / `tmpfs_max_size` do not apply in this mode. |

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

##### Run Timeout

The `run_timeout` option sets a maximum duration for the test execution phase of a run. If the timeout is exceeded, the run is cancelled with a `timed_out` status. Partial results collected before the timeout are still written and published.

```yaml
runner:
  client:
    config:
      run_timeout: 2h
```

The value is a Go duration string (e.g., `30m`, `1h`, `2h30m`). If not set, no timeout is applied.

The timeout covers only the test execution phase — container setup, image pulling, and RPC readiness checks are not included.

> **Note:** This is a per-instance timeout. For a global timeout that caps the entire run (all instances, setup, and teardown), use [`runner.run_timeout`](#runner-run-timeout).

**When to use:**
- When running large test suites that may hang or take unexpectedly long
- When you want to enforce a maximum wall-clock time per instance
- When running in CI/CD environments with time constraints

##### Post-Test Sleep Duration

The `post_test_sleep_duration` option adds a configurable pause after each test completes (after rollback and post-test RPC calls, but before the next test begins). This is useful for clients that need time to complete internal cleanup between tests.

```yaml
runner:
  client:
    config:
      post_test_sleep_duration: 200ms
```

Uses Go duration format (e.g., `200ms`, `1s`, `5s`). Default is `0` (disabled).

**When to use:**
- When a client needs time for internal cleanup between tests
- When you observe flaky results due to rapid successive test execution

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

Both this and `retry_new_payloads_failed_state` (below) apply to **all** `engine_newPayload*` calls — pre-run steps, setup steps, and test steps alike.

##### Retry New Payloads Failed State

Catch-all retry for `engine_newPayload*` calls that fail for any reason **other than** SYNCING — RPC/network errors, JSON-RPC errors (e.g. `-32603 Server error`), `INVALID` / `INVALID_BLOCK_HASH` payload statuses, or unparsable responses. Useful for transient client-side flakiness, where a single retry usually succeeds.

```yaml
runner:
  client:
    config:
      retry_new_payloads_failed_state:
        enabled: true
        max_retries: 3
        backoff: 500ms
```

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `enabled` | bool | Yes | Enable retry behavior |
| `max_retries` | int | Yes | Maximum number of retry attempts (must be ≥ 1) |
| `backoff` | string | Yes | Delay between retries (Go duration string) |

When both `retry_new_payloads_syncing_state` and `retry_new_payloads_failed_state` are enabled, SYNCING errors take the SYNCING retry path and everything else takes the failed-state retry path.

**When to use:**
- Recovering from transient JSON-RPC errors during long pre-run replays
- Suppressing one-off failures when clients are momentarily under load (e.g. cache warm-up)


##### Bootstrap FCU

Some clients (e.g., Erigon) may still be performing internal initialization or syncing after their RPC endpoint becomes available. The `bootstrap_fcu` option sends an `engine_forkchoiceUpdatedV3` call in a retry loop after RPC is ready, using the latest block hash from `eth_getBlockByNumber("latest")`. The client accepting the FCU with `VALID` status confirms it has finished syncing and is ready for test execution.

> **Besu** accepts the bootstrap FCU on an isolated snapshot node only with `--p2p-enabled=true`: its synchronizer must run to register the post-merge head as in-sync, otherwise besu answers `SYNCING` to every FCU. Set `extra_args: [--p2p-enabled=true]` on the besu instance (`--max-peers=0` + `--discovery-enabled=false` keep it isolated, with zero real peers).

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

##### Opcode Extraction

The `opcode_extraction` option captures per-test opcode counts as a side effect of running tests. After each test step, the runner walks the test's `engine_newPayload*` calls and runs `debug_traceBlockByNumber` against each block with a JS opcode-counting tracer. Per-tx counts are summed (and uppercased) into one map per newPayload, then appended to a per-test array. At the end of the run all the data lands in a single `test-opcodes.json` at the run results dir, in the same shape that `runner.benchmark.tests.opcode_source` expects.

```yaml
runner:
  client:
    config:
      opcode_extraction:
        enabled: true
        timeout: 2m   # per-block trace timeout; default 2m
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `enabled` | bool | Yes | `false` | Enable the post-test extraction step |
| `timeout` | string | No | `2m` | Per-block `debug_traceBlockByNumber` timeout (Go duration). Long traces on fat blocks may need a higher value |

`opcode_extraction` can be set globally under `runner.client.config` and/or per-instance under `runner.instances[]`. Instance-level config (when non-nil) fully replaces the global default. The output file shape is:

```json
{
  "test-name.txt": [
    { "PUSH1": 23432, "DUP1": 11231, "SSTORE": 3321 }
  ]
}
```

(One entry per `engine_newPayload*` in the test step, summed across all txs in that block.)

**Requirements:**
- The client must accept JS tracers via `debug_traceBlockByNumber`. Geth, Erigon, and Nethermind support them; coverage on Reth/Besu/Nimbus/ethrex varies — check your client docs.
- The trace runs against the EL state right after the test step, before rollback, so the client must still have the block.

**When to use:**
- When you want a ground-truth opcode profile of every benchmarked test (instead of relying on `opcode_source` JSON shipped from a separate pipeline)
- When investigating client-vs-client divergence in EVM execution paths

##### Database Compaction

The `db_compaction` option compacts the client database before the run measures anything. Compaction needs exclusive access to the database, so the client is never running while it happens: the runner runs the client's own offline command in a one-shot container against the datadir.

Only clients that ship an offline compaction command support this. Today that is **geth** alone (`geth db compact`); every other client fails validation with a clear message.

Besu was checked and cannot be supported yet: as of Besu 26.6.1, `besu storage` has no compaction subcommand. `trie-log prune` deletes trie logs below the retention limit instead of rewriting the database, which is a different operation and removes history the Bonsai rollback needs.

```yaml
runner:
  client:
    config:
      db_compaction:
        enabled: true
        when: [before_benchmarks]   # or [before_pre_runs], or both
        inspect: true               # `geth db inspect` before and after
        timeout: 3h
        extra_args: ["--cache=16384"]
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `enabled` | bool | Yes | `false` | Enable the compaction |
| `when` | []string | No | `[before_benchmarks]` | The lifecycle points at which to compact (see below). A plain string also works |
| `inspect` | bool | No | `true` | Run the client database inspection before and after each compaction. A failed inspection is logged and never fails the run |
| `timeout` | string | No | `3h` | Cap for one phase's work — the compaction and both inspections (Go duration). Applies per phase |
| `image` | string | No | the instance image | Image of the compaction container. The default keeps the tool version and the client version identical |
| `extra_args` | []string | No | - | Extra arguments for the compaction command, e.g. `--cache=16384` |
| `continue_on_error` | bool | No | `false` | Downgrade a compaction failure to a warning. A failed compaction makes the results incomparable, so the run fails by default |
| `skip_if_marked` | bool | No | `true` with `persist`, else `false` | Skip a phase the datadir marker already names (see below) |
| `persist` | object | No | - | Write the compacted database back to the datadir baseline (see below) |

`db_compaction` can be set globally under `runner.client.config` and/or per-instance under `runner.instances[]`. Instance-level config (when non-nil) fully replaces the global default.

###### Phases

| `when` value | When it runs | Client |
|--------------|--------------|--------|
| `before_pre_runs` | After the datadir is prepared, before the client boots — so before the suite pre-run steps replay | Not yet started. No stop and restart |
| `before_benchmarks` | After the pre-run steps, before the first test | Stopped gracefully, then started again |

Both phases may be listed. The runner always executes them in lifecycle order, whatever order they are written in:

```yaml
db_compaction:
  enabled: true
  when: [before_pre_runs, before_benchmarks]
```

`before_benchmarks` is the default because the pre-run bundle writes blocks that undo most of what an earlier compaction achieved. It also lands before the state-baking step of every runner-level rollback strategy, so the compacted database goes into the ZFS ready-snapshot, the CRIU checkpoint, or the schelk promote for free.

`before_pre_runs` needs a pre-populated datadir: without one the database is created when the client boots.

###### Persisting the compacted database

Without `persist`, the compaction is run-local: it costs the same time on every run, and the clone, copy, or restored volume that holds it is destroyed at the end. `persist` writes it back to the baseline instead.

```yaml
db_compaction:
  enabled: true
  when: [before_pre_runs]
  persist:
    enabled: true
    safety_snapshot: true   # ZFS only
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `enabled` | bool | Yes | `false` | Persist the compacted database |
| `phases` | []string | No | every phase in `when` | Restrict the persist to a subset of `when` |
| `safety_snapshot` | bool | No | `true` | Snapshot the ZFS source dataset before it is compacted in place. ZFS only |

The mechanism comes from `datadir.method`, so there is one knob and no way to pair the wrong mechanism with the wrong method:

| `datadir.method` | `before_pre_runs` | `before_benchmarks` |
|------------------|-------------------|---------------------|
| `schelk` | `schelk promote` makes the compacted volume the new baseline | Same, folded into the existing promote |
| `zfs` | The source dataset is compacted in place, before the snapshot and the clone | Not supported |
| `direct` | Already permanent: the client writes to `source_dir` itself | Same |
| `copy`, `overlayfs`, `fuse-overlayfs` | Not supported | Not supported |

Two rules follow from the mechanisms:

- **ZFS persists only at `before_pre_runs`.** The clone is a child of its source dataset, so no promote or rename puts the compacted clone back at the source path. Compacting the source first also keeps the clone small, since a compaction inside a copy-on-write clone rewrites the whole database into it.
- **A schelk persist at `before_benchmarks` needs `promote_post_pre_runs: true`.** Persisting there moves the baseline head past the pre-run bundle, which is exactly what that option does. Requiring it keeps the decision explicit, and lets the runner persist both with a single promote.

> **A persist is destructive and irreversible.** It overwrites the golden image. On ZFS, `safety_snapshot` leaves a `<dataset>@benchmarkoor-precompaction-<run-id>` snapshot that a `zfs rollback` restores exactly.

###### The datadir marker

A finished compaction writes `.benchmarkoor-db-compaction.json` at the root of the datadir it compacted, so a persisted baseline says what has already been done to it:

```json
{
  "version": 1,
  "phases": {
    "before_pre_runs": {
      "client": "geth",
      "image": "ethereum/client-go:stable",
      "run_id": "20260827-101203-abcd",
      "completed_at": "2026-08-27T10:12:03Z",
      "duration_ms": 812345,
      "datadir_bytes": { "before": 812000000000, "after": 640000000000 }
    }
  }
}
```

It holds only what is known the moment the compaction finishes, so it is written once and never patched. With `persist` enabled, `skip_if_marked` defaults to true and a later run skips a phase the marker already names — which is what stops every run paying the compaction cost again.

The marker cannot tell that a datadir advanced after it was written. If you point a longer pre-run bundle at a baseline persisted at `before_benchmarks`, set `skip_if_marked: false` to force the compaction.

###### Rollback strategy

`container-recreate` re-prepares the datadir for every test, so the runner rejects a compaction it would discard at the first recreate:

| `datadir.method` | `container-recreate` |
|------------------|----------------------|
| `zfs` | Supported. The ready-snapshot carries the compaction |
| `schelk` | Needs `db_compaction.persist.enabled: true` |
| `copy`, `overlayfs`, `fuse-overlayfs` | Rejected |
| none (fresh volume per test) | Rejected |

###### Output

Each phase writes its reports to the run results directory:

```
<run-results>/db-compaction/
  before_pre_runs/inspect-before.txt
  before_pre_runs/compact.log
  before_pre_runs/inspect-after.txt
  before_pre_runs/compaction.json
  before_benchmarks/...
```

`compaction.json` records the command, the duration, the datadir size either side, and — at `before_benchmarks` only, where the runner reads it from the client it is about to stop — the datadir head.

The inspection is a report, so a failure is logged and the compaction still runs. geth 1.17.5 exits 1 on `db inspect` against a datadir whose freezer is empty, which a freshly-initialised datadir has.

**When to use:**
- When benchmarking on a snapshot whose database has poor key locality after a long fill or replay
- When you want every client to start from a database in the same shape, rather than one that reflects how the snapshot was built
- With `persist`, when you want to pay the compaction cost once and have every later run start from the compacted baseline

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
| `schelk_options` | object | – | schelk-specific behaviour; only valid with `method: schelk` (see below) |

###### `schelk_options`

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `promote_post_pre_runs` | bool | `false` | Once the suite's pre-run steps have **all** succeeded, stop the client, run `schelk promote` so the advanced datadir becomes the new virgin baseline, remount, and bring the client back. Every later `schelk restore` — including the per-test one a `container-recreate` rollback performs — then starts post-pre-run, so the bundle never has to be replayed again. |

Supported on all rollback strategies (`none`, `rpc-debug-setHead`, `container-recreate`, `container-checkpoint-restore`). It is refused when the suite has no pre-run steps (the baseline would just be the raw snapshot), and only one instance may set it — there is one schelk volume per host, so a second promoter would silently redefine the baseline for the first.

> **Irreversible.** `schelk promote` overwrites the virgin volume; the original snapshot cannot be recovered without re-fetching it. Best on a host dedicated to one benchmark campaign. The builder-side equivalent is [`builder.pre_runs` `schelk_options.promote`](#builderpre_runs-options), which is preferable when the pre-run runs on the same machine.

##### Data Directory Methods

| Method | Description | Requirements |
|--------|-------------|--------------|
| `copy` | Parallel Go copy with progress display | None (default, works everywhere) |
| `overlayfs` | Linux overlayfs for near-instant setup | Root access |
| `fuse-overlayfs` | FUSE-based overlayfs | `fuse-overlayfs` package; `user_allow_other` in `/etc/fuse.conf` if Docker runs as root. **Warning:** ~3x slower than native overlayfs |
| `zfs` | ZFS snapshots and clones for copy-on-write setup | Source directory on ZFS filesystem; root access or ZFS delegations configured |
| `direct` | Bind-mount `source_dir` directly into the container with no copy/snapshot. **Changes persist after the run.** Intended for inspection / resume workflows, not normal benchmarking | None |
| `schelk` | Use a [schelk](https://github.com/tempoxyz/schelk)-managed scratch volume restored from a virgin baseline between iterations | `schelk` binary on PATH (or `BENCHMARKOOR_SCHELK_BIN`); schelk initialised via `schelk init-new` / `init-from`; root access |

###### ZFS Setup

For ZFS method without root:
```bash
zfs allow -u <user> clone,create,destroy,mount,snapshot <dataset>
```

The dataset is auto-detected from the source directory mount point.

###### Schelk Setup

[schelk](https://github.com/tempoxyz/schelk) keeps a pristine virgin block device and a scratch block device, using `dm-era` on a ramdisk to track which blocks changed during a run so the scratch can be surgically restored from virgin between iterations. It pairs well with `rollback_strategy: container-recreate`, which gives schelk a clean baseline at the start of every test.

Before configuring benchmarkoor, initialise schelk on the host (see [schelk's SKILL.md](https://github.com/tempoxyz/schelk/blob/master/docs/SKILL.md)):

```bash
sudo schelk init-from \
  --virgin /dev/<virgin>  --scratch /dev/<scratch> \
  --ramdisk /dev/ram0 --mount-point /schelk --fstype ext4
```

Then point `source_dir` at the path inside the schelk mount that holds your client's datadir:

```yaml
runner:
  client:
    config:
      rollback_strategy: container-recreate
  instances:
    - id: reth-schelk
      client: reth
      datadir:
        method: schelk
        source_dir: /schelk/eth/reth   # subpath under the schelk mount
```

What benchmarkoor does at runtime:
- **Config validation** verifies `schelk` is on PATH, reads `/var/lib/schelk/state.json` to learn the mount point, and runs `schelk mount` if the scratch isn't currently mounted. If state says mounted but `/proc/mounts` disagrees (a crash artefact), benchmarkoor surfaces a clear error pointing at `schelk full-recover`.
- **Per container lifecycle**, `Prepare` runs `schelk restore` (recover + mount) so each iteration starts from the virgin baseline. `Cleanup` runs `schelk recover` to unmount and restore baseline.
- **Graceful shutdown**: schelk commands run in their own process group, so a SIGTERM to benchmarkoor does not propagate to schelk. An in-flight schelk command is given up to 60 seconds to finish before being killed, so a recover mid-flight is not interrupted.

**Building onto a schelk mount:** when a `builder.state_actor` target's `output_dir` is under the schelk mount, `benchmarkoor build` mounts the scratch first (the same `schelk mount` preflight as above), materialises the datadir onto it, and — only when a build actually ran (fresh / `--force` / a `--rebuild-on-diff` change) — runs `schelk promote` to persist the new datadir as the virgin baseline. A skipped, unchanged target is not promoted. This is how the built datadir becomes the baseline the runner's per-iteration `schelk restore` resets to. No configuration is needed — it is detected from the output_dir path.

Notes:
- `rollback_strategy: container-checkpoint-restore` is **not** compatible with `method: schelk` (it requires `method: zfs`).
- All operational schelk commands require root.
- `source_dir` must be the schelk mount point or a subdirectory of it.

| Environment Variable | Description |
|----------------------|-------------|
| `BENCHMARKOOR_SCHELK_BIN` | Override the schelk executable path. Useful when running under `sudo` with a sanitised PATH that does not include `~/.cargo/bin`. Accepts a bare name (resolved via PATH) or an absolute/relative path. Default: `schelk` |
| `SCHELK_STATE` | Override the schelk state-file path. Honoured by both schelk itself and benchmarkoor's preflight. Default: `/var/lib/schelk/state.json` |

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
| `genesis_fork_override` | map | No | - | Activate forks at given timestamps by patching a geth-format genesis at boot. See [Genesis Fork & EIP Overrides](#genesis-fork--eip-overrides) |
| `genesis_eip_override` | object | No | - | Activate EIPs at a timestamp by patching a parity/nethermind chainspec at boot. See [Genesis Fork & EIP Overrides](#genesis-fork--eip-overrides) |
| `datadir` | object | No | From `runner.client.datadirs` | Instance-specific data directory config |
| `drop_memory_caches` | string | No | From `runner.client.config` | Instance-specific cache drop setting |
| `rollback_strategy` | string | No | From `runner.client.config` | Instance-specific rollback strategy |
| `checkpoint_restore_strategy_options` | object | No | From `runner.client.config` | Instance-specific checkpoint-restore strategy options (replaces global) |
| `wait_after_rpc_ready` | string | No | From `runner.client.config` | Instance-specific RPC ready wait duration |
| `run_timeout` | string | No | From `runner.client.config` | Instance-specific run timeout duration |
| `retry_new_payloads_syncing_state` | object | No | From `runner.client.config` | Instance-specific retry config for SYNCING responses |
| `retry_new_payloads_failed_state` | object | No | From `runner.client.config` | Instance-specific retry config for non-SYNCING failures |
| `resource_limits` | object | No | From `runner.client.config` | Instance-specific resource limits |
| `post_test_rpc_calls` | []object | No | From `runner.client.config` | Instance-specific post-test RPC calls (replaces global) |
| `post_test_sleep_duration` | string | No | From `runner.client.config` | Instance-specific post-test sleep duration |
| `bootstrap_fcu` | bool/object | No | From `runner.client.config` | Instance-specific bootstrap FCU setting |
| `opcode_extraction` | object | No | From `runner.client.config` | Instance-specific opcode extraction setting (replaces global) |
| `db_compaction` | object | No | From `runner.client.config` | Instance-specific database compaction setting (replaces global) |

#### Genesis Fork & EIP Overrides

These options let an instance activate a fork that is not scheduled in the genesis it boots from — for example, running Amsterdam payloads against an Osaka snapshot. benchmarkoor patches the genesis file in-memory at boot, before mounting it; the source genesis on disk is never modified, and untouched fields (including large integers) round-trip verbatim, so the genesis block hash is unchanged.

Use these only for clients that read their fork schedule from the genesis file. **geth and erigon do not** — they read the fork schedule from the datadir, so a patched genesis is ignored. For those, use the client's own fork-override flag instead (e.g. `--override.amsterdam=<timestamp>` in `extra_args`).

**`genesis_fork_override`** — for geth-format genesis files (besu, reth, ethrex). A map of fork name to activation timestamp. For each entry it sets `config.<fork>Time`, and if the genesis has a `blobSchedule` that lacks the fork, it inherits the schedule of the latest preceding fork (so the new fork carries a blob schedule, as geth-family clients require).

```yaml
runner:
  instances:
    - id: besu
      client: besu
      genesis: /path/to/osaka-chainspec.json   # used as-is
      genesis_fork_override:
        amsterdam: 1   # sets config.amsterdamTime=1, inherits blobSchedule.amsterdam
```

**`genesis_eip_override`** — for parity/nethermind-format chainspecs, which schedule forks per-EIP rather than by fork name. It sets `params.eip<N>TransitionTimestamp` for each listed EIP to the given (hex-encoded) timestamp. The EIP list is devnet-specific, so it lives in config.

```yaml
runner:
  instances:
    - id: nethermind
      client: nethermind
      genesis: /path/to/osaka-parity-chainspec.json   # used as-is
      genesis_eip_override:
        timestamp: 1
        eips: [7708, 7778, 7843, 7928, 7954, 7976, 7981, 8024, 8037]
```

| Option | Type | Description |
|--------|------|-------------|
| `genesis_fork_override` | map[string]uint | Fork name → activation timestamp (unix seconds). geth-format genesis only. |
| `genesis_eip_override.timestamp` | uint | Activation timestamp (unix seconds) applied to every listed EIP. |
| `genesis_eip_override.eips` | []uint | EIP numbers to activate, e.g. `[7928, 8037]`. parity/nethermind chainspec only. |

Applying an override to the wrong genesis format is an error (a geth-format override needs a top-level `config` object; an EIP override needs a top-level `params` object).

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

## Builder

The `builder` section configures tools that pre-populate benchmark inputs on disk. There are three builders:

- **`state_actor`** (https://github.com/ethereum/state-actor) writes per-client genesis state directly in each EL's native on-disk format — geth Pebble, reth MDBX, besu/nethermind RocksDB — bypassing the client's normal genesis-replay path.
- **`pre_runs`** advances an existing snapshot datadir — gas-bump, funding block, optional pre-fork system-contract deploy, then a `fill-stateful` run on *setup* tests — so a later benchmark fill builds on top of that state. It also records a replayable payload bundle. Optional.
- **`eest_payloads`** generates stateful EEST benchmark fixtures by running [`fill-stateful`](https://github.com/ethereum/execution-specs/pull/2637) against a filler client booted on a pre-populated snapshot (typically one produced by `state_actor` or `pre_runs`). The fixtures are replayed by `benchmarkoor run`.

Builds are **decoupled from `benchmarkoor run`**: invoke `benchmarkoor build` to materialise the artifacts, then run benchmarks against them via the regular `datadir.method: copy|zfs|schelk|…` providers and test-source config. A missing datadir at `run` time is an error — it is never auto-built. When several builders are configured, they run in stage order (`state_actor` → `pre_runs` → `eest_payloads`) so a fixture build can consume a datadir produced earlier in the same `benchmarkoor build` invocation. **A failed `pre_runs` target blocks every `eest_payloads` target**: `eest_payloads` fills against the datadir the pre-run was supposed to advance, and filling against an un-advanced one does not fail loudly — it emits fixtures that look valid but describe the wrong chain.

| Option | Type | Default | Description |
|---|---|---|---|
| `run_timeout` | string | – | Global timeout capping the entire `benchmarkoor build` (all builders and targets), as a Go duration (e.g. `2h`, `90m`). Empty means no timeout. Overridable via `BENCHMARKOOR_BUILDER_RUN_TIMEOUT`. The analogue of [`runner.run_timeout`](#runner-run-timeout) for builds. |
| `cleanup_on_start` | bool | `false` | Remove leftover benchmarkoor resources (containers, volumes, the build network, ZFS clones, overlay mounts, CPU-freq state) before the build starts — the same sweep as the `benchmarkoor cleanup` command. Overridable via `BENCHMARKOOR_BUILDER_CLEANUP_ON_START`. The analogue of `runner.cleanup_on_start` for builds. |

### `builder.state_actor` options

```yaml
builder:
  state_actor:
    # Per-client images. State-actor needs cgo to write reth/besu/nethermind
    # datadirs, so each client has its own image. Every active target's
    # client must have an entry here.
    images:
      geth: ghcr.io/ethereum/state-actor:latest
      reth: ghcr.io/ethereum/state-actor-reth:latest
      besu: ghcr.io/ethereum/state-actor-besu:latest
      nethermind: ghcr.io/ethereum/state-actor-nethermind:latest
    pull_policy: always                           # always | if-not-present | never (default: always)
    container_runtime: docker                     # docker | podman (default: inherits runner.container_runtime, then docker)
    # spec source — top-level, shared across every target.
    # Pick at most one of:
    #   spec:         # structured YAML (or a `|` block scalar); written to a temp file before invoking state-actor
    #     entities: [ ... ]
    #   spec_file: /etc/benchmarkoor/state-spec.yaml   # absolute host path
    config:                                       # shared per-target defaults; targets override when set
      seed: 1
      fork: prague
      chain_id: 1337
    targets:
      - name: geth-5g                             # optional, defaults to `client`; used by --target filter
        client: geth
        output_dir: /srv/state/geth-5g
        target_size: 5GB
```

| Option | Type | Default | Description |
|---|---|---|---|
| `images` | map[string]string | – | Per-client docker images for state-actor. Every active target's client must have an entry; state-actor needs a different cgo build per client (reth → MDBX, besu → RocksDB JNI, nethermind → .NET RocksDB). |
| `pull_policy` | string | `always` | One of `always`, `if-not-present`, `never`. |
| `container_runtime` | string | runner's runtime, then `docker` | Container runtime for the build container. |
| `spec` | YAML mapping or string | – | Inline state spec body (see [state-actor SPEC.md](https://github.com/ethereum/state-actor/blob/main/docs/SPEC.md)). Write it as **structured YAML** (a mapping — your editor highlights it) or as a `\|` block scalar; both materialise to the same temp spec file at build time. Mutually exclusive with `spec_file`. |
| `spec_file` | string | – | Absolute host path to a state spec YAML. Bind-mounted read-only into the build container. Mutually exclusive with `spec`. |
| `config` | object | – | Shared defaults for the per-target build parameters. See below. |
| `targets` | []object | – | Required when invoking `benchmarkoor build`. See below. |

The top-level `spec`/`spec_file` applies to every target. A target without its own `target_size` runs with just the spec; a target with both flags runs with both (state-actor uses the spec and treats `target_size` as a headroom budget for any further auto-fill). A target with neither `target_size` nor any spec source is a validation error.

### `builder.state_actor.config` options

Every field is also available per-target; a non-nil/non-empty value on a target overrides the corresponding default from `config`. Use this block to avoid repeating the same `seed`, `fork`, `chain_id`, etc. across every target.

| Option | Type | Default | Description |
|---|---|---|---|
| `target_size` | string | – | Default size budget for every target (e.g. `5GB`). Targets without their own `target_size` inherit this value. Complements `spec`/`spec_file` — state-actor uses both at once, treating `target_size` as a headroom budget on top of the spec. |
| `seed` | int64 | – | RNG seed for auto-fill. `0` = wall-clock (non-reproducible). |
| `fork` | string | – | Hard fork at genesis, e.g. `prague`, `osaka`. |
| `chain_id` | int64 | – | Genesis chain ID. |
| `gas_limit` | uint64 | – | Genesis gas limit. |
| `timestamp` | uint64 | – | Unix seconds at genesis. |
| `extra_data` | string | – | Hex `extraData` for the genesis block. |
| `archive` | bool | – | Archive-mode metadata. Effective value must be limited to geth/reth, regardless of where it was set. |
| `binary_trie` | bool | – | EIP-7864 binary trie. Effective value must be limited to geth. |
| `group_depth` | int (1..8) | – | Binary-trie serialisation unit. Requires effective `binary_trie=true`. |

Applicability validation runs on the **effective** target (config defaults + per-target overrides), so `archive: true` set globally is rejected if any active target is besu/nethermind. To opt a target out of a global `archive: true`, set `archive: false` on that target.

### `builder.state_actor.targets[]` options

The fields below mirror `builder.state_actor.config`; any field set here overrides the corresponding default from `config`. Identifier fields (`name`, `client`, `output_dir`, `target_size`) are target-only.

| Option | Type | Default | Applies to | Description |
|---|---|---|---|---|
| `name` | string | `client` | all | Human-readable name. Used by `--target` to filter. Must be unique across targets; defaults to the `client` field when omitted. |
| `client` | string | – | all | One of `geth`, `reth`, `besu`, `nethermind`, `ethrex`. State-actor does not support `erigon` or `nimbus`. |
| `output_dir` | string | – | all | Absolute host path. If the directory already contains entries, that target is **skipped** (no error) — pass `--force` (CLI) or set `force: true` here to wipe and rebuild. For geth, state-actor writes into `<output_dir>/geth/chaindata`. |
| `target_size` | string | from `config` | all | Advisory size budget for auto-generated state, e.g. `5GB`, `500MB` (base-1024). Required for the target when no spec is configured; when a spec is configured (top-level or default), `target_size` is optional and acts as a headroom budget that state-actor fills past the spec's projected cost. |
| `force` | bool | `false` | all | Per-target override of the CLI `--force` flag: wipes `output_dir` before building so state-actor sees a clean directory. Useful when most targets should skip-if-built but specific ones should always rebuild. |
| `seed` | int64 | from `config`, then `1` (state-actor) | all | RNG seed for auto-fill. `0` = wall-clock (non-reproducible). |
| `fork` | string | from `config`, then latest supported by state-actor | all | Hard fork at genesis, e.g. `prague`, `osaka`. Run `state-actor --list-forks` for the current list. |
| `chain_id` | int64 | from `config`, then `1337` (state-actor) | all | Genesis chain ID. |
| `gas_limit` | uint64 | from `config`, then `30000000` (state-actor) | all | Genesis gas limit. |
| `timestamp` | uint64 | from `config`, then `0` (state-actor) | all | Unix seconds at genesis. |
| `extra_data` | string | from `config`, then `""` | all | Hex `extraData` for the genesis block. |
| `archive` | bool | from `config`, then `false` | geth, reth | Archive-mode metadata. Set `false` to opt out of a global `archive: true`. Rejected (after resolution) for besu/nethermind. |
| `binary_trie` | bool | from `config`, then `false` | geth | EIP-7864 binary trie. Set `false` to opt out of a global default. Rejected (after resolution) for non-geth. |
| `group_depth` | int | from `config`, then `8` (state-actor) | geth + binary_trie | Binary-trie serialisation unit. Range 1..8. Requires effective `binary_trie=true`. |

State-actor itself only writes the genesis block; subsequent blocks come from running a client against the produced datadir. See [state-actor RUNBOOK.md](https://github.com/ethereum/state-actor/blob/main/docs/RUNBOOK.md) for the per-client boot recipes (e.g. geth needs `--db.engine=pebble`; reth needs `--debug.skip-genesis-validation`; besu needs `--data-storage-format=BONSAI`; ethrex needs `--skip-genesis-validation` and ≥ v16.0.0).

### Running

```bash
# Build every target declared under builder.state_actor.targets / builder.eest_payloads.targets
benchmarkoor build --config build.yaml

# Build only specific targets by name, across all builders
benchmarkoor build --config build.yaml --target geth-5g --target reth-spec

# Limit a single builder's targets (the other builder is unrestricted)
benchmarkoor build --config build.yaml --limit-state-actor-target nethermind
benchmarkoor build --config build.yaml --limit-eest-payload-target payload-generator-nethermind

# Build just one client end-to-end: its snapshot, then its fill
benchmarkoor build --config build.yaml \
  --limit-state-actor-target nethermind \
  --limit-eest-payload-target payload-generator-nethermind

# Overwrite existing output_dir contents
benchmarkoor build --config build.yaml --force
```

| Flag | Description |
|---|---|
| `--target` | Filter by target `name` across **all** builders (comma-separated or repeated). |
| `--limit-state-actor-target` | Filter only `builder.state_actor` targets; `eest_payloads` is left unrestricted. |
| `--limit-eest-payload-target` | Filter only `builder.eest_payloads` targets; `state_actor` is left unrestricted. |
| `--skip-state-actor-build` | Skip the `builder.state_actor` builder entirely (only `eest_payloads` runs). |
| `--skip-eest-payload-build` | Skip the `builder.eest_payloads` builder entirely (only `state_actor` runs). |
| `--force` | Wipe each selected target's `output_dir` before building (bypasses the skip-if-populated behaviour). |
| `--rebuild-on-diff` | Rebuild a populated `output_dir` when its config changed since the last build, instead of skipping (see below). |

A target is built when it passes the global `--target` filter **and** the per-builder limit for the builder that owns it; an unset filter imposes no restriction. Any filter value that names no existing target is a hard error (typos surface immediately — the per-builder limits are checked against only that builder's target names). `--skip-*-build` removes a whole builder; a skipped builder's `--limit-*-target` is then ignored. Skipping every configured builder is an error.

### Rebuild on config change (`--rebuild-on-diff`)

By default a populated `output_dir` is skipped regardless of whether the config that produced it changed — you must `--force` to pick up a new fork, seed, spec, filter, etc. After every successful build benchmarkoor now records a `.benchmarkoor-build.json` sidecar in the `output_dir` holding a fingerprint of the output-affecting config. With `--rebuild-on-diff`, a populated target is rebuilt only when that fingerprint differs from the current config (the changed keys are logged); an unchanged config still skips, and a directory with no sidecar (built before this existed) is skipped until the next `--force` records a baseline.

The fingerprint covers the inputs that actually change the output:

- **state_actor:** client, image, target_size, seed, fork, chain_id, gas_limit, timestamp, extra_data, archive, binary_trie, group_depth, and the spec **content**.
- **eest_payloads:** filler client + image, fork, tests, filter, marker, gas/opcode values, max gas, rpc seed key, eoa start, filler extra args, datadir method, the **content** of the genesis / address-stubs / fill Dockerfile, the fork/EIP genesis overrides, and the execution-specs checkout resolved to a **commit SHA** (via `git ls-remote`, so a moving `eest_ref` that advanced is detected). It also folds in the **source snapshot's** fingerprint, so rebuilding a `state_actor` datadir cascades into rebuilding the fixtures generated from it.

The command exits non-zero if any target fails; successful targets are still left in place on partial failure. A final summary lists each target with `OK ` (built), `SKIP` (output_dir already populated), or `ERR ` (failed).

### Examples

Minimal — one geth datadir sized at 5 GB:

```yaml
builder:
  state_actor:
    images:
      geth: ghcr.io/ethereum/state-actor:latest
    targets:
      - client: geth
        output_dir: /srv/state/geth-5g
        target_size: 5GB
```

Full — three clients sharing a top-level spec file and global defaults; geth opts out via `target_size`, reth opts out of the global archive setting:

```yaml
builder:
  state_actor:
    images:
      geth: ghcr.io/ethereum/state-actor:latest
      reth: ghcr.io/ethereum/state-actor-reth:latest
      besu: ghcr.io/ethereum/state-actor-besu:latest
    pull_policy: if-not-present
    spec_file: /etc/benchmarkoor/state-spec.yaml
    config:
      # Applies to every target that doesn't override it.
      seed: 42
      fork: prague
      chain_id: 1337
      archive: true       # geth + reth inherit this; besu sets archive: false below
    targets:
      - name: geth-archive
        client: geth
        output_dir: /srv/state/geth-archive
        # target_size + top-level spec: spec drives the build, target_size sets the headroom budget
        target_size: 5GB
        binary_trie: true
        group_depth: 4
      - client: reth
        output_dir: /srv/state/reth-spec
      - client: besu
        output_dir: /srv/state/besu-spec
        archive: false    # overrides config.archive=true (besu doesn't support archive)
```

Inline spec — write the YAML directly in the config (structured, so editors highlight it; a `|` block scalar works too):

```yaml
builder:
  state_actor:
    images:
      geth: ghcr.io/ethereum/state-actor:latest
    spec:
      entities:
        - kind: eoa
          name: bloated-eoa
          approximate_size_bytes: 2_000_000_000
      # … rest of the state spec
    targets:
      - client: geth
        output_dir: /srv/state/geth-spec
```

### `builder.pre_runs` options

`pre_runs` advances an existing snapshot datadir so a later benchmark fill starts from pre-deployed state, and records a **replayable payload bundle** describing everything it built. Per target it boots a filler client on the snapshot and, in order:

1. Builds a **funding block** crediting `funding_accounts` (and any `funding_pools`) via beacon withdrawals.
2. Optionally runs a **`predeploy`**: deploys the target fork's system contracts while the chain is still on the *pre*-fork.
3. **Gas-bumps** with empty blocks until the head gas limit reaches `gas_limit` (default 1 TGas), capped by `gas_bump_max_blocks`.
4. Runs `fill-stateful --no-reset-between-tests` on the configured setup `tests`, so deployed state persists across them.
5. Writes `<bundle_dir>/pre_run_bundle/pre-run.request` plus a `pre-run.meta.json` sidecar describing it.

Only `geth`, `besu` and `nethermind` can act as the filler. A target with `replay_from` set instead *consumes* a bundle: it boots any client (including non-fillers like reth/ethrex, since replay only needs the engine API) and replays the recorded payloads onto its own snapshot.

```yaml
builder:
  pre_runs:
    pull_policy: always
    eest_repo: https://github.com/ethereum/execution-specs.git
    eest_ref: forks/amsterdam
    config:                              # shared per-target defaults; targets override when set
      fork: amsterdam                    # the TARGET fork being filled
      datadir_method: schelk             # copy | overlayfs | fuse-overlayfs | zfs | direct | schelk
      gas_limit: 1000000000000           # gas-bump target (default 1 TGas)
      rpc_seed_key: "0x…01"
      funding_accounts:
        - address: 0x7e5f…5bdf
      tests:
        - tests/benchmark/stateful/bloatnet/test_setup_contracts.py
      fill_env:
        BLOATNET_RECEIVER_CONTRACT_COUNT: "10000"
    targets:
      - name: pre-run-geth
        filler_client: geth
        filler_image: ethpandaops/geth:master
        source_dir: /schelk/snapshots/geth/<network>/<block>
        bundle_dir: /var/lib/benchmarkoor/pre-runs/geth   # keep the bundle off the schelk scratch
        genesis: /path/to/geth-genesis.json
        genesis_fork_override: { amsterdam: 1769856769 }
        schelk_options:
          promote: true                  # persist the advanced datadir as the new schelk baseline
```

| Option | Type | Default | Description |
|---|---|---|---|
| `filler_client` | string | – | `geth`, `besu` or `nethermind`. Replay targets may use any bootable client. |
| `source_dir` | string | – | Snapshot datadir to advance. Absolute. |
| `output_dir` | string | – | Where the advanced datadir is written. Not used (and not required) when `datadir_method: schelk`, which advances `source_dir` in place. |
| `bundle_dir` | string | `output_dir`, else `source_dir` | Where the replay bundle is written (as `<dir>/pre_run_bundle/`). **A schelk target needs this**: the advanced datadir is the copy-on-write scratch, and the next `schelk restore` — including the one the runner performs before replaying — discards anything on it, bundle included. |
| `replay_from` | string | – | Makes this a *replay* target: instead of filling, it replays another target's bundle (by target name) or a path to a `.request` file / `pre_run_bundle` directory. |
| `genesis` | string | – | Boot genesis for the filler. Local path or http(s) URL. |
| `genesis_fork_override` | map | – | Schedules forks by name (`{amsterdam: <ts>}`) in a **geth-format** genesis. |
| `genesis_eip_override` | object | – | Schedules per-EIP transitions at a timestamp in a **parity/nethermind chainspec**. |
| `gas_limit` | uint | `1000000000000` | Gas-bump target. |
| `gas_bump_max_blocks` | int | `20000` | Safety cap on empty gas-bump blocks. |
| `funding_accounts` | list | – | Addresses credited in the funding block. Empty skips the block. |
| `funding_pools` | list | – | Credits a derived sender pool (`base_key_seed` + `count`), matching EEST's `SENDER_BASE_KEY` derivation, for benchmarks drawing senders from a pool. |
| `predeploy` | object | – | See below. Deploys contracts on the pre-fork. |
| `schelk_options.promote` | bool | `false` | After the pre-run finishes and its client has stopped, run `schelk promote` so the advanced datadir becomes the new **virgin baseline**. Requires `datadir_method: schelk`. |
| `eoa_start` | uint64 | `1000000000` | First private key of `fill-stateful`'s EOA iterator for the pre-run fill (`--eoa-start`). The default sits a billion keys above the `eest_payloads` default **on purpose**: the pre-run advances a datadir the later `eest_payloads` fill builds on, and a shared start makes that second fill re-derive accounts the pre-run already used — its transactions then carry a stale nonce and never reach a block (`No receipt found for transaction …`). Pin it per target when you chain more than two fills onto one datadir. Must be > 0. |
| `tests`, `filter`, `marker`, `gas_benchmark_values`, `fill_env`, `filler_extra_args`, `address_stubs`, `rpc_seed_key`, `datadir_method`, `filler_image`, `fork` | | | As in `eest_payloads`; all are hoistable from `config`. Note `filler_extra_args` **replaces** the `config` value rather than merging, so a per-client target must list every flag it needs. |

#### `predeploy`

Deploys a fork's system contracts *before* that fork activates — a strict client rejects post-fork blocks whose system contracts have no code. Requires the target fork to be scheduled at a positive timestamp the deploy blocks precede, via **either** `genesis_eip_override` **or** `genesis_fork_override.<fork>`.

```yaml
predeploy:
  pre_fork: osaka                        # the fork the snapshot boots at
  deployer_key: "0x…02"                  # funded automatically alongside funding_accounts
  contracts:
    # Form A — plain CREATE of runtime bytecode. Lands at the deployer's CREATE
    # address, which is fine only when the fork's addresses are chainspec params.
    - code: "0x…"
    # Form B — a call to a deployer contract, typically the CREATE2 factory.
    # A CREATE2 address is keccak256(0xff ++ factory ++ salt ++ keccak256(initcode))
    # with no msg.sender in the preimage, so replaying a network's own deployment
    # calldata reproduces the canonical address from any funded sender. This is what
    # puts contracts where clients HARDCODE them (e.g. EIP-8282's request contracts).
    - to: "0x4e59b44847b379578588920cA78FbF26c0B4956C"
      data: "0x<32-byte salt><initcode>"
      address: "0x…"                     # verified against the deployed code afterwards
```

The two forms are mutually exclusive per contract. `address` is required with `to`, since the resulting address follows the callee's scheme and cannot be derived here. The CREATE2 factory must already exist on the chain — it is not in the mainnet genesis alloc but has been deployed on mainnet since 2020, so a mainnet-fork snapshot has it; a synthetic chain must predeploy it (`state_actor`'s `create2_factory` template).

> **`schelk promote` is destructive and irreversible.** It overwrites the virgin volume, and the original snapshot cannot be recovered without re-fetching it. It is refused unless the filler shut down gracefully (a killed client may not have flushed), refused when there are no pre-run steps to bake in, and only one target per config may promote — there is one schelk volume per host, so a second promoter would silently redefine the baseline for the first.

### `builder.eest_payloads` options

`eest_payloads` generates **stateful** EEST benchmark fixtures: it boots a filler EL client on a *writable copy* of a pre-populated snapshot datadir, runs `fill-stateful` against the live client (recording engine-API payloads anchored to the snapshot's head block), and writes the fixtures to each target's `output_dir`. `fill-stateful` itself does not manage datadirs — benchmarkoor boots the filler and snapshots it.

> **Filler client:** `geth` (`ethpandaops/geth:master`) is the production-ready filler. `nethermind` (`nethermindeth/nethermind:master`) also works — it implements `testing_buildBlockV1` with correct EIP-7928 block-access-lists, and `fill-stateful`'s per-test rewind falls back to `debug_resetHead` for it (nethermind has no `debug_setHead`). `besu` works too with an image carrying the merged `TestingBuildBlockV1` coinbase fix (e.g. `ethpandaops/besu:bal-devnet-7`); benchmarkoor auto-pins its session priority fee.
>
> **Fill image:** by default benchmarkoor builds the fill image (the `uv`/python toolchain that runs `fill-stateful`) from a Dockerfile **embedded in the binary** — nothing to publish or pass. To pull a pre-built image instead, set `fill_image`; to build from a custom Dockerfile, set `fill_dockerfile`. The embedded Dockerfile lives at `pkg/builder/Dockerfile.eest-filler`; to build it by hand:
> ```bash
> docker build -f pkg/builder/Dockerfile.eest-filler -t ghcr.io/your-org/eest-fill-stateful:latest .
> ```

```yaml
builder:
  eest_payloads:
    # Fill image defaults to a Dockerfile embedded in the binary. Optionally:
    # fill_image: ghcr.io/your-org/eest-fill-stateful:latest   # pull a pre-built image instead
    # fill_dockerfile: pkg/builder/Dockerfile.eest-filler      # or build from a custom Dockerfile
    pull_policy: always                  # always | if-not-present | never (default: always)
    container_runtime: docker            # docker | podman (default: inherits runner.container_runtime, then docker)
    # jwt: <hex>                         # Engine API secret, shared with the filler (default: benchmarkoor's DefaultJWT)
    # fill_command: [uv, run, fill-stateful]   # argv prefix inside fill_image (this is the default)
    # eest_repo: https://github.com/ethereum/execution-specs.git   # cloned + mounted at /eest (default)
    # eest_ref: forks/amsterdam          # branch, tag, or commit to check out (default: forks/amsterdam)
    config:                              # shared per-target defaults; targets override when set
      filler_image: ethpandaops/geth:master
      fork: Osaka
      gas_benchmark_values: [10, 30]     # millions of gas to parametrise against
      # fixed_opcode_count: [0.5, 1, 2]  # thousands of opcodes; mutually exclusive with gas_benchmark_values
      # extract_opcode_count: true       # per-opcode counts in _info.metadata.opcode_count via debug_traceBlockByHash + a custom JS tracer (slow)
      datadir_method: copy               # copy | overlayfs | fuse-overlayfs | zfs | direct | schelk
    targets:
      - name: compute-geth
        filler_client: geth
        source_dir: /srv/state/geth-archive     # PRISTINE snapshot (never mutated; a writable copy is filled)
        # geth boots from the datadir; to fill a fork that activates after the
        # snapshot, pass --override.<fork> here (besu/nethermind use `genesis` +
        # genesis_fork_override / genesis_eip_override instead):
        # filler_extra_args: [--override.amsterdam=1]
        output_dir: /srv/fixtures/compute
        tests:
          - tests/benchmark/compute              # pytest paths inside the fill image
        filter: bn128                            # optional pytest -k expression
        # eoa_start: 1000                        # first key of the EOA iterator (--eoa-start); 1000 when unset
```

| Option | Type | Default | Description |
|---|---|---|---|
| `fill_image` | string | – | Pre-built container image carrying the uv/python toolchain that runs `fill-stateful`. Optional: when neither this nor `fill_dockerfile` is set, benchmarkoor builds the fill image from a Dockerfile embedded in the binary. |
| `fill_dockerfile` | string | – | Path to a custom Dockerfile that benchmarkoor builds with the container runtime at build time, instead of pulling a pre-built image or using the embedded default. Tagged `fill_image` when set, else `benchmarkoor-eest-fill:local`. Requires the runtime's `build` CLI (docker/podman) on the host. |
| `pull_policy` | string | `always` | One of `always`, `if-not-present`, `never`. Applies to both the fill image and the filler image (ignored for a locally built fill image). |
| `container_runtime` | string | runner's runtime, then `docker` | Container runtime for the filler + fill containers. |
| `jwt` | string | benchmarkoor's `DefaultJWT` | Engine API JWT secret; shared between the filler client and `fill-stateful`. |
| `fill_command` | []string | `[uv, run, fill-stateful]` | argv prefix invoked inside `fill_image` before the `fill-stateful` flags. Override if your image exposes the command differently. |
| `eest_repo` | string | `https://github.com/ethereum/execution-specs.git` | execution-specs repo cloned for filling. |
| `eest_ref` | string | `forks/amsterdam` | Branch, tag, or commit of `eest_repo`. benchmarkoor always clones the repo at this ref into an on-disk cache at build time and mounts the checkout into the fill container at `/eest` (the `fill_image` carries only the uv/python toolchain, not the repo), so the EEST version is config-driven and changeable without rebuilding the image. The clone is cached and re-fetched only when the ref changes; `uv` builds the venv into the mounted checkout on first use (cached across runs). |
| `config` | object | – | Shared defaults for the per-target parameters. See below. |
| `targets` | []object | – | Required when invoking `benchmarkoor build`. See below. |

### `builder.eest_payloads.config` options

Every field below is also available per-target; a non-nil/non-empty value on a target overrides the default. Use this block to avoid repeating shared knobs (`fork`, `tests`, `filter`, `address_stubs`, …) across targets that build the same suite. (Only the identity/locator fields — `name`, `filler_client`, `source_dir`, `output_dir`, `genesis`, `genesis_fork_override`, `genesis_eip_override` — are target-only and never hoisted.)

| Option | Type | Default | Description |
|---|---|---|---|
| `filler_image` | string | – | Docker image for the filler client (e.g. `ethpandaops/geth:master`). |
| `fork` | string | – | Fork to fill against, e.g. `Osaka` (passed to `fill-stateful --fork`). |
| `tests` | string[] | – | pytest paths inside the fill image, e.g. `tests/benchmark/compute`. Required after resolution — set here or per-target. |
| `filter` | string | – | pytest `-k` expression (substring/node-id selection). |
| `marker` | string | – | pytest `-m` marker expression, orthogonal to `filter`'s `-k`, e.g. `repricing` / `not repricing`. |
| `address_stubs` | map | – | Inline `--address-stubs` map: stub name → arbitrary string fields (e.g. `addr`, `pkey`). Materialised to a temp JSON file at build time. Mutually exclusive with `address_stubs_file`. |
| `address_stubs_file` | string | – | **Absolute** host path to a `--address-stubs` JSON map. Mutually exclusive with `address_stubs`. |
| `gas_benchmark_values` | int[] | – | Gas budgets in millions, e.g. `[10, 30]`; joined into `--gas-benchmark-values`. Mutually exclusive with `fixed_opcode_count`. |
| `fixed_opcode_count` | float[] | – | Opcode counts in thousands, e.g. `[0.5, 1, 2]`; joined into `--fixed-opcode-count`. An empty list (`[]`) passes the flag bare, using the fill image's `.fixed_opcode_counts.json` default. Mutually exclusive with `gas_benchmark_values`. |
| `extract_opcode_count` | bool | `false` | Enable `fill-stateful --extract-opcode-count`: after building each execution-phase block, trace it via `debug_traceBlockByHash` with a **custom JS opcode-counting tracer** and record per-opcode execution counts in each fixture's `_info.metadata.opcode_count`. The per-block re-trace with the custom tracer is what makes it **slow**, so it is opt-in. Works with any filler exposing `debug_traceBlockByHash` + JS tracer support (geth is the validated/production-ready filler). Needs an `eest_repo`/`eest_ref` whose `fill-stateful` carries the flag ([execution-specs#3124](https://github.com/ethereum/execution-specs/pull/3124)). |
| `datadir_method` | string | `copy` | How the filler's writable copy of `source_dir` is prepared: `copy`, `overlayfs`, `fuse-overlayfs`, `zfs`, `direct`, `schelk`. Use `zfs`/`overlayfs` to avoid a full copy of a large snapshot. |
| `max_gas_per_test` | uint64 | – | Overrides the fork's transaction gas-limit cap (`--max-gas-per-test`). |
| `rpc_seed_key` | string | – | Pin the seed EOA for reproducible fills (`--rpc-seed-key`); otherwise one is generated and funded via CL withdrawal. |
| `eoa_start` | uint64 | `1000` | First private key of `fill-stateful`'s EOA iterator (`--eoa-start`). The fill mints every account a test funds — `pre.fund_eoa()`, the nonexistent-account addresses, and the per-worker sender under xdist — from the keys that count up from this integer. benchmarkoor **always** passes the flag, because `fill-stateful` otherwise picks a random 256-bit start and the generated addresses change on every fill. Must be > 0. A `pre_runs` fill defaults to `1000000000` instead, so the two stages never mint the same accounts on a shared datadir. |
| `filler_extra_args` | []string | – | Extra argv appended to the filler client command. |

> **Address-stubs hoisting:** `address_stubs` / `address_stubs_file` hoist as a *unit* — a target that sets either form inherits neither from `config`, so their mutual exclusion is preserved. An inline `address_stubs` example:
> ```yaml
> address_stubs:
>   bloated_eoa_10GB:
>     addr: "0x87a6314da5ac8832f6e7a176c8fb133b19f5be04"
>     pkey: "0x4da32d29f6dcffa26e09dc4e102033f2d105de1444fb893493ae703289275e0e"
> ```

### `builder.eest_payloads.targets[]` options

Identity/locator fields are target-only; the rest mirror `config` and are resolved with per-target precedence.

| Option | Type | Default | Description |
|---|---|---|---|
| `name` | string | `filler_client` | Used by `--target` to filter. Must be unique across targets. |
| `filler_client` | string | – | Client booted as the filler: `geth`, `nethermind`, or `besu` (all implement `testing_buildBlockV1`). |
| `source_dir` | string | – | **Absolute** host path to the pristine snapshot datadir (e.g. a `state_actor` `output_dir`). Never mutated — a writable copy is filled. Existence is checked at build time. |
| `genesis` | string | – | **Absolute** host path to the genesis/chainspec the filler boots with (besu/nethermind read their fork schedule from it; passed via the client's genesis flag). Must match the chain config used to produce `source_dir`. geth/erigon boot from the datadir instead and need no `genesis`. |
| `genesis_fork_override` | map | – | Patch the geth-format `genesis` at filler boot to activate forks at given timestamps (`{amsterdam: 1}` → `config.amsterdamTime`, inheriting the blob schedule). For besu/reth/ethrex fillers. Same mechanism as the runner. Requires `genesis`. |
| `genesis_eip_override` | object | – | Patch a parity/nethermind `genesis` at filler boot, setting `params.eip<N>TransitionTimestamp` for each listed EIP. Fields: `timestamp` (uint), `eips` ([]uint). For the nethermind filler. Requires `genesis`; mutually exclusive with `genesis_fork_override`. |
| `output_dir` | string | – | **Absolute** host path for the generated fixtures. Skipped if already populated unless `--force` / `force: true`. Written under `<output_dir>/blockchain_tests_stateful_engine/`. |
| `force` | bool | `false` | Per-target override of `--force`: wipe `output_dir` before filling. |
| `filler_image`, `fork`, `tests`, `filter`, `marker`, `address_stubs`, `address_stubs_file`, `gas_benchmark_values`, `fixed_opcode_count`, `extract_opcode_count`, `datadir_method`, `max_gas_per_test`, `rpc_seed_key`, `eoa_start`, `filler_extra_args` | — | from `config` | Mirror `config` with per-target precedence — see the `config` table above. `tests`, `fork`, and `filler_image` are required after resolution (set on the target or in `config`). |

### Replaying generated fixtures

Point `benchmarkoor run` at the **pristine** snapshot (never the copy the filler mutated) and at the fixture output:

```yaml
runner:
  client:
    datadirs:
      geth:
        source_dir: /srv/state/geth-archive       # the pristine snapshot
        method: zfs                                # or copy/overlayfs/…
  benchmark:
    tests:
      source:
        eest_fixtures:
          local_fixtures_dir: /srv/fixtures/compute
          fixtures_subdir: blockchain_tests_stateful_engine
```

> Stateful replay needs the new fixture format support — see benchmarkoor [#182](https://github.com/ethpandaops/benchmarkoor/pull/182).

As a sanity check, each fixture's recorded `benchmarkGasUsed` should match benchmarkoor's measured `gas_used_total` for that test.

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
          github_repo: ethereum/execution-specs
          github_release: tests-benchmark@v0.0.9

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
