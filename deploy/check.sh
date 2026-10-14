#!/usr/bin/env bash
# Tolikopa pre-merge gate. Run from the repo root: bash deploy/check.sh
#
# This exists because several safety properties are asserted by unit tests that
# nothing was running:
#   - appcatalog.TestNoCatalogItemIsForbidden — no catalog entry names a package
#     whose removal breaks Windows
#   - the shared golden vector — agent and server compute the same catalog hash,
#     otherwise the drift handshake marks every agent stale
#   - collector.TestMatchStartupItemDoesNotOverMatch — "Skype" must not disable
#     Skype for Business
#   - the synccatalog diff — the server's catalog.json is a generated copy
#
# Go is not installed on the admin workstation, so everything runs in the pinned
# golang image (same version as the production build). Docker is the only
# prerequisite.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="golang:1.26"
MODCACHE="tolikopa-gomod"
fail=0

# Git Bash on Windows rewrites anything that looks like a POSIX path in a command
# line ("/repo/agent" -> "C:/Program Files/Git/repo/agent"), which breaks every
# container path. MSYS_NO_PATHCONV=1 disables that, and the host mount then has to
# be a native Windows path, which `pwd -W` provides.
HOST_ROOT="$ROOT"
if command -v cygpath >/dev/null 2>&1; then
  HOST_ROOT="$(cygpath -w "$ROOT")"
  export MSYS_NO_PATHCONV=1
fi

run_go() { # run_go <module-dir> <shell-command>
  docker run --rm -v "$HOST_ROOT:/repo" -v "$MODCACHE:/go/pkg/mod" -w "/repo/$1" "$IMAGE" sh -c "$2"
}

step() { printf '\n\033[1m== %s\033[0m\n' "$1"; }
check() { if "$@"; then echo "  ok"; else echo "  FAILED"; fail=1; fi }

step "agent — build, vet, test (linux + windows)"
check run_go agent 'go build ./... && go vet ./... && go test ./... >/dev/null &&
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./... &&
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...'

step "server — build, vet, test"
check run_go server 'go build ./... && go vet ./... && go test ./... >/dev/null'

step "catalog — server copy is in sync with the agent's canonical file"
check run_go server 'go run ./tools/synccatalog -check'

step "web — typecheck and build"
if [ -d "$ROOT/web/node_modules" ]; then
  check bash -c "cd '$ROOT/web' && npm run typecheck --silent && npm run build --silent >/dev/null"
else
  echo "  SKIPPED (web/node_modules missing — run 'npm install' in web/)"
fi

step "secrets — no credential literals in tracked files"
# .env is gitignored and is the only place a secret may live.
if grep -rIn --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=.git \
     --exclude='*.env' -E '(password|passwd|şifre)[[:space:]]*[:=][[:space:]]*["'"'"'`][^"'"'"'`]{6,}' \
     "$ROOT/docs" "$ROOT/deploy" 2>/dev/null | grep -v '\.env'; then
  echo "  FAILED — a credential literal appears above"
  fail=1
else
  echo "  ok"
fi

printf '\n'
if [ "$fail" -ne 0 ]; then
  echo "check.sh: FAILED"
  exit 1
fi
echo "check.sh: all checks passed"
