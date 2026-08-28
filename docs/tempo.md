# Benchmark Tempo with Benchmarkoor

This tutorial runs a deterministic Tempo block suite through Tempo's authenticated Engine API in
Docker, preserves the exact input bytes, and turns the run into machine-readable JSON, aggregate
statistics, and a Markdown report.

The data flow is:

```text
Tempo transactions -> canonical Tempo blocks -> manifest + RLP + BAL
  -> fresh Tempo container -> reth_newPayload -> reth_forkchoiceUpdated
  -> correctness, execution timing, wait timing, resources, JSON/Markdown reports
```

Tempo does not use Ethereum's standard `engine_newPayload*` interface for these suites. Its extended
headers and transactions are replayed with `reth_newPayload`; canonical head movement is explicit
through `reth_forkchoiceUpdated`.

## 1. Prerequisites

You need Docker 20.10 or newer, Git, and the Benchmarkoor repository. A Tempo source checkout is
only needed when generating new suites; replay uses the published Tempo image. `jq` is optional but
used by the report examples.

```sh
docker version
docker info

export BENCHMARKOOR_REPO=/absolute/path/to/benchmarkoor

test -f "$BENCHMARKOOR_REPO/go.mod"
```

On Docker Desktop, use the dedicated Compose file in this guide. A Benchmarkoor binary running
directly on a macOS or Windows host usually cannot reach the private bridge address of a sibling
Tempo container. The Compose runner joins the same Docker network as Tempo and works on Docker
Desktop and Linux.

## 2. Select and inspect a suite

Benchmarkoor ships self-contained Tempo suites. `all` is the complete selected runnable corpus:
18 EEST-derived source segments plus nine TIP-20 examples. Focused suites include the six 500M-gas
`tip20-full-blocks` workloads, the KZG `point-evaluation-warm` workload, and four Osaka
`p256verify` workloads:

```sh
find "$BENCHMARKOOR_REPO/integrations/tempo/suites" -maxdepth 2 -name manifest.json -print
```

Start with the focused full-block bundle, or use `suites/all` for the full corpus:

```sh
export TEMPO_SUITE_DIR="$BENCHMARKOOR_REPO/integrations/tempo/suites/tip20-full-blocks"

jq '{format, name, origin, chain, tests: [.tests[] | {
  name, tags, setup_calls: (.setup | length), measured_calls: (.test | length)
}]}' "$TEMPO_SUITE_DIR/manifest.json"
```

`TEMPO_SUITE_DIR` must name the directory containing all three parts:

```text
tip20-full-blocks/
├── manifest.json       suite structure, metadata, setup/test boundary, expectations
├── genesis.json        exact genesis used to create the blocks
└── blocks/
    ├── <sha>.rlp       raw canonical Tempo block, encoded as 0x-prefixed hex
    ├── <sha>.bal       raw block access list, encoded as 0x-prefixed hex
    └── ...
```

Do not copy only `manifest.json`: its genesis, RLP, and BAL paths are relative to the manifest.

## 3. Run the suite in Docker

The dedicated Compose file builds Benchmarkoor, mounts the selected suite, gives Benchmarkoor
access to the Docker socket, and places Benchmarkoor and the Tempo client on the same network.
Benchmarkoor then creates and removes a fresh Tempo data volume for the run.

```sh
cd "$BENCHMARKOOR_REPO"
mkdir -p tmp results

docker compose -f docker-compose.tempo.yaml run --rm --build benchmarkoor
```

By default this uses `docker.io/tempoxyz/tempo:latest`. The Tempo container is started with dev
consensus and a one-hour block interval, preventing the local payload builder from racing the
fixture replay. Benchmarkoor automatically mounts the suite genesis and supplies the JWT secret.

A successful run contains these milestones:

```text
Prepared Tempo Engine suite
RPC endpoint ready
Running setup step
Running test step
Test completed successfully
Run completed ... status=completed
```

The command exits non-zero if the container fails, an RPC call fails, or a payload/forkchoice status
does not match the manifest expectation.

### Run both shipped suites

Each invocation gets a fresh container and data volume. Build Benchmarkoor once, then select each
aggregate suite:

```sh
cd "$BENCHMARKOOR_REPO"

docker compose -f docker-compose.tempo.yaml build benchmarkoor

for suite in "$BENCHMARKOOR_REPO"/integrations/tempo/suites/*; do
  test -f "$suite/manifest.json" || continue
  echo "Running $(basename "$suite")"
  TEMPO_SUITE_DIR="$suite" \
    docker compose -f docker-compose.tempo.yaml run --rm benchmarkoor
done
```

### Collect repeated samples

One run is enough for correctness, but not for a performance baseline. Run each suite at least five
times on an otherwise idle host:

