# Creating EEST-derived Tempo suites

This guide covers Tempo suites generated from Ethereum Execution Spec Tests
(EEST). For shared suite layout, replay, aggregate, validation, and commit
guidance, see `integrations/tempo/CREATE_SUITES.md`.

## Required inputs

Set:

- `EEST_REPO`: absolute path to an `ethereum/execution-specs` checkout.
- `TEMPO_REPO`: absolute path to a `tempoxyz/tempo` checkout. The exporter uses
  `crates/chainspec/src/genesis/dev.json`.
- `SUITE_OUT`: output directory for the generated suite.

Optional but commonly set:

- `SUITE_NAME`: stable suite name stored in `manifest.json`.
- `SUITE_DESCRIPTION`: human-readable suite description.
- `TEMPO_IMAGE`: Docker image used for generation, default
  `docker.io/tempoxyz/tempo:latest`.
- `UV_IMAGE`: EEST runner image, default
  `ghcr.io/astral-sh/uv:python3.11-bookworm`.
- `EEST_FORK`: EEST fork, default `Prague`.
- `GAS_MILLIONS`: EEST `--gas-benchmark-values`, default `10`.
- `BLOCK_TIME`: generation node block time, default `250ms` in `run-batch.sh`.
- `FORCE=true`: replace an existing output manifest.

If overriding `EOA_START`, also set the matching `EOA_START_HEX` so provenance
does not lose the full 256-bit value through shell integer handling.

## Main scripts

Use these scripts from the repository root.

| Script | Purpose | Typical output |
| --- | --- | --- |
| `integrations/tempo/run-batch.sh` | Generate a suite from one or more EEST test files or exact pytest node IDs. This is the normal path. | `SUITE_OUT/manifest.json`, `SUITE_OUT/genesis.json`, `SUITE_OUT/blocks/` |
| `integrations/tempo/run-one.sh` | Debug one selected EEST benchmark with a pytest `-k` filter. Useful while developing a new test. | One single-case suite |

## Generate a normal EEST batch suite

Use `run-batch.sh` for a new suite that should contain all selected passing
cases from one or more EEST files:

```sh
EEST_REPO=/absolute/path/to/execution-specs \
TEMPO_REPO=/absolute/path/to/tempo \
SUITE_OUT=/absolute/path/to/benchmarkoor/integrations/tempo/suites/my-new-suite \
SUITE_NAME=eest-prague-my-new-suite-10m \
SUITE_DESCRIPTION='EEST Prague my new 10M workloads regenerated on Tempo' \
GAS_MILLIONS=10 \
FORCE=true \
./integrations/tempo/run-batch.sh \
  tests/benchmark/compute/instruction/test_arithmetic.py \
  tests/benchmark/compute/instruction/test_comparison.py
```

You may also pass an exact pytest node ID to generate only one parametrized
case:

```sh
EEST_REPO=/absolute/path/to/execution-specs \
TEMPO_REPO=/absolute/path/to/tempo \
SUITE_OUT=/absolute/path/to/benchmarkoor/integrations/tempo/suites/point-evaluation-warm \
SUITE_NAME=eest-prague-point-evaluation-warm-10m \
SUITE_DESCRIPTION='EEST Prague KZG point-evaluation warm 10M workload' \
GAS_MILLIONS=30 \
FORCE=true \
./integrations/tempo/run-batch.sh \
  'tests/benchmark/compute/precompile/test_point_evaluation_warm.py::test_point_evaluation_warm[fork_Prague-benchmark_test-point_evaluation_warm_10m-benchmark-gas-value_30M]'
```

The script starts a temporary Tempo container, runs EEST through the Tempo
adapter, writes a capture file, exports passing transaction-bearing cases, and
then removes the temporary generation container.

For EEST capture-based exports, the last transaction-bearing block of each
passing pytest case becomes the measured Benchmarkoor `test` block; earlier
blocks in that case become setup.

## Develop or override an EEST test

Tempo-specific EEST overrides live in:

```text
integrations/tempo/eest_overrides/
```

When developing an override:

1. Add or edit the override in `integrations/tempo/eest_overrides/`.
2. Copy that file into the matching path under `EEST_REPO/tests/...` before
   running `run-batch.sh`.
3. Generate the target suite.
4. Remove the temporary copied file from the EEST checkout unless it is meant
   to stay there.

Keep setup vs measured work explicit. If a setup workload transaction mutates a
generator object, use a fresh generator for the measured workload so the
measured transaction keeps the intended gas limit.

## EEST checklist

Before committing:

- Temporary files copied into the EEST checkout have been removed.
