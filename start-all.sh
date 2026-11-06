#!/usr/bin/env bash
# Run in WSL/Linux. Credentials are stored only in ignored data/, never here.
#
# Subcommands: start (default) | status | doctor | logs | token | stop
#
# The Windows half (start.cmd) starts Docker/MinerU/supervisor/models and then
# calls this script. Running it alone still works: it reports which Windows-side
# services are unreachable instead of failing with a mysterious RAG error.
set -Eeuo pipefail
umask 077
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"
export PATH="/usr/local/go/bin:$PATH"
ACTION="${1:-start}"
if [[ $# -gt 0 ]]; then shift; fi

probe() { curl --noproxy '*' --silent --fail --max-time 2 -o /dev/null "$1"; }
port_open() { timeout 2 bash -c "cat < /dev/null > /dev/tcp/$1/$2" 2>/dev/null; }
status_entry() {
    local name="$1" target="$2" hostport host port
    if [[ "$target" == tcp://* ]]; then
        # bash needs /dev/tcp/host/port, not host:port.
        hostport="${target#tcp://}"
        host="${hostport%%:*}"; port="${hostport##*:}"
        if port_open "$host" "$port"; then printf '  %-16s UP\n' "$name"; else printf '  %-16s DOWN\n' "$name"; fi
    elif probe "$target"; then
        printf '  %-16s UP\n' "$name"
    else
        printf '  %-16s DOWN\n' "$name"
    fi
}
state_value() { python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get(sys.argv[2],""))' "$ROOT/data/agentrag-launcher.json" "$1" 2>/dev/null || true; }

cmd_status() {
    local core_port
    core_port="$(state_value CORE_PORT)"; core_port="${core_port:-8080}"
    echo '[STATUS] AgentRAG'
    echo "  wsl  network : $(ip route show default 2>/dev/null | head -1 | sed 's/^default //')"
    echo "  wsl  mode    : $(grep -is '^\s*networkingMode' /mnt/c/Users/*/.wslconfig 2>/dev/null | tail -1 || echo 'unknown')"
    for entry in "MinerU :8000|http://127.0.0.1:8000/v1/health" \
                 "supervisor :8092|http://127.0.0.1:8092/healthz" \
                 "embedding :8081|http://127.0.0.1:8081/health" \
                 "rerank :8082|http://127.0.0.1:8082/health" \
                 "docreader :50051|tcp://127.0.0.1:50051" \
                 "Host :8090|http://127.0.0.1:8090/health" \
                 "Core :$core_port|http://127.0.0.1:$core_port/health"; do
        status_entry "${entry%%|*}" "${entry#*|}"
    done
}

cmd_doctor() {
    cmd_status
    echo
    local down=0
    probe http://127.0.0.1:8081/health || down=$((down + 1))
    probe http://127.0.0.1:8082/health || down=$((down + 1))
    probe http://127.0.0.1:8092/healthz || down=$((down + 1))
    if (( down == 3 )); then
        echo '[DIAG] 三个 Windows 侧服务从 WSL 全部不可达。'
        echo '       1) 先看 .wslconfig 是否是 networkingMode=mirrored；'
        echo '       2) Windows 侧服务没起时运行 start.cmd（会自动切模式并拉起）；'
        echo '       3) 仅想启动 Core/Host 时可直接 ./start-all.sh。'
    elif (( down > 0 )); then
        echo "[DIAG] 有 $down 个 Windows 侧服务不可达；运行 start.cmd 补齐。"
    else
        echo '[DIAG] Windows 侧服务全部可达。'
    fi
}

cmd_logs() {
    local run
    run="$(ls -dt "$ROOT"/data/launcher-run-* 2>/dev/null | head -1 || true)"
    [[ -n "$run" ]] || { echo '[LOGS] no launcher run found'; return 0; }
    echo "[LOGS] $run"
    for file in host.log core.log auth-result; do
        [[ -f "$run/$file" ]] && { echo "== $file =="; tail -n 40 "$run/$file"; }
    done
}

cmd_stop() {
    local run="" candidate pids pid
    # Newest run that actually owns processes: later idempotent runs create and
    # retire empty directories, and stopping the newest empty one would be a no-op.
    for candidate in $(ls -dt "$ROOT"/data/launcher-run-* 2>/dev/null); do
        if [[ -f "$candidate/pids" ]]; then run="$candidate"; break; fi
    done
    if [[ -z "$run" ]]; then echo '[STOP] no launcher-owned run recorded'; return 0; fi
    pids="$(cat "$run/pids")"
    for pid in $pids; do kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true; done
    sleep 2
    for pid in $pids; do kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true; done
    rm -f "$run/pids"
    echo "[STOP] stopped launcher-owned processes from $run"
}

case "$ACTION" in
    status|doctor) cmd_"$ACTION"; exit 0 ;;
    logs) cmd_logs; exit 0 ;;
    stop) cmd_stop; exit 0 ;;
    token) exec python3 "$ROOT/extensions/scripts/launcher_config.py" token ;;
    mcp-key) exec python3 "$ROOT/extensions/scripts/launcher_config.py" mcp-key ;;
    start) ;;
    *) echo "usage: $0 [start|status|doctor|logs|token|mcp-key|stop]" >&2; exit 2 ;;
