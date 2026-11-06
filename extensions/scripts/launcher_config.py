#!/usr/bin/env python3
"""Local launcher configuration/authentication. Never source arbitrary shell files."""
import argparse
import getpass
import json
import os
import re
from pathlib import Path
import secrets
import shlex
import sqlite3
import sys
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
STATE = Path(os.environ.get("AGENTRAG_LAUNCHER_CONFIG", ROOT / "data/agentrag-launcher.json")).resolve()
ALLOWED = set("AGENTRAG_MANAGEMENT_KEY AGENTRAG_MODE AGENTRAG_EXT_DB AGENTRAG_TENANT_ID AGENTRAG_KB_IDS AGENTRAG_WEKNORA_URL AGENTRAG_WEKNORA_API_KEY AGENTRAG_RECONCILE_INTERVAL MINERU_ENDPOINT AGENTRAG_PARSER_UPSTREAM_URL AGENTRAG_PARSER_UPSTREAM_KEY AGENTRAG_PARSER_PROXY_KEY AGENTRAG_PARSER_ARTIFACT_ROOT AGENTRAG_PARSER_SCOPES ENV_FILE DB_PATH CORE_PORT WEKNORA_WEB_DIR AGENTRAG_PARSER_PROXY_ADDR SSRF_DISABLE DOCREADER_ADDR DOCREADER_TRANSPORT".split())
# Kept in the 0600 state file but never written into private.env: the Host and
# Core have no use for a supervisor credential, and every extra copy is another
# place for it to leak into a log or a child environment.
STATE_ONLY = {
    "MODEL_SUPERVISOR_TOKEN", "MODEL_SUPERVISOR_ADDR", "MODEL_SUPERVISOR_BIN",
    "MODEL_SUPERVISOR_STATE", "MODEL_SERVICES", "DOCKER_SERVICES", "MCP_ENABLED",
    "MCP_ADMIN_KEY", "AGENTRAG_DOCREADER_IMAGE",
}
# The two local runtimes the console manages. ctx/threads match the specs the
# instances were last started with; a caller can override the whole list with
# AGENTRAG_MODEL_SERVICES as JSON.
DEFAULT_MODELS = [
    {"id": "embedding", "model": "Qwen3-Embedding-4B-Q6_K.gguf", "task_type": "embedding", "port": 8081, "ctx_size": 4096, "threads": 8},
    {"id": "rerank", "model": "qwen3-reranker-0.6b-q8_0.gguf", "task_type": "rerank", "port": 8082, "ctx_size": 4096, "threads": 8},
]


def save(values):
    STATE.parent.mkdir(parents=True, exist_ok=True)
    temporary = STATE.with_name(STATE.name + "." + secrets.token_hex(6) + ".tmp")
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as output:
        json.dump(values, output)
    os.replace(temporary, STATE)


def load():
    return json.loads(STATE.read_text(encoding="utf-8")) if STATE.exists() else {}


def path(value):
    result = Path(value).expanduser()
    return (ROOT / result).resolve() if not result.is_absolute() else result.resolve()


def dotenv_value(filename, key, default):
    if not filename.is_file():
        return default
    for line in filename.read_text(encoding="utf-8-sig").splitlines():
        match = re.match(r"^\s*(?:export\s+)?" + re.escape(key) + r"\s*(?:=|:)\s*(.*)$", line)
        if match:
            values = shlex.split(match.group(1), comments=True)
            if len(values) > 1 or (values and "$" in values[0]):
                raise RuntimeError("use an explicit DB_PATH environment override for a complex dotenv path expression")
            return values[0] if values else ""
    return default


def tcp_open(host, port, timeout=0.3):
    import socket
    try:
        with socket.create_connection((host, int(port)), timeout=timeout):
            return True
    except OSError:
        return False


LOOPBACK_MINERU = "http://127.0.0.1:8000"


def networking_mode():
    """Active WSL networking mode, or "" when this is not WSL."""
    import subprocess
    try:
        result = subprocess.run(["wslinfo", "--networking-mode"], capture_output=True, text=True, timeout=2)
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout.strip().lower()
    except (OSError, subprocess.SubprocessError):
        pass
    for config in Path("/mnt/c/Users").glob("*/.wslconfig"):
        try:
            match = re.search(r"(?im)^\s*networkingMode\s*=\s*(\w+)", config.read_text(encoding="utf-8", errors="ignore"))
        except OSError:
            continue
        if match:
            return match.group(1).lower()
    return ""


