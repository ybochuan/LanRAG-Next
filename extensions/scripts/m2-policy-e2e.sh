#!/usr/bin/env bash
# Owned Linux/WSL fixture; no production DB/model/token is used.
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
scratch=$(mktemp -d /tmp/agentrag-m2-e2e-XXXXXX)
host_pid=""
cleanup() {
  if [[ -n "$host_pid" ]]; then
    kill -TERM "$host_pid" 2>/dev/null || true
    wait "$host_pid" 2>/dev/null || true
  fi
  case "$scratch" in /tmp/agentrag-m2-e2e-*) rm -rf -- "$scratch" ;; esac
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
export PATH="/usr/local/go/bin:$PATH"
cd "$repo/extensions"
go test -mod=readonly -c -o "$scratch/host.test" ./host
AGENTRAG_M2_E2E_READY="$scratch/ready" "$scratch/host.test" -test.run '^TestM2PolicyHostProcess$' >"$scratch/host.log" 2>&1 &
host_pid=$!
for ((attempt=0; attempt<250; attempt++)); do
  [[ -f "$scratch/ready" ]] && break
  if ! kill -0 "$host_pid" 2>/dev/null; then cat "$scratch/host.log"; exit 1; fi
  sleep 0.1
done
if [[ ! -f "$scratch/ready" ]]; then cat "$scratch/host.log"; exit 1; fi
cd "$repo"
CGO_ENABLED=1 AGENTRAG_M2_LIVE_HOST=owned-harness go test -mod=readonly -tags sqlite_fts5 ./internal/application/service/chat_pipeline -run '^TestSearchParallelBridgeLiveDatabasePolicy$' -count=1 -v
kill -TERM "$host_pid"
if ! wait "$host_pid"; then cat "$scratch/host.log"; exit 1; fi
host_pid=""
printf '%s\n' 'M2 POLICY CROSS-PROCESS E2E: PASS'
