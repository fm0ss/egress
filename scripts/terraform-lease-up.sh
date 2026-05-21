#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:?ROOT_DIR is required}"
STATE_PATH="${STATE_PATH:?STATE_PATH is required}"
AWS_PROFILE_NAME="${AWS_PROFILE_NAME:?AWS_PROFILE_NAME is required}"
ACCOUNT_NAME="${ACCOUNT_NAME:-$AWS_PROFILE_NAME}"
LOCATION="${LOCATION:?LOCATION is required}"
ACCESS_MODE="${ACCESS_MODE:-proxy}"
WORKLOAD_ID="${WORKLOAD_ID:?WORKLOAD_ID is required}"
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
mkdir -p "$(dirname "$STATE_PATH")" "$(dirname "$LEASE_FILE")"

$EGRESS_BIN import-aws-cli -profile "$AWS_PROFILE_NAME" -name "$ACCOUNT_NAME" -state "$STATE_PATH" >/dev/null
$EGRESS_BIN provision -account "$ACCOUNT_NAME" -location "$LOCATION" -access-mode "$ACCESS_MODE" -workload "$WORKLOAD_ID" -state "$STATE_PATH" >"$LEASE_FILE"
