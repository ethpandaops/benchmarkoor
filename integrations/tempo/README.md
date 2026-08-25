# Tempo integration

This directory contains Benchmarkoor's Tempo suite tooling and checked-in Tempo
benchmark suites. Tempo suites are portable `tempo-engine-suite/v1` manifests:
Benchmarkoor replays raw canonical Tempo blocks through the Reth-compatible
Engine API surface and reports correctness and performance.

## Contents

- `suites/all`: the full checked-in Tempo corpus.
- `suites/tip20-full-blocks`: focused high-gas TIP-20 full-block workloads.
- `suites/point-evaluation-warm`: focused KZG point-evaluation warm workload.
- `export-suite.py`: export canonical blocks from a running Tempo node.
- `validate-suite.py`: validate suite manifests and referenced block files.
- `merge-suites.py` and `merge-all-suites.sh`: maintainer helpers for rebuilding
  aggregate manifests.
- `tempo_eest_adapter.py`, `run-batch.sh`, and `run-one.sh`: EEST-to-Tempo
  capture tooling.

## Creating Suites

- For suites generated directly from a running Tempo node, see
  `integrations/tempo/CREATE_SUITES.md`.
- For EEST-derived Tempo suites, see
  `integrations/tempo/CREATE_SUITES_EEST.md`.

## Run The Full Suite

Run the checked-in `all` suite with container recreation at source-suite
boundaries. This takes around 16 minutes on a typical local Docker setup:

```sh
TEMPO_SUITE_DIR="$PWD/integrations/tempo/suites/all" \
TEMPO_ROLLBACK_STRATEGY=container-recreate \
TEMPO_IMAGE=docker.io/tempoxyz/tempo:latest \
docker compose -f docker-compose.tempo.yaml run --rm benchmarkoor
```

The compose file mounts `TEMPO_SUITE_DIR` at `/app/tempo-suite`, writes results
to `./results`, and defaults to `container-recreate` because each merged source
suite starts from genesis.

For a much quicker smoke run, execute the smaller `tip20-full-blocks` suite:

```sh
TEMPO_SUITE_DIR="$PWD/integrations/tempo/suites/tip20-full-blocks" \
TEMPO_ROLLBACK_STRATEGY=container-recreate \
TEMPO_IMAGE=docker.io/tempoxyz/tempo:latest \
docker compose -f docker-compose.tempo.yaml run --rm benchmarkoor
```

## Run The UI

After a run, start the static Tempo results UI:

```sh
docker compose -f docker-compose.tempo.yaml up --build ui
```

Open `http://localhost:8080`. Set `UI_PORT` to use another host port:

```sh
UI_PORT=3000 docker compose -f docker-compose.tempo.yaml up --build ui
```

The Tempo UI compose service serves `./results` read-only and uses
`ui/public/config.tempo-static.json`, so it does not require the API service or
login.