```sh
for iteration in 1 2 3 4 5; do
  echo "Iteration $iteration"
  docker compose -f docker-compose.tempo.yaml run --rm benchmarkoor
done
```

Every invocation receives a distinct run ID. `results/suites/<suite-hash>/stats.json` then lists all
successful samples for that exact suite and timing source.

## 4. Pin or replace the Tempo image

`latest` is convenient for smoke tests but can change. `config.json` records the resolved image
digest, and a repeatable comparison should explicitly pin it:

```sh
docker pull docker.io/tempoxyz/tempo:latest
export TEMPO_IMAGE="$(docker image inspect docker.io/tempoxyz/tempo:latest \
  --format '{{index .RepoDigests 0}}')"

docker compose -f docker-compose.tempo.yaml run --rm benchmarkoor
```

To benchmark the current local Tempo checkout, build its profiling image with the platform used by
Docker. Use `linux/arm64` for Apple Silicon/ARM hosts or `linux/amd64` for x86-64 hosts:

```sh
cd "$TEMPO_REPO"

export VERGEN_GIT_SHA="$(git rev-parse HEAD)"
export VERGEN_GIT_SHA_SHORT="$(git rev-parse --short HEAD)"
export DOCKER_PLATFORM=linux/arm64

docker buildx bake tempo \
  --set tempo.tags=tempo:local \
  --set tempo.platform="$DOCKER_PLATFORM" \
  --load

cd "$BENCHMARKOOR_REPO"
export TEMPO_IMAGE=tempo:local
docker compose -f docker-compose.tempo.yaml run --rm benchmarkoor
```

Prefer Tempo's `profiling` or `release` profile for measurements. A debug binary is useful for
correctness and diagnosis but is not a performance baseline.

## 5. How suite data is represented

The canonical format is `tempo-engine-suite/v1`, validated by
[`schemas/tempo-engine-suite-v1.schema.json`](../schemas/tempo-engine-suite-v1.schema.json).
A shortened block pair looks like this:

```json
{
  "format": "tempo-engine-suite/v1",
  "origin": {
    "kind": "tempo-native",
    "revision": "a41e3184...",
    "generator": "benchmarkoor integrations/tempo/export-suite.py",
    "seed": "20260819"
  },
  "chain": {
    "name": "tempo",
    "chain_id": 1337,
    "hardfork": "dev-all",
    "genesis": "genesis.json"
  },
  "defaults": {
    "wait_for_persistence": true,
    "wait_for_caches": true,
    "expected_status": "VALID"
  },
  "tests": [{
    "name": "tip20-new-recipients",
    "tags": ["tempo-native", "tip20", "transfer", "new-recipient"],
    "setup": [],
    "test": [
      {
        "rlp_file": "blocks/5.rlp",
        "bal_file": "blocks/5.bal",
        "block_number": 5,
        "block_hash": "0x32c25d6a...",
        "gas_used": 5410300,
        "transaction_count": 10,
        "expected_status": "VALID"
      },
      {
        "method": "reth_forkchoiceUpdated",
        "params": [{
          "headBlockHash": "0x32c25d6a...",
          "safeBlockHash": "0x32c25d6a...",
          "finalizedBlockHash": "0x32c25d6a..."
        }],
        "expected_status": "VALID"
      }
    ]
  }]
}
```

The important boundaries are:

- `setup`: replayed first and reported separately; it constructs the exact prestate.
- `test`: the calls whose correctness and performance form the benchmark result.
- `cleanup`: optional calls after measurement.
- `rlp_file` plus `bal_file`: expanded to `reth_newPayload` with the raw Tempo block and BAL.
- explicit `method` plus `params`: used for `reth_forkchoiceUpdated` and future RPC extensions.
- `expected_status`: normally `VALID`; negative suites may expect `INVALID` or an RPC error.
- `origin`, `revision`, `seed`, `hardfork`, tags, gas, and transaction count: provenance and
  dimensions needed to group and explain results.

For a normal block, keep `reth_newPayload` and the following forkchoice update together. A payload
returning `VALID` proves that Tempo accepted the bytes and computed the block/state identity expected
by the fixture; forkchoice proves that the accepted block can become canonical.

## 6. Generate a new Tempo-specific suite

First produce deterministic workload blocks on a Tempo node—using an integration test, txgen, or a
purpose-built generator. Record the seed and keep setup transactions outside the measured range.
Then export the canonical range from the still-running node with Benchmarkoor's RPC exporter:

