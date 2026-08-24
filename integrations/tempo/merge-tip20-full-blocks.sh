#!/usr/bin/env bash
# Rebuild the TIP-20 full-block aggregate from locally regenerated source suites.
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
SUITES_DIR="$SCRIPT_DIR/suites"
FIRST_SOURCE="$SUITES_DIR/tip20/full-block-shared-recipient/manifest.json"

if [[ ! -f "$FIRST_SOURCE" ]]; then
  echo "missing per-source TIP-20 suites under $SUITES_DIR/tip20" >&2
  echo "the checked-in corpus ships the self-contained aggregate in suites/tip20-full-blocks" >&2
  exit 1
fi

"$SCRIPT_DIR/merge-suites.py" \
  --out "$SUITES_DIR/tip20-full-blocks/manifest.json" \
  --name tip20-full-blocks \
  "$SUITES_DIR/tip20/full-block-shared-recipient/manifest.json" \
  "$SUITES_DIR/tip20/full-block-random-recipients/manifest.json" \
  "$SUITES_DIR/tip20/full-block-initialized-recipients/manifest.json" \
  "$SUITES_DIR/tip20/full-block-2d-single-lane/manifest.json" \
  "$SUITES_DIR/tip20/full-block-2d-fresh-lanes/manifest.json" \
  "$SUITES_DIR/tip20/full-block-2d-initialized-lanes/manifest.json"
