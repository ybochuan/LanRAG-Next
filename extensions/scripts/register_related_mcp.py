#!/usr/bin/env python3
"""Register the local read-only MCP through native admin APIs; never print credentials.

Two credential routes, in order:
  1. MCP_ADMIN_KEY in the 0600 launcher state: a dedicated full-access key that
     also carries manage_mcp_services, issued once by `launcher_config.py mcp-key`
     and sent as X-API-Key. This is the non-interactive route the one-key
     launcher uses.
  2. AGENTRAG_CORE_JWT in the environment, the legacy manual route.
"""
import json
import os
from pathlib import Path
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
STATE = Path(os.environ.get("AGENTRAG_LAUNCHER_CONFIG", ROOT / "data/agentrag-launcher.json")).resolve()
NAME = "AgentRAG"
URL = "http://127.0.0.1:8090/mcp"


def credentials():
    state = json.loads(STATE.read_text(encoding="utf-8")) if STATE.exists() else {}
    if state.get("MCP_ADMIN_KEY"):
        return {"X-API-Key": state["MCP_ADMIN_KEY"], "X-Tenant-ID": str(state.get("AGENTRAG_TENANT_ID", ""))}, state
    token = os.environ.get("AGENTRAG_CORE_JWT", "")
    if token:
        return {"Authorization": "Bearer " + token}, state
    raise RuntimeError("no MCP credential: run ./start-all.sh mcp-key once (or set AGENTRAG_CORE_JWT)")


def main():
    headers, state = credentials()
    base = (state.get("AGENTRAG_WEKNORA_URL") or "http://127.0.0.1:8080").rstrip("/")
    key = state.get("AGENTRAG_MANAGEMENT_KEY", "")
    if len(key) < 32:
        raise RuntimeError("management key missing or too short; run the launcher prepare first")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def api(method, path, data=None):
        request = urllib.request.Request(
            base + "/api/v1" + path,
            data=None if data is None else json.dumps(data).encode(),
            headers=dict(headers, **{"Content-Type": "application/json"}),
            method=method,
        )
        with opener.open(request, timeout=60) as response:
            result = json.load(response)
        if result.get("success") is False:
            raise RuntimeError("native MCP registration rejected")
        return result

    payload = {
        "name": NAME,
        "description": "查询有效补充通知及附件",
        "enabled": True,
        "transport_type": "http-streamable",
        "url": URL,
        "headers": {"Authorization": "Bearer " + key},
    }
    services = api("GET", "/mcp-services")["data"]
    existing = [service for service in services if service.get("name") == NAME]
    if existing:
        if len(existing) != 1 or existing[0].get("url") != payload["url"]:
            raise RuntimeError("AgentRAG name already used by another service")
        service_id = existing[0]["id"]
        api("PUT", "/mcp-services/" + service_id, payload)
    else:
        service_id = api("POST", "/mcp-services", payload)["data"]["id"]
    api("POST", "/mcp-services/" + service_id + "/metadata/refresh", {})
    print("[MCP] AgentRAG registered; tool directory refreshed")


if __name__ == "__main__":
    try:
        main()
    except urllib.error.HTTPError as error:
        print("[MCP] registration failed: HTTP " + str(error.code) + " (admin key invalid or lacks manage_mcp_services)")
        raise SystemExit(1)
    except (OSError, ValueError, RuntimeError) as error:
        print("[MCP] " + str(error))
        raise SystemExit(1)
