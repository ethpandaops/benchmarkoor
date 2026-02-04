# Block Metrics Log Capture

This document describes the block metrics log capture feature, which collects detailed block execution metrics from Ethereum execution clients during benchmark runs.

## Table of Contents

- [Overview](#overview)
- [Background](#background)
- [Supported Clients](#supported-clients)
- [Configuration](#configuration)
- [Metrics Captured](#metrics-captured)
- [How It Works](#how-it-works)
- [Output Format](#output-format)
- [UI Visualization](#ui-visualization)

## Overview

Block metrics log capture enables benchmarkoor to collect detailed performance metrics emitted by execution clients during block processing. When enabled, clients emit structured JSON logs containing timing breakdowns, throughput measurements, state access patterns, and cache statistics for each block.

This feature correlates these metrics with specific benchmark tests using block hash matching, making it possible to analyze per-test execution characteristics beyond simple timing measurements.

## Background

This feature implements the unified "slowblock" metrics specification developed across Ethereum execution clients. The specification standardizes how clients report detailed block execution metrics.

For more details on the specification and motivation, see:
- [ethresear.ch: Unified slowblock metrics specification](https://ethresear.ch/t/unifying-execution-layer-execution-metrics/22089)

### Client Implementation PRs

| Client | PR |
|--------|-----|
| Geth | [#33655](https://github.com/ethereum/go-ethereum/pull/33655) |
| Reth | [#21237](https://github.com/paradigmxyz/reth/pull/21237) |
| Besu | [#9660](https://github.com/hyperledger/besu/pull/9660) |
| Nethermind | [#10288](https://github.com/NethermindEth/nethermind/pull/10288) |

## Supported Clients

| Client | Parser Status |
|--------|---------------|
| Geth | Fully supported |
| Reth | Stub (pending) |
| Besu | Stub (pending) |
| Nethermind | Stub (pending) |
| Erigon | Stub (pending) |
| Nimbus | Stub (pending) |

The parsing infrastructure (`pkg/blocklog`) supports all clients via the `Parser` interface. Currently only Geth's log format parser is implemented. Other client parsers return no matches until their specific log formats are implemented.

## Configuration

To enable block metrics capture, add the appropriate flag to the client's `extra_args` in your configuration:

### Geth

```yaml
client:
  instances:
    - id: geth
      client: geth
      image: ethpandaops/geth:master
      extra_args:
        - --debug.logslowblock=0
```

The `--debug.logslowblock=0` flag sets the threshold to 0 milliseconds, meaning every block execution will emit metrics. Higher values (e.g., `--debug.logslowblock=100`) only log blocks taking longer than that threshold.

### Other Clients

Configuration flags for other clients will be documented as their parsers are implemented.

## Metrics Captured

The block metrics include:

### Block Information

| Field | Type | Description |
|-------|------|-------------|
| `number` | int | Block number |
| `hash` | string | Block hash (0x-prefixed) |
| `gas_used` | int | Total gas consumed |
| `tx_count` | int | Number of transactions |

### Timing Breakdown

| Field | Type | Description |
|-------|------|-------------|
| `execution_ms` | float | Time spent executing transactions |
| `state_read_ms` | float | Time spent reading state |
| `state_hash_ms` | float | Time spent computing state root hash |
| `commit_ms` | float | Time spent committing state changes |
| `total_ms` | float | Total block processing time |

### Throughput

| Field | Type | Description |
|-------|------|-------------|
| `mgas_per_sec` | float | Megagas processed per second |

### State Reads

| Field | Type | Description |
|-------|------|-------------|
| `accounts` | int | Number of account reads |
| `storage_slots` | int | Number of storage slot reads |
| `code` | int | Number of code reads |
| `code_bytes` | int | Total bytes of code read |

### State Writes

| Field | Type | Description |
|-------|------|-------------|
| `accounts` | int | Number of accounts written |
| `accounts_deleted` | int | Number of accounts deleted |
| `storage_slots` | int | Number of storage slots written |
| `storage_slots_deleted` | int | Number of storage slots deleted |
| `code` | int | Number of code entries written |
| `code_bytes` | int | Total bytes of code written |

### Cache Statistics

| Cache | Fields | Description |
|-------|--------|-------------|
| `account` | hits, misses, hit_rate | Account cache performance |
| `storage` | hits, misses, hit_rate | Storage cache performance |
| `code` | hits, misses, hit_rate, hit_bytes, miss_bytes | Code cache performance |

## How It Works

The block log capture system operates through two main components:

### 1. Parser (`pkg/blocklog/parser.go`)

The `Parser` interface extracts JSON payloads from client-specific log formats. Each client has its own parser implementation due to different log line formats.

For Geth, the parser matches log lines like:
```
WARN [02-02|15:03:22.121] {"level":"warn","msg":"Slow block",...}
```

### 2. Collector (`pkg/blocklog/collector.go`)

The `Collector` interface associates captured logs with test names using block hash correlation:

1. **Registration**: When the executor runs an `engine_newPayload` call, it extracts the block hash from the payload and registers it with the collector along with the test name.

2. **Log Interception**: The collector wraps the client's log output stream. As log lines arrive, the parser attempts to extract JSON payloads.

3. **Matching**: Extracted payloads are matched to tests via their block hash. The collector handles both:
   - **Early registration**: Test registers hash before log arrives (hash stored in pending map)
   - **Late registration**: Log arrives before test registers hash (payload buffered in unmatched map)

4. **Output**: After all tests complete, matched logs are written to the results file.

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│   Client    │────>│  Collector   │────>│   Parser    │
│   Logs      │     │  (Writer)    │     │  (Geth/etc) │
└─────────────┘     └──────────────┘     └─────────────┘
                           │
                           ▼
                    ┌──────────────┐
                    │  Hash Match  │
                    │  (test<->log)│
                    └──────────────┘
                           │
                           ▼
                    ┌──────────────┐
                    │ Block Logs   │
                    │   Output     │
                    └──────────────┘
```

## Output Format

Block logs are written to `result.block-logs.json` in the results directory. The file maps test names to their captured block metrics:

```json
{
  "tests/erc20_transfer/test.txt": {
    "block": {
      "number": 1234,
      "hash": "0xabc123...",
      "gas_used": 21000,
      "tx_count": 1
    },
    "timing": {
      "execution_ms": 1.5,
      "state_read_ms": 0.3,
      "state_hash_ms": 0.8,
      "commit_ms": 0.2,
      "total_ms": 2.8
    },
    "throughput": {
      "mgas_per_sec": 7.5
    },
    "state_reads": {
      "accounts": 5,
      "storage_slots": 10,
      "code": 2,
      "code_bytes": 1024
    },
    "state_writes": {
      "accounts": 2,
      "accounts_deleted": 0,
      "storage_slots": 3,
      "storage_slots_deleted": 0,
      "code": 0,
      "code_bytes": 0
    },
    "cache": {
      "account": {
        "hits": 3,
        "misses": 2,
        "hit_rate": 0.6
      },
      "storage": {
        "hits": 8,
        "misses": 2,
        "hit_rate": 0.8
      },
      "code": {
        "hits": 2,
        "misses": 0,
        "hit_rate": 1.0,
        "hit_bytes": 1024,
        "miss_bytes": 0
      }
    }
  }
}
```

When running multiple genesis groups (client instances), block logs are merged across runs rather than overwritten.

## UI Visualization

The benchmarkoor UI includes a "Block Logs Analysis" view that displays captured metrics. This allows visual comparison of:

- Timing breakdowns across tests
- State access patterns
- Cache hit rates
- Throughput measurements

The UI loads data from `result.block-logs.json` and correlates it with test results for integrated analysis.