def default_gateway():
    import subprocess
    try:
        routes = subprocess.check_output(["ip", "route", "show", "default"], text=True).split()
    except (OSError, subprocess.SubprocessError):
        return ""
    if "via" in routes:
        index = routes.index("via") + 1
        if index < len(routes):
            return routes[index]
    return ""


def reachable_origin(origin):
    """True when a MinerU origin answers its health endpoint."""
    try:
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open(origin.rstrip("/") + "/v1/health", timeout=0.6) as response:
            return response.status == 200
    except (OSError, ValueError):
        return False


def normalize_mineru(value):
    """Re-validate the MinerU origin against the current network.

    A stored origin that stopped answering is the classic aftermath of a network
    mode switch: the old address came from a gateway guess that mirrored mode
    never has. Loopback is the right answer whenever the local service is up, and
    an unreachable gateway address must not survive into the Host environment.
    """
    if value:
        if reachable_origin(value):
            return value
        if reachable_origin(LOOPBACK_MINERU):
            print("[MINERU] configured origin %s is unreachable; using %s" % (value, LOOPBACK_MINERU), file=sys.stderr)
            return LOOPBACK_MINERU
        return value
    if reachable_origin(LOOPBACK_MINERU):
        return LOOPBACK_MINERU
    # Only NAT needs a Windows host address, and only the real gateway is
    # meaningful there; mirrored mode must never invent one.
    if networking_mode() == "nat":
        gateway = default_gateway()
        if gateway:
            return "http://" + gateway + ":8000"
    return LOOPBACK_MINERU


def normalize_docreader(saved):
    """Decide the docreader address from what is actually running.

    Core reads DOCREADER_ADDR once at startup, so the value written here is what
    the parser engine gets. A dangling address is worse than none: the native
    settings page would claim "connected" while every upload fails. Explicit
    operator addresses are kept as-is; only the built-in loopback default is
    re-checked on every prepare.
    """
    explicit = os.environ.get("DOCREADER_ADDR")
    if explicit is not None:
        return explicit
    candidate = saved.get("DOCREADER_ADDR") or "127.0.0.1:50051"
    if candidate not in ("127.0.0.1:50051", "localhost:50051"):
        return candidate
    return candidate if tcp_open("127.0.0.1", 50051) else ""


def service_defaults(saved):
    """Runtime services the launcher owns. Values live only in the state file."""
    services = os.environ.get("AGENTRAG_MODEL_SERVICES") or saved.get("MODEL_SERVICES")
    if not services:
        services = json.dumps(DEFAULT_MODELS)
    else:
        try:
            parsed = json.loads(services) if isinstance(services, str) else services
        except json.JSONDecodeError:
            raise RuntimeError("AGENTRAG_MODEL_SERVICES must be JSON") from None
        if not isinstance(parsed, list) or not parsed:
            raise RuntimeError("AGENTRAG_MODEL_SERVICES must be a non-empty list")
        services = json.dumps(parsed)
    docker = os.environ.get("AGENTRAG_DOCKER_SERVICES", saved.get("DOCKER_SERVICES", "docreader"))
    return {
        # Generated once and reused: a rotating token would invalidate the
        # browser's stored value on every launch, which is the failure this
        # launcher is meant to remove.
        "MODEL_SUPERVISOR_TOKEN": saved.get("MODEL_SUPERVISOR_TOKEN") or secrets.token_urlsafe(32),
        "MODEL_SUPERVISOR_ADDR": saved.get("MODEL_SUPERVISOR_ADDR", "127.0.0.1:8092"),
        "MODEL_SUPERVISOR_BIN": saved.get("MODEL_SUPERVISOR_BIN", "data/bin/model-supervisor.exe"),
        "MODEL_SUPERVISOR_STATE": saved.get("MODEL_SUPERVISOR_STATE", "data/model-supervisor-state.json"),
        "MODEL_SERVICES": services,
        "DOCKER_SERVICES": docker,
        "AGENTRAG_DOCREADER_IMAGE": os.environ.get("AGENTRAG_DOCREADER_IMAGE", saved.get("AGENTRAG_DOCREADER_IMAGE", "wechatopenai/weknora-docreader:latest")),
        # Only meaningful while a docreader is actually reachable; see normalize_docreader.
        "DOCREADER_ADDR": normalize_docreader(saved) if "docreader" in docker.split(",") else "",
        "DOCREADER_TRANSPORT": os.environ.get("DOCREADER_TRANSPORT", saved.get("DOCREADER_TRANSPORT", "grpc")),
        "MCP_ENABLED": os.environ.get("AGENTRAG_MCP_ENABLED", saved.get("MCP_ENABLED", "true")),
    }


