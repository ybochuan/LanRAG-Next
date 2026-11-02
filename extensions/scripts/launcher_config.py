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
ALLOWED = set("AGENTRAG_MANAGEMENT_KEY AGENTRAG_MODE AGENTRAG_EXT_DB AGENTRAG_TENANT_ID AGENTRAG_KB_IDS AGENTRAG_WEKNORA_URL AGENTRAG_WEKNORA_API_KEY AGENTRAG_RECONCILE_INTERVAL MINERU_ENDPOINT AGENTRAG_PARSER_UPSTREAM_URL AGENTRAG_PARSER_UPSTREAM_KEY AGENTRAG_PARSER_PROXY_KEY AGENTRAG_PARSER_ARTIFACT_ROOT AGENTRAG_PARSER_SCOPES ENV_FILE DB_PATH CORE_PORT WEKNORA_WEB_DIR AGENTRAG_PARSER_PROXY_ADDR SSRF_DISABLE".split())


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
    endpoint = setting("MINERU_ENDPOINT")
    if not endpoint:
        endpoint = "http://127.0.0.1:8000"
        kernel = Path("/proc/sys/kernel/osrelease")
        if kernel.exists() and "microsoft" in kernel.read_text().lower():
            import subprocess
            routes = subprocess.check_output(["ip", "route", "show", "default"], text=True).split()
            if "via" in routes:
                endpoint = "http://" + routes[routes.index("via") + 1] + ":8000"
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
    save(values)
    emit_env(output)


def emit_env(output):
    values = load()
    target = Path(output)
    if target.is_symlink() or (target.exists() and not target.is_file()):
        raise RuntimeError("private environment output must be a regular file, not a link")
    if any(key not in ALLOWED for key in values):
        raise RuntimeError("invalid private configuration key")
    temporary = target.with_name(target.name + "." + secrets.token_hex(6) + ".tmp")
    try:
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            for key, value in values.items():
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


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["prepare", "env", "auth"])
    parser.add_argument("output")
    args = parser.parse_args()
    try:
        {"prepare": prepare, "env": emit_env, "auth": authenticate}[args.action](args.output)
    except (OSError, ValueError, sqlite3.Error, RuntimeError) as error:
        # Raw HTTP bodies, SQLite paths and credentials never enter launcher logs.
        message = str(error) if isinstance(error, RuntimeError) else "configuration, filesystem or local API operation failed"
        print("[ERROR] " + message, file=sys.stderr)
        sys.exit(1)
