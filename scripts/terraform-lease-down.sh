#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:?ROOT_DIR is required}"
STATE_PATH="${STATE_PATH:?STATE_PATH is required}"
LEASE_FILE="${LEASE_FILE:?LEASE_FILE is required}"

cd "$ROOT_DIR"
EGRESS_BIN="${EGRESS_BIN:-}"
if [[ -z "$EGRESS_BIN" ]]; then
  if [[ -x "$ROOT_DIR/egress" ]]; then
    EGRESS_BIN="$ROOT_DIR/egress"
  else
    EGRESS_BIN="go run ./cmd/egress"
  fi
fi

if [[ ! -f "$LEASE_FILE" ]]; then
  exit 0
fi

LEASE_ID="$(python3 - <<'PY' "$LEASE_FILE"
import json, sys
print(json.load(open(sys.argv[1])).get("id", ""))
PY
)"

if [[ -n "$LEASE_ID" ]]; then
  $EGRESS_BIN destroy-lease -lease "$LEASE_ID" -state "$STATE_PATH" >/dev/null
fi

rm -f "$LEASE_FILE"