def prepare(output):
    saved = load()
    def setting(key, default=""):
        return os.environ.get(key, saved.get(key, default))
    env_file = path(setting("ENV_FILE", str(ROOT / ".env")))
    native_value = setting("DB_PATH")
    native = path(native_value if native_value else dotenv_value(env_file, "DB_PATH", "data/weknora.db"))
    db = path(setting("AGENTRAG_EXT_DB", "data/agentrag_ext.db"))
    if db == native or db.name.lower() == "weknora.db":
        print("[DB] Native database path refused; selecting independent data/agentrag_ext.db", file=sys.stderr)
        db = ROOT / "data/agentrag_ext.db"
    if db.resolve() == native:
        raise RuntimeError("native and extension database paths must differ")
    tenant = setting("AGENTRAG_TENANT_ID")
    kb_ids = setting("AGENTRAG_KB_IDS")
    if native.is_file() and (not tenant or not kb_ids):
        with sqlite3.connect(native.as_uri() + "?mode=ro", uri=True) as source:
            if not tenant:
                tenants = source.execute("SELECT id FROM tenants").fetchall()
                if len(tenants) != 1:
                    raise RuntimeError("select AGENTRAG_TENANT_ID for this multi-tenant database")
                tenant = str(tenants[0][0])
            if not kb_ids:
                kb_ids = ",".join(row[0] for row in source.execute("SELECT id FROM knowledge_bases WHERE tenant_id=? AND deleted_at IS NULL ORDER BY id", (tenant,)))
    if not tenant or not kb_ids:
        raise RuntimeError("an existing native tenant and KB are required; configure them in the native UI first")
    port = str(setting("CORE_PORT", "8080"))
    if not port.isdigit() or not 1 <= int(port) <= 65535 or port == "8090":
        raise RuntimeError("invalid CORE_PORT")
    endpoint = normalize_mineru(setting("MINERU_ENDPOINT"))
    values = {
        "AGENTRAG_MODE": "worker", "AGENTRAG_EXT_DB": str(db),
        "AGENTRAG_TENANT_ID": str(tenant), "AGENTRAG_KB_IDS": kb_ids,
        "AGENTRAG_WEKNORA_URL": "http://127.0.0.1:" + port,
        "AGENTRAG_WEKNORA_API_KEY": setting("AGENTRAG_WEKNORA_API_KEY") or "bootstrap-" + secrets.token_urlsafe(32),
        "AGENTRAG_RECONCILE_INTERVAL": setting("AGENTRAG_RECONCILE_INTERVAL", "5s"),
        "MINERU_ENDPOINT": endpoint,
        "AGENTRAG_PARSER_UPSTREAM_URL": setting("AGENTRAG_PARSER_UPSTREAM_URL", endpoint),
        "AGENTRAG_PARSER_UPSTREAM_KEY": setting("AGENTRAG_PARSER_UPSTREAM_KEY"),
        "AGENTRAG_PARSER_PROXY_KEY": setting("AGENTRAG_PARSER_PROXY_KEY") or secrets.token_urlsafe(32),
        "AGENTRAG_PARSER_ARTIFACT_ROOT": str(path(setting("AGENTRAG_PARSER_ARTIFACT_ROOT", "data/parser_artifacts"))),
        "AGENTRAG_PARSER_SCOPES": json.dumps([{"tenant_id": int(tenant), "knowledge_base_id": kb} for kb in kb_ids.split(",")]),
        "ENV_FILE": str(env_file), "DB_PATH": str(native),
        "CORE_PORT": port, "WEKNORA_WEB_DIR": str(ROOT / "web"),
        "AGENTRAG_PARSER_PROXY_ADDR": "http://127.0.0.1:8090",
        "SSRF_DISABLE": setting("SSRF_DISABLE", "true"),
        "AGENTRAG_MANAGEMENT_KEY": setting("AGENTRAG_MANAGEMENT_KEY") or secrets.token_urlsafe(32),
    }
    values.update(service_defaults(saved))
    save(values)
    emit_env(output)