esac

for command in python3 curl go setsid ip; do command -v "$command" >/dev/null || { echo "[ERROR] Missing $command" >&2; exit 1; }; done
CORE_BINARY="${CORE_BINARY:-$ROOT/WeKnora-lite}"
[[ -x "$CORE_BINARY" ]] || { echo '[ERROR] Build WeKnora-lite first (see extensions/README.md).' >&2; exit 1; }
mkdir -p "$ROOT/data"
RUN_DIR="${AGENTRAG_RUN_DIR:-$(mktemp -d "$ROOT/data/launcher-run-XXXXXX")}"
[[ ! -L "$RUN_DIR" ]] || { echo '[ERROR] Run directory must not be a symlink' >&2; exit 1; }
mkdir -p "$RUN_DIR" "$ROOT/data/bin"
[[ -z "$(find "$RUN_DIR" -mindepth 1 -maxdepth 1 -print -quit)" ]] || { echo '[ERROR] Choose an empty private AGENTRAG_RUN_DIR' >&2; exit 1; }
chmod 700 "$RUN_DIR"
HOST_PID= CORE_PID= AUTH_PID= STARTED=0

# Windows-side services cannot be started from here; the Windows half does that.
# What this side can do is prove they are reachable *from WSL*, which is the only
# path Core actually uses, and say what to do when they are not.
preflight_windows() {
    local missing=()
    probe "http://127.0.0.1:8092/healthz" || missing+=("supervisor :8092")
    probe "http://127.0.0.1:8000/v1/health" || missing+=("MinerU :8000")
    probe "http://127.0.0.1:8081/health" || missing+=("embedding :8081")
    probe "http://127.0.0.1:8082/health" || missing+=("rerank :8082")
    if ((${#missing[@]} == 0)); then
        echo '[READY] Windows-side services reachable from WSL'
        return 0
    fi
    echo "[WARN] 从 WSL 不可达: ${missing[*]}" >&2
    echo '[WARN] 常见原因：WSL 仍是 nat 模式（Windows 回环不可见），或 Windows 侧服务未启动。' >&2
    echo '[WARN] 处理：在 Windows 运行 start.cmd（自动切 mirrored 并拉起服务）；只想起 Core/Host 可忽略本警告。' >&2
    if [[ "${AGENTRAG_STRICT:-0}" == "1" ]]; then
        echo '[ERROR] AGENTRAG_STRICT=1 且 Windows 侧服务缺失' >&2
        return 1
    fi
    return 0
}

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
    rm -f "$RUN_DIR/pids"
    if ((STARTED == 1)); then
        echo "[STOP] Owned Core/Host stopped; logs: $RUN_DIR"
        echo '[STOP] Windows services (MinerU/models/supervisor) keep running; stop them with: start.cmd -Stop'
    fi
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
python3 "$ROOT/extensions/scripts/launcher_config.py" prepare "$RUN_DIR/private.env"
# Generated with shlex.quote from a fixed environment key set; root .env is NOT sourced.
source "$RUN_DIR/private.env"

# Idempotent rerun: if both owned services already answer, do not fight over ports.
if probe "http://127.0.0.1:$CORE_PORT/health" && probe http://127.0.0.1:8090/health; then
    echo "[READY] AgentRAG already running (Core :$CORE_PORT, Host :8090); nothing to do"
    rmdir "$RUN_DIR" 2>/dev/null || true
    exit 0
fi

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
preflight_windows
echo '[BUILD] Independent Extension Host (CGO_ENABLED=0)'
(cd "$ROOT/extensions" && CGO_ENABLED=0 GOWORK=off go build -mod=readonly -o "$RUN_DIR/extension-host" ./cmd/extension-host)
start_host() {
    (cd "$ROOT/extensions" && exec setsid "$RUN_DIR/extension-host") >> "$RUN_DIR/host.log" 2>&1 &
    HOST_PID=$!
    wait_health Host http://127.0.0.1:8090/health "$HOST_PID"
}
echo '[START] Host first; waiting for local DB and services (Core-independent readiness)'
start_host
STARTED=1
echo '[START] Core'
setsid "$CORE_BINARY" > "$RUN_DIR/core.log" 2>&1 &
CORE_PID=$!
wait_health Core "http://127.0.0.1:$CORE_PORT/health" "$CORE_PID"
printf '%s %s\n' "$HOST_PID" "$CORE_PID" > "$RUN_DIR/pids"
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

# MCP registration is best-effort: an unregistered tool costs a feature, not the
# system. The credential is a dedicated full-access key held in the 0600 state.
if [[ "${MCP_ENABLED:-true}" == "true" ]]; then
    if python3 "$ROOT/extensions/scripts/register_related_mcp.py"; then
        :
    else
        echo '[WARN] MCP 未注册：首次运行 ./start-all.sh mcp-key 签发管理员 key，之后自动注册/刷新' >&2
    fi
else
    echo '[MCP] disabled by configuration'
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
