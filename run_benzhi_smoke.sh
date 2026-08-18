#!/usr/bin/env bash
#
# run_benzhi_smoke.sh - deterministic end-to-end smoke test for QuorumForge.
#
# The script never builds from source and never accesses the network. It
# drives a real ceremony through the public HTTP API on the localhost
# loopback, verifies the observable state, then cleans up every process and
# temporary file.
#
# It expects a pre-built service binary named `quorumforge` to be present
# either on PATH (the Docker image installs it at /usr/local/bin/quorumforge)
# or alongside this script in the project root. It fails fast on the first
# error.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK_DIR="$(mktemp -d)"
DB="${WORK_DIR}/smoke.db"
ADDR="127.0.0.1:18099"
PID=""

# Locate the already-compiled service binary. The smoke test must not
# compile the service or reach a module proxy / checksum database.
if [[ -n "${QUORUMFORGE_BIN:-}" ]]; then
  BIN="${QUORUMFORGE_BIN}"
elif command -v quorumforge >/dev/null 2>&1; then
  BIN="$(command -v quorumforge)"
elif [[ -x "${ROOT_DIR}/quorumforge" ]]; then
  BIN="${ROOT_DIR}/quorumforge"
else
  echo "quorumforge binary not found" >&2
  echo "build it first: go build -o quorumforge ./cmd/quorumforge" >&2
  exit 1
fi

cleanup() {
  local rc=$?
  if [[ -n "${PID}" ]] && kill -0 "${PID}" 2>/dev/null; then
    kill "${PID}" 2>/dev/null || true
    wait "${PID}" 2>/dev/null || true
  fi
  rm -rf "${WORK_DIR}"
  exit "${rc}"
}
trap cleanup EXIT

echo "[smoke] starting service on ${ADDR}"
"${BIN}" -db "${DB}" -addr "${ADDR}" &
PID=$!

# Wait for the service to become healthy.
echo "[smoke] waiting for health endpoint"
for _ in $(seq 1 100); do
  if curl -fsS "http://${ADDR}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

health="$(curl -fsS "http://${ADDR}/healthz")"
echo "[smoke] health: ${health}"
[[ "${health}" == *'"status":"ok"'* ]] || { echo "health check failed" >&2; exit 1; }

echo "[smoke] creating ceremony"
curl -fsS -X POST "http://${ADDR}/api/v1/ceremonies" \
  -H 'Content-Type: application/json' \
  -d '{"id":"smoke-c1"}' >/dev/null

echo "[smoke] locking ceremony"
lock="$(curl -fsS -X POST "http://${ADDR}/api/v1/ceremonies/smoke-c1/lock" \
  -H 'Content-Type: application/json' \
  -d '{"operation":"smoke-lock","revision":0,"digest":"sha256:smoke","key_version":"key-v1","policy_version":"policy-v1","participants":["alice","bob","carol"]}')"
echo "[smoke] lock result: ${lock}"
[[ "${lock}" == *'"state":"gathering-witnesses"'* ]] || { echo "lock did not reach gathering-witnesses" >&2; exit 1; }

echo "[smoke] querying ceremony"
view="$(curl -fsS "http://${ADDR}/api/v1/ceremonies/smoke-c1")"
echo "[smoke] view: ${view}"
[[ "${view}" == *'"revision":1'* ]] || { echo "unexpected revision" >&2; exit 1; }
[[ "${view}" == *'"threshold":3'* ]] || { echo "unexpected threshold" >&2; exit 1; }

echo "[smoke] operations page"
page="$(curl -fsS "http://${ADDR}/")"
[[ "${page}" == *'QuorumForge'* ]] || { echo "operations page missing" >&2; exit 1; }

echo "[smoke] OK"
