#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${GITHUB_ACTION_PATH:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$ROOT_DIR"

EGRESS_BIN="${EGRESS_BIN:-}"
if [[ -z "$EGRESS_BIN" ]]; then
  if [[ -x "$ROOT_DIR/egress" ]]; then
    EGRESS_BIN="$ROOT_DIR/egress"
  else
    EGRESS_BIN="go run ./cmd/egress"
  fi
fi

LEASE_ID="${INPUT_LEASE_ID:-}"
STATE_PATH="${INPUT_STATE_PATH:-.egress/state.json}"
ACCOUNT_NAME="${INPUT_ACCOUNT_NAME:-}"

if [[ -n "$LEASE_ID" ]]; then
  $EGRESS_BIN destroy-lease -lease "$LEASE_ID" -state "$STATE_PATH"
  exit 0
fi

if [[ -n "$ACCOUNT_NAME" ]]; then
  python3 - <<'PY'
import json, os, subprocess, sys
state_path = os.environ["INPUT_STATE_PATH"] if "INPUT_STATE_PATH" in os.environ else ".egress/state.json"
account_name = os.environ["INPUT_ACCOUNT_NAME"]
state = json.load(open(state_path))
leases = [l for l in state.get("leases", []) if l.get("provider") == "aws"]
target_accounts = [a for a in state.get("accounts", []) if a.get("name") == account_name or a.get("id") == account_name or a.get("aws_profile") == account_name]
if not target_accounts:
    sys.exit(f"account {account_name} not found in state")
target_ids = {a["id"] for a in target_accounts}
seen = set()
for lease in leases:
    if lease.get("account_id") not in target_ids:
        continue
    lease_id = lease.get("id")
    gateway = lease.get("gateway_id")
    if gateway in seen:
        continue
    seen.add(gateway)
    egress_bin = os.environ.get("EGRESS_BIN", "go run ./cmd/egress")
    subprocess.run(egress_bin.split() + ["destroy-lease", "-lease", lease_id, "-state", state_path], check=True)
PY
  exit 0
fi

$EGRESS_BIN cleanup-all -state "$STATE_PATH"
