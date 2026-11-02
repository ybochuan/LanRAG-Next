#!/usr/bin/env bash
# Run in WSL/Linux. Credentials are stored only in ignored data/, never here.
set -Eeuo pipefail
umask 077
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"
export PATH="/usr/local/go/bin:$PATH"
for command in python3 curl go setsid; do command -v "$command" >/dev/null || { echo "[ERROR] Missing $command" >&2; exit 1; }; done
CORE_BINARY="${CORE_BINARY:-$ROOT/WeKnora-lite}"
[[ -x "$CORE_BINARY" ]] || { echo '[ERROR] Build WeKnora-lite first (see extensions/README.md).' >&2; exit 1; }
mkdir -p "$ROOT/data"
RUN_DIR="${AGENTRAG_RUN_DIR:-$(mktemp -d "$ROOT/data/launcher-run-XXXXXX")}"
[[ ! -L "$RUN_DIR" ]] || { echo '[ERROR] Run directory must not be a symlink' >&2; exit 1; }
mkdir -p "$RUN_DIR" "$ROOT/data/bin"
[[ -z "$(find "$RUN_DIR" -mindepth 1 -maxdepth 1 -print -quit)" ]] || { echo '[ERROR] Choose an empty private AGENTRAG_RUN_DIR' >&2; exit 1; }
chmod 700 "$RUN_DIR"
HOST_PID= CORE_PID= AUTH_PID=
stop_child() {
    local pid="${1:-}" deadline
    [[ -n "$pid" ]] || return 0
    kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
    deadline=$((SECONDS + 35))
    while kill -0 "$pid" 2>/dev/null && (( SECONDS < deadline )); do sleep 0.2; done
    if kill -0 "$pid" 2>/dev/null; then kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true; fi
    wait "$pid" 2>/dev/null || true
}
cleanup() {
    local result=$?
    trap - EXIT INT TERM
    [[ -z "$AUTH_PID" ]] || kill -TERM "$AUTH_PID" 2>/dev/null || true
    stop_child "$CORE_PID"
    stop_child "$HOST_PID"
    echo "[STOP] Owned Core/Host stopped; logs: $RUN_DIR"
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
python3 "$ROOT/extensions/scripts/launcher_config.py" prepare "$RUN_DIR/private.env"
# Generated with shlex.quote from a fixed environment key set; root .env is NOT sourced.
source "$RUN_DIR/private.env"
python3 - "$CORE_PORT" <<'PY'
import socket, sys
for port in (int(sys.argv[1]), 8090):
    with socket.socket() as listener:
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        try: listener.bind(('127.0.0.1', port))
        except OSError: raise SystemExit('[ERROR] Port %s already in use; existing process left untouched' % port)
PY
# Core loads its own dotenv file; keep runtime defaults explicit for fresh installs.
export DB_DRIVER="${DB_DRIVER:-sqlite}" RETRIEVE_DRIVER="${RETRIEVE_DRIVER:-sqlite}"
export STORAGE_TYPE="${STORAGE_TYPE:-local}" STREAM_MANAGER_TYPE="${STREAM_MANAGER_TYPE:-memory}"
export LOCAL_STORAGE_BASE_DIR="${LOCAL_STORAGE_BASE_DIR:-$ROOT/data/files}"
export NEO4J_ENABLE=false ENABLE_GRAPH_RAG=false
export PORT="$CORE_PORT"
export SERVER_PORT="$CORE_PORT" SERVER_HOST=127.0.0.1
export AGENTRAG_MODE=worker
wait_health() {
    local name="$1" url="$2" pid="$3" deadline=$((SECONDS + ${AGENTRAG_START_TIMEOUT:-60}))
    while (( SECONDS < deadline )); do
        kill -0 "$pid" 2>/dev/null || { echo "[ERROR] $name exited before readiness; inspect $RUN_DIR" >&2; return 1; }
        if curl --noproxy '*' --fail --silent --max-time 2 "$url" > "$RUN_DIR/health.json"; then
            if [[ "$name" != Host ]] || python3 -c 'import json,sys; sys.exit(json.load(open(sys.argv[1])).get("status") != "ready")' "$RUN_DIR/health.json"; then
                echo "[READY] $name"; return 0
            fi
        fi
        sleep 0.25
    done
    echo "[ERROR] $name readiness timed out" >&2; return 1
}
echo '[BUILD] Independent Extension Host (CGO_ENABLED=0)'
(cd "$ROOT/extensions" && CGO_ENABLED=0 GOWORK=off go build -mod=readonly -o "$RUN_DIR/extension-host" ./cmd/extension-host)
start_host() {
    (cd "$ROOT/extensions" && exec setsid "$RUN_DIR/extension-host") >> "$RUN_DIR/host.log" 2>&1 &
    HOST_PID=$!
    wait_health Host http://127.0.0.1:8090/health "$HOST_PID"
}
echo '[START] Host first; waiting for local DB and services (Core-independent readiness)'
start_host
echo '[START] Core'
setsid "$CORE_BINARY" > "$RUN_DIR/core.log" 2>&1 &
CORE_PID=$!
wait_health Core "http://127.0.0.1:$CORE_PORT/health" "$CORE_PID"
# Validate upstream permissions separately: local readiness cannot depend on Core.
python3 "$ROOT/extensions/scripts/launcher_config.py" auth "$RUN_DIR/auth-result" &
AUTH_PID=$!
wait "$AUTH_PID"
AUTH_PID=
if [[ "$(cat "$RUN_DIR/auth-result")" == updated ]]; then
    echo '[AUTH] Official login/API Key bootstrap complete; password not saved'
    stop_child "$HOST_PID"; HOST_PID=
    python3 "$ROOT/extensions/scripts/launcher_config.py" env "$RUN_DIR/private.env"
    source "$RUN_DIR/private.env"
    start_host
else
    echo '[AUTH] Existing scoped machine credential verified'
fi
echo "[READY] AgentRAG: http://localhost:$CORE_PORT (Ctrl+C stops this launcher’s processes)"
echo "[LOGS] $RUN_DIR; native and extension databases are separate"
# Exit if either owned service dies; EXIT trap shuts down its sibling.
set +e
wait -n "$HOST_PID" "$CORE_PID"
result=$?
set -e
echo "[ERROR] An owned service exited unexpectedly (status=$result)" >&2
exit 1
