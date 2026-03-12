#!/usr/bin/env bash
set -euo pipefail
# Source this file to configure the environment to use the repo-local Go binary
# Usage: source ./scripts/setup-go.sh

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
LOCAL_GO="$ROOT_DIR/ohmf/.tools/go/bin/go"

if [[ -x "$LOCAL_GO" ]]; then
  export GO_CMD="$LOCAL_GO"
  export PATH="$(dirname "$LOCAL_GO"):$PATH"
  echo "Using local go at $LOCAL_GO"
else
  echo "Local go not found at $LOCAL_GO; leaving GO_CMD/PATH unchanged"
fi
