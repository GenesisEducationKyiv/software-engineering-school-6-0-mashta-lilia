#!/usr/bin/env bash
# End-to-end test runner. Boots a self-contained Docker stack (postgres,
# redis, mailpit, app), installs Playwright deps if needed, runs the
# browser tests, then tears everything down (even on failure).
#
# Prereqs: git, docker (with docker compose), node + npm.
# Designed to run identically on a developer machine and in CI.
set -euo pipefail

# Resolve the repo root regardless of where the user invokes this script.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${E2E_DIR}/docker-compose.e2e.yml"

PROJECT_NAME="${PROJECT_NAME:-grn-e2e}"

cleanup() {
  echo "==> Tearing down e2e stack"
  docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> Bringing up e2e stack (postgres, redis, mailpit, app)"
docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" up -d --build --wait

echo "==> Installing npm deps (if needed)"
cd "${E2E_DIR}"
if [ ! -d node_modules ]; then
  # npm ci enforces the committed lockfile — same versions every run on
  # every machine. If the lockfile is missing or out of sync, fail loudly
  # rather than silently resolving fresh versions via npm install.
  npm ci
fi

echo "==> Ensuring Playwright Chromium is installed"
# --with-deps is a no-op on Windows / macOS; required on bare Linux CI.
npx playwright install --with-deps chromium

echo "==> Running Playwright tests"
npx playwright test "$@"