```sh
export TEMPO_REPO=/absolute/path/to/tempo
cd "$BENCHMARKOOR_REPO"

./integrations/tempo/export-suite.py \
  --rpc-url http://127.0.0.1:8545 \
  --genesis "$TEMPO_REPO/crates/chainspec/src/genesis/dev.json" \
  --out /path/to/generated-tempo-suites/tip20/my-tip20-case \
  --name tip20-my-case \
  --description 'Describe the exact TIP-20 state transition' \
  --from-block 101 \
  --to-block 110 \
  --tag tempo-native \
  --tag tip20 \
  --tag transfer \
  --seed 42 \
  --revision "$(git -C "$TEMPO_REPO" rev-parse HEAD)" \
  --hardfork dev-all \
  --chain-id 1337
```

Blocks `1..100` become setup; blocks `101..110` become measured calls. The exporter fetches each
canonical block's raw RLP and block access list and adds the matching forkchoice call. `--force`
replaces an existing generated suite and clears stale block files.

Good Tempo-specific dimensions include:

- TIP-20 new, shared, initialized, and existing recipient state, including full blocks;
- transfer, approve/transfer-from, mint/burn, and multi-payment paths;
- fee-token and fee-AMM paths;
- direct, keychain, and key-authorized accounts;
- 2D, expiring, and protocol nonce behavior;
- DEX, storage-growth, subblock, invalid-block, hardfork, and gas-boundary cases.

Generate independent source chains when comparing independent state dimensions. If several
workloads share one source chain, a later suite's setup contains earlier workloads and may silently
warm or initialize sender/recipient state.

## 7. How result data is represented

After a run, Benchmarkoor writes:

```text
results/
├── runs/
│   ├── index.json
│   └── <timestamp>_<run-id>_tempo/
│       ├── config.json
│       ├── result.json
│       ├── benchmarkoor.log
│       ├── container.log
│       └── <test-name>/
│           ├── setup.response
│           ├── setup.result-details.json
│           ├── setup.result-aggregated.json
│           ├── test.response
│           ├── test.result-details.json
│           └── test.result-aggregated.json
└── suites/
    └── <suite-hash>/
        ├── summary.json
        └── stats.json
```

Use each layer for a different purpose:

| File | Meaning |
| --- | --- |
| `config.json` | Run status, Benchmarkoor version, host/container facts, exact Tempo image digest and command, genesis, rollback/cache settings |
| `result.json` | All per-test setup/test/cleanup aggregates used for programmatic analysis |
| `*.response` | Raw JSON-RPC responses, in request order; use these for protocol-level diagnosis |
| `*.result-details.json` | One value per RPC call: duration, status, gas, server timing, resources |
| `*.result-aggregated.json` | Step totals and per-method min/p50/p95/p99/mean/last statistics |
| `runs/index.json` | Lightweight catalog of every run and its pass/fail/step totals |
| `suites/<hash>/summary.json` | Immutable suite provenance, tags, transaction counts, and payload sizes |
| `suites/<hash>/stats.json` | Comparable successful samples grouped by suite/test/client |

Durations are stored in nanoseconds. For `reth_newPayload`, Tempo also returns an internal timing
breakdown. Benchmarkoor represents it as:

- `server_time_total` / `server_execution`: execution measured inside Tempo;
- `times.reth_newPayload`: HTTP-observed round trip;
- `persistence_wait`: time waiting for persistence;
- `execution_cache_wait`: time waiting for the execution cache;
- `sparse_trie_wait`: time waiting for the sparse trie;
- `gas_used_time_source`: the denominator used for gas throughput;
- `mgas_s`: `gas_used / selected_time`, expressed as millions of gas per second.

Always compare samples with the same `gas_used_time_source`. Keep HTTP duration and wait metrics in
the report so cache or persistence stalls are not mislabelled as EVM execution cost.

## 8. Inspect correctness and metrics

Find the newest successful Tempo run:

```sh
cd "$BENCHMARKOOR_REPO"

export RUN_ID="$(jq -r '
  [.entries[] | select(.instance.client == "tempo" and .status == "completed")]
  | max_by(.timestamp).run_id
' results/runs/index.json)"
export RUN_DIR="$BENCHMARKOOR_REPO/results/runs/$RUN_ID"

echo "$RUN_DIR"
```

Check reproducibility and correctness metadata:

```sh
jq '{
  status,
  suite_hash,
  image: .instance.image,
  image_sha256: .instance.image_sha256,
  client_version: .instance.client_version,
  test_counts
}' "$RUN_DIR/config.json"
```

Print a tab-separated metrics table suitable for a spreadsheet or report:

```sh
jq -r '
  ["test", "gas_used", "server_execution_ms", "http_new_payload_ms",
   "persistence_wait_ms", "execution_cache_wait_ms", "mgas_s"],
  (.tests | to_entries[] |
    .key as $name |
    .value.steps.test.aggregated as $a |
    [$name,
     $a.gas_used_total,
     ($a.server_time_total / 1000000),
     ($a.method_stats.times.reth_newPayload.last / 1000000),
     ($a.method_stats.persistence_wait.reth_newPayload.last / 1000000),
     ($a.method_stats.execution_cache_wait.reth_newPayload.last / 1000000),
     $a.method_stats.mgas_s.reth_newPayload.last])
  | @tsv
' "$RUN_DIR/result.json"
```

