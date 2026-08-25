#!/usr/bin/env bash
# Rebuild the all aggregate from locally regenerated per-source suites.
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
SUITES_DIR="$SCRIPT_DIR/suites"
FIRST_SOURCE="$SUITES_DIR/eest/batch-arithmetic-10m/manifest.json"

if [[ ! -f "$FIRST_SOURCE" ]]; then
  echo "missing per-source suites under $SUITES_DIR/eest and $SUITES_DIR/tip20" >&2
  echo "the checked-in corpus ships self-contained aggregates in suites/all and suites/tip20-full-blocks" >&2
  exit 1
fi

"$SCRIPT_DIR/merge-suites.py" \
  --out "$SUITES_DIR/all/manifest.json" \
  --name tempo-complete-benchmark-suite \
  --copy-files \
  "$SUITES_DIR/eest/batch-arithmetic-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-bitwise-context-flow-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-stack-memory-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-instruction-core-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-comparison-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-keccak-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-call-context-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-log-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-storage-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-system-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-precompile-basic-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-precompile-modexp-10m/manifest.json" \
  "$SUITES_DIR/eest/batch-scenarios-small-10m/manifest.json" \
  "$SUITES_DIR/point-evaluation-warm/manifest.json" \
  "$SUITES_DIR/tip20/existing-recipients/manifest.json" \
  "$SUITES_DIR/tip20/full-block-2d-fresh-lanes/manifest.json" \
  "$SUITES_DIR/tip20/full-block-2d-initialized-lanes/manifest.json" \
  "$SUITES_DIR/tip20/full-block-2d-single-lane/manifest.json" \
  "$SUITES_DIR/tip20/full-block-initialized-recipients/manifest.json" \
  "$SUITES_DIR/tip20/full-block-random-recipients/manifest.json" \
  "$SUITES_DIR/tip20/full-block-shared-recipient/manifest.json" \
  "$SUITES_DIR/tip20/new-recipients/manifest.json" \
  "$SUITES_DIR/tip20/shared-existing-recipient/manifest.json"