def emit_env(output):
    values = load()
    target = Path(output)
    if target.is_symlink() or (target.exists() and not target.is_file()):
        raise RuntimeError("private environment output must be a regular file, not a link")
    if any(key not in ALLOWED and key not in STATE_ONLY for key in values):
        raise RuntimeError("invalid private configuration key")
    temporary = target.with_name(target.name + "." + secrets.token_hex(6) + ".tmp")
    try:
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            for key, value in values.items():
                if key not in ALLOWED:
                    continue
                stream.write("export " + key + "=" + shlex.quote(str(value)) + "\n")
        os.replace(temporary, target)
    finally:
        if temporary.exists(): temporary.unlink()


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def authenticate(result):
    values = load()
    base = values["AGENTRAG_WEKNORA_URL"] + "/api/v1"
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    def call(method, route, headers, body=None):
        headers = dict(headers, **{"Content-Type": "application/json", "X-Tenant-ID": values["AGENTRAG_TENANT_ID"]})
        request = urllib.request.Request(base + route, data=json.dumps(body).encode() if body is not None else None, headers=headers, method=method)
        with opener.open(request, timeout=8) as response:
            return json.load(response)
    def verified(key):
        for kb in values["AGENTRAG_KB_IDS"].split(","):
            call("GET", "/knowledge-bases/" + kb, {"X-API-Key": key})
    try:
        verified(values["AGENTRAG_WEKNORA_API_KEY"])
        Path(result).write_text("unchanged")
        return
    except urllib.error.HTTPError as error:
        if error.code not in (401, 403):
            raise RuntimeError("native API credential/scope validation failed") from None
    email = os.environ.get("AGENTRAG_LOGIN_EMAIL", "")
    password = os.environ.get("AGENTRAG_LOGIN_PASSWORD", "")
    if not email or not password:
        try:
            with open("/dev/tty", "r+") as terminal:
                terminal.write("First launch: native login email: "); terminal.flush()
                email = terminal.readline().strip()
            password = getpass.getpass("Native password (not saved): ")
        except OSError:
            raise RuntimeError("no valid machine API Key; run interactively for first login, or supply AGENTRAG_LOGIN_EMAIL/AGENTRAG_LOGIN_PASSWORD") from None
    login = call("POST", "/auth/login", {}, {"email": email, "password": password})
    token = login.get("token", "")
    if not token:
        raise RuntimeError("native login failed")
    created = call("POST", "/tenants/" + values["AGENTRAG_TENANT_ID"] + "/api-keys", {"Authorization": "Bearer " + token}, {
        "name": "AgentRAG local Extension Host", "full_access": False,
        "knowledge_base_ids": values["AGENTRAG_KB_IDS"].split(","), "capabilities": ["retrieve", "ingest", "manage_kbs"],
    })
    key = created.get("data", {}).get("token", "")
    if not key:
        raise RuntimeError("native API did not issue a machine credential")
    verified(key)
    values["AGENTRAG_WEKNORA_API_KEY"] = key
    save(values)
    Path(result).write_text("updated")


def show_token():
    """Print the supervisor token on demand; it never appears anywhere else."""
    token = load().get("MODEL_SUPERVISOR_TOKEN", "")
    if not token:
        raise RuntimeError("no supervisor token yet; run prepare first")
    print(token)


