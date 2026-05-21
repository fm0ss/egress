#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:?ROOT_DIR is required}"
STATE_PATH="${STATE_PATH:?STATE_PATH is required}"
LEASE_FILE="${LEASE_FILE:?LEASE_FILE is required}"

cd "$ROOT_DIR"

if [[ ! -f "$LEASE_FILE" ]]; then
  exit 0
fi

LEASE_ID="$(python3 - <<'PY' "$LEASE_FILE"
import json, sys
print(json.load(open(sys.argv[1])).get("id", ""))
PY
)"

if [[ -n "$LEASE_ID" ]]; then
  go run ./cmd/egress destroy-lease -lease "$LEASE_ID" -state "$STATE_PATH" >/dev/null
fi

rm -f "$LEASE_FILE"
