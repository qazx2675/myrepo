#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

BIN="./vm-shares-normal"

if [ ! -x "$BIN" ]; then
  echo "[vm-shares-normal.sh] binary not found, building..." >&2
  if [ -d vendor ]; then
    GOFLAGS=-mod=vendor GOPROXY=off go build -o vm-shares-normal .
  else
    go build -o vm-shares-normal .
  fi
fi

exec "$BIN" "$@"
