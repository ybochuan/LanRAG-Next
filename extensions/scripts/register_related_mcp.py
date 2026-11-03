#!/usr/bin/env python3
"""Register the local read-only MCP through native admin APIs; never print credentials."""
import json
import os
import urllib.request

def main():
    base = os.environ.get("AGENTRAG_WEKNORA_URL", "http://127.0.0.1:18880").rstrip("/")
    token = os.environ["AGENTRAG_CORE_JWT"]
    key = os.environ["AGENTRAG_MANAGEMENT_KEY"]
    if len(key) < 32:
        raise ValueError("management key must contain at least 32 bytes")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    def api(method, path, data=None):
        request = urllib.request.Request(base + "/api/v1" + path, data=None if data is None else json.dumps(data).encode(), headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"}, method=method)
        with opener.open(request, timeout=60) as response:
            result = json.load(response)
        if result.get("success") is False:
            raise RuntimeError("native MCP registration rejected")
        return result
    payload = {"name": "AgentRAG", "description": "查询有效补充通知及附件", "enabled": True, "transport_type": "http-streamable", "url": "http://127.0.0.1:8090/mcp", "headers": {"Authorization": "Bearer " + key}}
    services = api("GET", "/mcp-services")["data"]
    existing = [s for s in services if s.get("name") == "AgentRAG"]
    if existing:
        if len(existing) != 1 or existing[0].get("url") != payload["url"]:
            raise RuntimeError("AgentRAG name already used by another service")
        service_id = existing[0]["id"]
        api("PUT", "/mcp-services/" + service_id, payload)
    else:
        service_id = api("POST", "/mcp-services", payload)["data"]["id"]
    api("POST", "/mcp-services/" + service_id + "/metadata/refresh", {})
    print("AgentRAG MCP registered; directory refreshed. Enable it in the agent's MCP selection.")

if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print("Registration failed: " + type(error).__name__ + "; verify local URL, admin JWT and shared key (values redacted).")
        raise SystemExit(1)
