#!/usr/bin/env bash
# Build one boundary-aware suite from every TIP-20 full-block fixture.
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
SUITES_DIR="$SCRIPT_DIR/suites"

"$SCRIPT_DIR/merge-suites.py" \
  --out "$SUITES_DIR/tip20-full-blocks/manifest.json" \
  --name tip20-full-blocks \
  "$SUITES_DIR/tip20/full-block-shared-recipient/manifest.json" \
  "$SUITES_DIR/tip20/full-block-random-recipients/manifest.json" \
  "$SUITES_DIR/tip20/full-block-initialized-recipients/manifest.json" \
  "$SUITES_DIR/tip20/full-block-2d-single-lane/manifest.json" \
  "$SUITES_DIR/tip20/full-block-2d-fresh-lanes/manifest.json" \
  "$SUITES_DIR/tip20/full-block-2d-initialized-lanes/manifest.json"
