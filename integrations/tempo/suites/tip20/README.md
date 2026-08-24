# TIP-20 Engine API suites

These fixtures exercise direct `TIP20.transfer(address,uint256)` execution through Tempo's
authenticated Engine API. Each suite contains the source genesis, the exact Tempo block RLP and
block access list, and a semantic manifest that separates untimed setup blocks from one measured
workload block.

## Cases

| Suite | Measured block | Workload | Gas used |
| --- | ---: | --- | ---: |
| `shared-existing-recipient` | 4 | 10 fresh funded dev accounts transfer one unit each to funded dev account 19 | 3,168,760 |
| `new-recipients` | 5 | 10 fresh funded dev accounts transfer one unit to distinct previously untouched addresses `0x1000...0001` through `0x1000...000a` | 5,410,300 |
| `existing-recipients` | 12 | The same 10 initialized senders transfer another unit to the recipients created by setup block 5 | 417,300 |
| `full-block-shared-recipient` | 1 | 15,934 transfers from 10 funded dev accounts to one shared funded recipient | 499,029,664 |
| `full-block-random-recipients` | 1 | 1,779 transfers from one funded dev account to 1,779 distinct seeded random recipients | 499,161,634 |
| `full-block-initialized-recipients` | 31 | 16,022 transfers from one funded dev account to 16,022 distinct seeded random recipients created by the 20,000-transfer setup pass | 499,005,096 |
| `full-block-2d-single-lane` | 1 | 13,716 transfers from one funded dev account on ordered 2D nonce key 42 | 499,015,836 |
| `full-block-2d-fresh-lanes` | 6 | 1,774 transfers from one funded dev account, each on a distinct freshly created seeded 2D nonce lane | 499,002,304 |
| `full-block-2d-initialized-lanes` | 48 | 13,730 transfers from one funded dev account on 13,730 distinct seeded 2D nonce lanes created by the 20,000-transfer setup pass | 499,030,580 |

The six full-block cases are also available as the combined, boundary-aware
`suites/tip20-full-blocks/manifest.json`. It references the source fixture files instead of
duplicating them and marks every test as a separate chain segment. Regenerate it with:

```sh
./integrations/tempo/merge-tip20-full-blocks.sh
```

When using Docker Compose, mount `integrations/tempo/suites` and select
`/app/tempo-suite/tip20-full-blocks/manifest.json`; `container-recreate` restores genesis at each
segment boundary.

The token is the dev-genesis TIP-20 at `0x20c0000000000000000000000000000000000000`.
Every measured transaction succeeded in the source chain. The full blocks use Tempo's 500M block
gas limit: the shared and initialized-recipient cases measure balance-slot updates, while the fresh
random-recipient case measures recipient state creation. The 2D cases compare one ordered lane,
fresh parallel-lane creation, and updates to parallel lanes initialized during setup. Successful
replay requires both
`reth_newPayload` and the following `reth_forkchoiceUpdated` call to succeed, and the payload must
return `VALID`; this validates Tempo's computed block hash/state root against the recorded canonical
block.

The three small source chains were produced from Tempo revision
`a41e3184a2f1a50a4a0b60fcdd536c1a4cc2dae0` with seed `20260819`. The full-block source chains use
Tempo revision `1d61c6f3a67d3e3ea8b921d9db003b7fd07189ec` and txgen revision `1cda02e0`,
with their seeds recorded in each manifest. All use chain ID 1337 and the same exact genesis.
`shared-existing-recipient` and `new-recipients` use independent fresh source chains so their
prestates do not include another measured workload. `existing-recipients` intentionally shares the
new-recipient chain and includes its first-touch block in setup. The full initialized-recipient case
generates the same 20,000-address sequence twice with seed `20260822`: the first pass initializes
every address, and the second pass supplies the measured block. Empty blocks are retained so every
suite is independently replayable from genesis.

The initialized 2D-lane case generates the same 20,000-key sequence twice with seed `20260824`.
The first pass creates every lane at nonce 0; the measured pass uses the same keys at nonce 1.

## Replay locally

Start a fresh Tempo node for each manifest. Reusing a node would make the tests stateful and cause
the next suite's setup chain to conflict with the existing head.

```sh
tempo node \
  --dev \
  --dev.block-time 1h \
  --chain /path/to/selected-suite/genesis.json \
  --datadir /tmp/tempo-tip20-replay \
  --http --http.api all \
  --authrpc.addr 127.0.0.1 --authrpc.port 8551 \
  --debug.startup-sync-state-idle
```

From the Benchmarkoor repository, replay one suite against that process:

```sh
go run -tags containers_image_openpgp ./cmd/benchmarkoor replay \
  --manifest /path/to/benchmarkoor/integrations/tempo/suites/tip20/new-recipients/manifest.json \
  --engine-endpoint http://127.0.0.1:8551 \
  --jwt-file /tmp/tempo-tip20-replay/jwt.hex \
  --results-dir /tmp/benchmarkoor-tip20-results \
  --run-id tip20-new-recipients
```

Benchmarkoor records HTTP round-trip time, Tempo's server-side execution time, persistence wait,
execution-cache wait, sparse-trie wait, gas, and derived MGas/s. Use server execution time for EVM
comparisons and keep wait metrics visible when diagnosing end-to-end latency.

## Regenerate

The full-block transaction streams are generated with `txgen-tempo` from the specs in
`integrations/tempo/txgen`. Generate more transactions than fit, submit them to a deterministic
Tempo dev node configured with a 500M builder gas limit, then export the first full canonical block.
The 2D workloads use txgen's native `nonce_key` field: a literal selects one ordered lane and a
`uniform` generator creates independently scheduled parallel lanes.
For the initialized-recipient case, generate and mine the same spec once as setup, then generate it
again with the same seed after txgen has fetched the sender's new nonce:

```sh
txgen-tempo generate \
  --spec integrations/tempo/txgen/tip20-full-block-initialized-recipients.yaml \
  --count 20000 --seed 20260822 --rpc http://127.0.0.1:8545 \
  --output /tmp/tip20-setup.ndjson
bench send --input /tmp/tip20-setup.ndjson --rpc-url http://127.0.0.1:8545

# Wait until the setup transaction pool is empty, then repeat with the same seed.
txgen-tempo generate \
  --spec integrations/tempo/txgen/tip20-full-block-initialized-recipients.yaml \
  --count 20000 --seed 20260822 --rpc http://127.0.0.1:8545 \
  --output /tmp/tip20-measured.ndjson
bench send --input /tmp/tip20-measured.ndjson --rpc-url http://127.0.0.1:8545
```

Export a measured block from the still-running source node with
`integrations/tempo/export-suite.py`. For example:

```sh
./integrations/tempo/export-suite.py \
  --rpc-url http://127.0.0.1:8545 \
  --genesis "$TEMPO_REPO/crates/chainspec/src/genesis/dev.json" \
  --out integrations/tempo/suites/tip20/new-recipients \
  --name tip20-new-recipients \
  --description 'Ten direct TIP-20 transfers from distinct senders to distinct new recipients' \
  --from-block 5 --to-block 5 \
  --tag tempo-native --tag tip20 --tag transfer --tag new-recipient --tag state-growth \
  --seed 20260819 --revision "$(git -C "$TEMPO_REPO" rev-parse HEAD)" \
  --hardfork dev-all --chain-id 1337 --force
```

The manifest records fixture provenance. Performance reports should additionally record the Tempo
and Benchmarkoor revisions, build profile, host, and repeated-run statistics; a single local replay
is a correctness smoke test and only a directional performance sample.
