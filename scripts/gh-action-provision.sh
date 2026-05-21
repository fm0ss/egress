#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${GITHUB_ACTION_PATH:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$ROOT_DIR"

PROFILE="${INPUT_AWS_PROFILE:-}"
ACCOUNT_NAME="${INPUT_ACCOUNT_NAME:-}"
LOCATION="${INPUT_LOCATION:-}"
ACCESS_MODE="${INPUT_ACCESS_MODE:-proxy}"
WORKLOAD_ID="${INPUT_WORKLOAD_ID:-github-actions-${GITHUB_RUN_ID:-manual}}"
STATE_PATH="${INPUT_STATE_PATH:-.egress/state.json}"

if [[ -z "$LOCATION" ]]; then
  echo "location input is required" >&2
  exit 1
fi

if [[ -z "$PROFILE" ]]; then
  PROFILE="egress-ci"
  mkdir -p "$HOME/.aws"
  aws configure set aws_access_key_id "${AWS_ACCESS_KEY_ID:?AWS_ACCESS_KEY_ID is required}" --profile "$PROFILE"
  aws configure set aws_secret_access_key "${AWS_SECRET_ACCESS_KEY:?AWS_SECRET_ACCESS_KEY is required}" --profile "$PROFILE"
  if [[ -n "${AWS_SESSION_TOKEN:-}" ]]; then
    aws configure set aws_session_token "$AWS_SESSION_TOKEN" --profile "$PROFILE"
  fi
  if [[ -n "${AWS_REGION:-}" ]]; then
    aws configure set region "$AWS_REGION" --profile "$PROFILE"
  fi
fi

if [[ -z "$ACCOUNT_NAME" ]]; then
  ACCOUNT_NAME="$PROFILE"
fi

mkdir -p "$(dirname "$STATE_PATH")"

go run ./cmd/egress import-aws-cli -profile "$PROFILE" -name "$ACCOUNT_NAME" -state "$STATE_PATH" >/tmp/egress-import.json
go run ./cmd/egress provision -account "$ACCOUNT_NAME" -location "$LOCATION" -access-mode "$ACCESS_MODE" -workload "$WORKLOAD_ID" -state "$STATE_PATH" >/tmp/egress-lease.json

python3 - <<'PY'
import json, os
lease = json.load(open("/tmp/egress-lease.json"))
github_output = os.environ["GITHUB_OUTPUT"]
github_env = os.environ["GITHUB_ENV"]

with open(github_output, "a", encoding="utf-8") as fh:
    for key in ["id", "public_ip", "endpoint", "region", "location", "access_mode"]:
        value = lease.get(key, "")
        fh.write(f"{key}={value}\n")
    fh.write("lease_json<<EOF\n")
    fh.write(json.dumps(lease))
    fh.write("\nEOF\n")

env_map = lease.get("connection", {}).get("env", {}) or {}
if env_map:
    with open(github_env, "a", encoding="utf-8") as fh:
        for key, value in env_map.items():
            fh.write(f"{key}={value}\n")
PY

cat /tmp/egress-lease.json