def plan():
    """Machine-readable launch plan. The Windows half reads this; no secrets."""
    values = load()
    if not values:
        raise RuntimeError("run prepare first")
    try:
        models = json.loads(values.get("MODEL_SERVICES") or "[]")
    except json.JSONDecodeError:
        raise RuntimeError("MODEL_SERVICES is not valid JSON") from None
    services = [item.strip() for item in (values.get("DOCKER_SERVICES") or "").split(",") if item.strip()]
    print(json.dumps({
        "root": str(ROOT),
        "core_port": int(values.get("CORE_PORT", "8080")),
        "core_url": values.get("AGENTRAG_WEKNORA_URL", ""),
        "web_dir": values.get("WEKNORA_WEB_DIR", ""),
        "tenant": values.get("AGENTRAG_TENANT_ID", ""),
        "mineru_endpoint": values.get("MINERU_ENDPOINT", ""),
        "supervisor": {
            "addr": values.get("MODEL_SUPERVISOR_ADDR", "127.0.0.1:8092"),
            "bin": values.get("MODEL_SUPERVISOR_BIN", "data/bin/model-supervisor.exe"),
            "state": values.get("MODEL_SUPERVISOR_STATE", "data/model-supervisor-state.json"),
            "token_present": bool(values.get("MODEL_SUPERVISOR_TOKEN")),
        },
        "models": models,
        "docker_services": services,
        "docreader": {
            "addr": values.get("DOCREADER_ADDR", ""),
            "transport": values.get("DOCREADER_TRANSPORT", "grpc"),
            "image": values.get("AGENTRAG_DOCREADER_IMAGE", "wechatopenai/weknora-docreader:latest"),
        },
        "mcp": {
            "enabled": values.get("MCP_ENABLED", "true") == "true",
            "url": "http://127.0.0.1:8090/mcp",
            "admin_key_present": bool(values.get("MCP_ADMIN_KEY")),
        },
    }, indent=2, sort_keys=True))


def issue_mcp_key():
    """Issue (once) the dedicated full-access key used for MCP registration.

    MCP service management is guarded by apiKeyManageMCPServices(apiKeyFullAccess())
    and a full-access key short-circuits every capability check, so this key needs
    no capability list — it is still a credential, so it lives only in the 0600
    state file and is never printed.
    """
    values = load()
    if not values:
        raise RuntimeError("run prepare first")
    base = values["AGENTRAG_WEKNORA_URL"] + "/api/v1"
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def call(method, route, headers, body=None):
        headers = dict(headers, **{"Content-Type": "application/json", "X-Tenant-ID": values["AGENTRAG_TENANT_ID"]})
        request = urllib.request.Request(base + route, data=json.dumps(body).encode() if body is not None else None, headers=headers, method=method)
        with opener.open(request, timeout=8) as response:
            return json.load(response)

    email = os.environ.get("AGENTRAG_LOGIN_EMAIL", "")
    password = os.environ.get("AGENTRAG_LOGIN_PASSWORD", "")
    if not email or not password:
        try:
            with open("/dev/tty", "r+") as terminal:
                terminal.write("Native login email: "); terminal.flush()
                email = terminal.readline().strip()
            password = getpass.getpass("Native password (not saved): ")
        except OSError:
            raise RuntimeError("mcp-key needs a native login: run it interactively or set AGENTRAG_LOGIN_EMAIL/AGENTRAG_LOGIN_PASSWORD") from None
    login = call("POST", "/auth/login", {}, {"email": email, "password": password})
    token = login.get("token", "")
    if not token:
        raise RuntimeError("native login failed")
    created = call("POST", "/tenants/" + values["AGENTRAG_TENANT_ID"] + "/api-keys", {"Authorization": "Bearer " + token}, {
        "name": "AgentRAG launcher (MCP registration)", "full_access": True,
    })
    key = created.get("data", {}).get("token", "")
    if not key:
        raise RuntimeError("native API did not issue the MCP registration key")
    values["MCP_ADMIN_KEY"] = key
    save(values)
    print("[MCP] full-access registration key issued and stored in the 0600 launcher state")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["prepare", "env", "auth", "plan", "token", "mcp-key"])
    parser.add_argument("output", nargs="?")
    args = parser.parse_args()
    actions = {"prepare": prepare, "env": emit_env, "auth": authenticate,
               "plan": lambda _unused: plan(), "token": lambda _unused: show_token(),
               "mcp-key": lambda _unused: issue_mcp_key()}
    try:
        actions[args.action](args.output)
    except (OSError, ValueError, sqlite3.Error, RuntimeError) as error:
        # Raw HTTP bodies, SQLite paths and credentials never enter launcher logs.
        message = str(error) if isinstance(error, RuntimeError) else "configuration, filesystem or local API operation failed"
        print("[ERROR] " + message, file=sys.stderr)
        sys.exit(1)