Inspect raw protocol responses when a test fails:

```sh
find "$RUN_DIR" -name '*.response' -print
sed -n '1,20p' "$RUN_DIR"/*/test.response
```

## 9. Generate reports

Build the host CLI once if it is not already available:

```sh
cd "$BENCHMARKOOR_REPO"
make build-core
```

Generate a human-readable report for one run:

```sh
./bin/benchmarkoor generate-markdown-summary \
  --run-dir "$RUN_DIR" \
  --output "$RUN_DIR/summary.md"
```

Regenerate the global run catalog and per-suite sample collection after importing or moving result
directories:

```sh
./bin/benchmarkoor generate-index-file \
  --method local \
  --results-dir "$BENCHMARKOOR_REPO/results"

./bin/benchmarkoor generate-suite-stats-file \
  --method local \
  --results-dir "$BENCHMARKOOR_REPO/results"
```

For a benchmark report, include at least:

1. suite hash, suite origin revision/seed/hardfork, and test tags;
2. Tempo image name, immutable digest, client version, and effective command;
3. host/runtime, architecture, CPU/memory limits, cache policy, and build profile;
4. passed/failed calls and expected payload status;
5. gas, transaction count, server execution, HTTP duration, all wait categories, and MGas/s;
6. sample count plus median, p95, and dispersion across fresh-node runs;
7. an explicit statement of which state is fresh, initialized, shared, or intentionally warmed.

Do not compare a debug host run with a profiling Docker run, different image digests, different
suite hashes, or different timing sources as if they were the same benchmark.

## 10. View results in HTML

Start the static Benchmarkoor UI after at least one run has populated `results/`:

```sh
cd "$BENCHMARKOOR_REPO"
docker compose -f docker-compose.tempo.yaml up -d --build ui
```

Open [http://localhost:8080](http://localhost:8080). The UI reads the mounted `results/` directory
directly, so this mode needs no API, database, login, or running Tempo node. It provides the run
catalog, run and test details, suite statistics, and comparisons. Refresh the browser after a new
benchmark finishes because automatic refresh is disabled for the static view.

Choose another host port if `8080` is occupied:

```sh
UI_PORT=8181 docker compose -f docker-compose.tempo.yaml up -d --build ui
```

Check the viewer and stop it with:

```sh
curl http://localhost:${UI_PORT:-8080}/results/runs/index.json | jq '.entries | length'
docker compose -f docker-compose.tempo.yaml logs -f ui
docker compose -f docker-compose.tempo.yaml stop ui
```

To turn only the generated Markdown summary into a standalone HTML file, install Pandoc and run:

```sh
pandoc "$RUN_DIR/summary.md" \
  --standalone \
  --metadata title="Tempo Benchmark Report" \
  --output "$RUN_DIR/summary.html"
```

On macOS, open it with `open "$RUN_DIR/summary.html"`. The Benchmarkoor UI is preferable when
exploring or comparing many runs; the standalone file is convenient to archive or share.

## 11. Troubleshooting

### Tempo exits and asks for `--consensus.signing-key`

Use a Benchmarkoor build containing the Tempo defaults from this repository. They add `--dev` and
`--dev.block-time=1h`. If you override the full instance `command`, include equivalent consensus
configuration yourself.

### Benchmarkoor waits forever for RPC on Docker Desktop

Run Benchmarkoor with `docker-compose.tempo.yaml`. The host binary path is intended primarily for
native Linux Docker, where the host can reach container bridge addresses.

### Manifest or referenced block file is missing

Set `TEMPO_SUITE_DIR` to the suite directory, not to `manifest.json`, and preserve its `genesis.json`
and `blocks/` children.

### Payload returns `INVALID`

Confirm the run used the manifest's genesis, started from a fresh data volume, replayed setup in
order, and did not let a local block producer advance the head. Then inspect `test.response` and
`container.log`.

### The run warns that genesis has no lower root anchor

`Could not reset safe/finalized ... head is the genesis block` is harmless for these fresh-genesis,
linear, one-test manifests. The measured replay can proceed and must still finish with
`status=completed`.

### Results contain zero CPU or disk deltas

Very short tests may complete between Docker Stats samples, and Docker Desktop exposes VM-level
information differently from native Linux. Increase workload size/repetitions and use native Linux
for stable resource measurements.

### A run was interrupted

Benchmarkoor normally removes its client container and volume. To remove leftovers:

```sh
cd "$BENCHMARKOOR_REPO"
./bin/benchmarkoor cleanup --force
```

Preserve the failed run directory first if its `container.log` or raw responses are needed for
diagnosis.
