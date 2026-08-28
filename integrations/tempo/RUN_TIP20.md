# Running the TIP-20 suite

Run from the repository root.

## Focused TIP-20 full-block suite

```sh
TEMPO_SUITE_DIR="$PWD/integrations/tempo/suites/tip20-full-blocks" \
TEMPO_ROLLBACK_STRATEGY=container-recreate \
TEMPO_IMAGE=docker.io/tempoxyz/tempo:latest \
docker compose -f docker-compose.tempo.yaml run --rm benchmarkoor
```

Use a local or custom Tempo image:

```sh
TEMPO_SUITE_DIR="$PWD/integrations/tempo/suites/tip20-full-blocks" \
TEMPO_ROLLBACK_STRATEGY=container-recreate \
TEMPO_IMAGE=tempoxyz/tempo:v1.13.1-maxperf-release-local \
docker compose -f docker-compose.tempo.yaml run --rm benchmarkoor
```

The suite is:

```text
integrations/tempo/suites/tip20-full-blocks
```

It currently contains:

```text
tempo/test_tip20_recipient.py::test_full_block[layout_single-recipient_shared-block_full-workload_tip20]
tempo/test_tip20_recipient.py::test_full_block[layout_single-recipient_random-block_full-workload_tip20]
tempo/test_tip20_recipient.py::test_full_block[layout_single-recipient_initialized-block_full-workload_tip20]
tempo/test_tip20_2d.py::test_full_block[layout_2d-lane_single-block_full-workload_tip20]
tempo/test_tip20_2d.py::test_full_block[layout_2d-lane_fresh-block_full-workload_tip20]
tempo/test_tip20_2d.py::test_full_block[layout_2d-lane_initialized-block_full-workload_tip20]
```

## Open results UI

```sh
docker compose -f docker-compose.tempo.yaml up --build ui
```

Then open:

```text
http://localhost:8080
```

Use another UI port:

```sh
UI_PORT=3000 docker compose -f docker-compose.tempo.yaml up --build ui
```

## Quick validation before running

```sh
python3 integrations/tempo/validate-suite.py \
  integrations/tempo/suites/tip20-full-blocks/manifest.json
```

## Notes

- Results are written under `./results`.
- The compose file mounts `TEMPO_SUITE_DIR` at `/app/tempo-suite`.
- `TEMPO_ROLLBACK_STRATEGY=container-recreate` is safe for this suite and keeps
  behavior consistent with the aggregate `all` suite.
