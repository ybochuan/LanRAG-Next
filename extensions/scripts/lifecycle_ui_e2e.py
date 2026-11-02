#!/usr/bin/env python3
"""Real native JWT facade + worker Host + SQLite + uploaded TXT, with local model fixtures.

Run in WSL. Port 8090 must be free, or use a private network namespace.
No production database, account or document is modified. Private logs stay in tmp/.
CORE_BINARY defaults to tmp/m2-ui-WeKnora-lite; no cloud model claim.
"""
import concurrent.futures
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import sqlite3
import subprocess
import tempfile
import urllib.request
import uuid

import startup_e2e as startup

ROOT = Path(__file__).resolve().parents[2]
helpers = startup.helpers


def main(extra_check=None, model_factory=None):
    report = ROOT / "docs/evidence/m2-lifecycle-ui-e2e-2026-11-01.log"
    report.parent.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="m2-lifecycle-ui-", dir=ROOT / "tmp"))
    os.chmod(work, 0o700)
    handles = []
    core = host = model = None
    output = report.open("w", encoding="utf-8")

    def emit(message):
        print(message, flush=True)
        output.write(message + "\n")
        output.flush()

    def interrupted(*_args):
        raise InterruptedError("lifecycle UI E2E interrupted")

    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        startup.free_port(8090)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        model = (model_factory or helpers.ModelFixture)()
        model_url = model.start()
        management_key = secrets.token_urlsafe(32)
        env = {k: v for k, v in os.environ.items() if not k.startswith("AGENTRAG_")}
        env.update(ENV_FILE=str(ROOT / ".env"), DB_DRIVER="sqlite", DB_PATH=str(work / "core.db"), RETRIEVE_DRIVER="sqlite",
                   STORAGE_TYPE="local", LOCAL_STORAGE_BASE_DIR=str(work / "files"), STREAM_MANAGER_TYPE="memory",
                   SERVER_HOST="127.0.0.1", SERVER_PORT=str(port), WEKNORA_WEB_DIR=str(ROOT / "frontend/dist"),
                   SSRF_DISABLE="true", NEO4J_ENABLE="false", ENABLE_GRAPH_RAG="false", WEKNORA_SANDBOX_MODE="disabled",
                   LOG_PATH="", JWT_SECRET=secrets.token_hex(32), SYSTEM_SIGNING_KEY=secrets.token_hex(32),
                   AGENTRAG_MANAGEMENT_KEY=management_key)
        binary = Path(os.environ.get("CORE_BINARY", ROOT / "tmp/m2-ui-WeKnora-lite"))
        log = (work / "core-private.log").open("w"); handles.append(log)
        core = subprocess.Popen([str(binary)], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        http = helpers.HTTP("http://127.0.0.1:" + str(port))
        helpers.wait_health(http, core, http.base_url + "/health", 60)
        nonce = uuid.uuid4().hex[:12]
        account = {"username": "life_" + nonce, "email": "life-" + nonce + "@example.invalid", "password": helpers.new_password()}
        http.api("POST", "/auth/register", account)
        http.token = http.api("POST", "/auth/login", {k: account[k] for k in ("email", "password")})["token"]
        emit("[AUTH] Native register/login PASS; private credentials omitted")
        models = []
        for kind in ("KnowledgeQA", "Embedding"):
            models.append(http.api("POST", "/models", {"name": "lifecycle-" + kind, "type": kind, "source": "remote", "parameters": {"base_url": model_url, "api_key": "fixture-not-secret", "provider": "generic", "interface_type": "openai", "embedding_parameters": {"dimension": 8}}})["data"]["id"])
        payload = {"name": "Lifecycle isolated test", "type": "document", "embedding_model_id": models[1], "summary_model_id": models[0], "indexing_strategy": {"vector_enabled": True, "keyword_enabled": True, "graph_enabled": False, "wiki_enabled": False}, "auto_tag_config": {"enabled": False}, "profile_config": {"enabled": False}}
        kb = http.api("POST", "/knowledge-bases", payload)["data"]
        other = http.api("POST", "/knowledge-bases", dict(payload, name="Other isolated KB"))["data"]
        key = http.api("POST", "/tenants/" + str(kb["tenant_id"]) + "/api-keys", {"name": "Lifecycle fixture Host", "full_access": False, "knowledge_base_ids": [kb["id"]], "capabilities": ["retrieve", "ingest", "manage_kbs"]})["data"]["token"]
        base = "/knowledge-bases/" + kb["id"] + "/lifecycle"
        assert http.call("GET", http.base_url + "/api/v1" + base)[0] == 503
        emit("[OFFLINE] Management facade returns 503 with Host stopped PASS")
        host_binary = work / "extension-host"
        subprocess.run(["/usr/local/go/bin/go", "build", "-buildvcs=false", "-mod=readonly", "-o", str(host_binary), "./cmd/extension-host"], cwd=ROOT / "extensions", env=dict(env, CGO_ENABLED="0", GOWORK="off"), check=True)
        host_env = dict(env, AGENTRAG_MODE="worker", AGENTRAG_EXT_DB=str(work / "ext.db"), AGENTRAG_WEKNORA_URL=http.base_url,
                        AGENTRAG_WEKNORA_API_KEY=key, AGENTRAG_TENANT_ID=str(kb["tenant_id"]), AGENTRAG_KB_IDS=kb["id"], AGENTRAG_RECONCILE_INTERVAL="250ms")
        log = (work / "host-private.log").open("w"); handles.append(log)
        host = subprocess.Popen([str(host_binary)], cwd=ROOT / "extensions", env=host_env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        helpers.wait_health(http, host, "http://127.0.0.1:8090/health", 30)
        boundary = "life-" + nonce
        text = "AGENTRAG_E2E_MARKER. Lifecycle test requires two reviewers. " * 6
        multipart = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="lifecycle-test.txt"\r\nContent-Type: text/plain\r\n\r\n{text}\r\n--{boundary}--\r\n').encode()
        req = urllib.request.Request(http.base_url + "/api/v1/knowledge-bases/" + kb["id"] + "/knowledge/file", data=multipart, headers={"Authorization": "Bearer " + http.token, "Content-Type": "multipart/form-data; boundary=" + boundary}, method="POST")
        with http.opener.open(req, timeout=30) as response:
            doc = json.load(response)["data"]
        startup.until(lambda: http.api("GET", "/knowledge/" + doc["id"])["data"]["parse_status"] == "completed")
        detail_route = base + "/documents/" + doc["id"]
        def active():
            status, body = http.call("GET", http.base_url + "/api/v1" + detail_route)
            return status == 200 and json.loads(body)["data"]["snapshot"]["current"]["state"] == "active"
        startup.until(active, 20)
        view = http.api("GET", base)["data"]
        assert view["can_write"] and view["counts"]["active"] == 1 and view["documents"][0]["title"]
        emit("[UPLOAD] Real upload -> native chunks -> Observer active -> titled UI overview PASS")
        if extra_check is not None:
            extra_check(http, key, kb, doc, work)
        current = http.api("GET", detail_route)["data"]["snapshot"]["annotations"]
        body = {"expected_revision": current["revision"], "annotations": dict(current, effective_until="2000-01-01T00:00:00Z", labels={"classification": "fixture", "user-label": "preserve"})}
        changed = http.api("PUT", detail_route, body)["data"]
        assert changed["snapshot"]["current"]["state"] == "stale"
        assert changed["snapshot"]["annotations"]["labels"]["user-label"] == "preserve"
        assert len(changed["events"]) >= 2
        assert http.call("PUT", http.base_url + "/api/v1" + detail_route, body)[0] == 409
        emit("[DATES] Past expiry -> stale, labels retained, event timeline, stale CAS rejected PASS")
        policy_payload = {"request_id": "lifecycle-e2e", "session_id": "fixture", "mode": "rag", "tenant_id": kb["tenant_id"], "knowledge_base_ids": [kb["id"]], "knowledge_ids": [doc["id"]], "original_query": "reviewers", "rewritten_query": "reviewers"}
        req = urllib.request.Request("http://127.0.0.1:8090/bridge/before-retrieve", data=json.dumps(policy_payload).encode(), headers={"Content-Type": "application/json", "X-AgentRAG-Bridge-Version": "1"}, method="POST")
        with http.opener.open(req, timeout=3) as response:
            policy = json.load(response)
        assert policy["deny_all"] and doc["id"] in policy["excluded_document_ids"]
        emit("[POLICY] Same document excluded by real DB policy immediately after date edit PASS")
        revision = view["rule_revision"]
        update = {"expected_revision": revision, "rules": {"effective_delay_days": 0, "valid_days": 30, "review_days": 7}}
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
            statuses = list(executor.map(lambda _: http.call("PUT", http.base_url + "/api/v1" + base + "/rules", update)[0], range(8)))
        assert statuses.count(200) == 1 and statuses.count(409) == 7
        emit("[CONCURRENCY] Eight real HTTP writers: one 200 + seven 409, no lost update PASS")
        status = http.call("GET", http.base_url + "/api/v1/knowledge-bases/" + other["id"] + "/lifecycle/documents/" + doc["id"])[0]
        assert status == 404, "foreign document status=" + str(status)
        outsider = helpers.HTTP(http.base_url)
        status=outsider.call("GET", http.base_url + "/api/v1" + base)[0]
        assert status == 401, "missing login status=" + str(status)
        request=urllib.request.Request(http.base_url + "/api/v1" + base,headers={"X-API-Key":key})
        try:
            response=http.opener.open(request,timeout=5)
        except urllib.error.HTTPError as error:
            response=error
        with response:
            status=response.status
        assert status == 403, "machine key status=" + str(status)
        # Isolated fixture membership only; login again so no cached role claim is relied on.
        with sqlite3.connect(work / "core.db") as db:
            db.execute("UPDATE tenant_members SET role='viewer'")
        http.token = http.api("POST", "/auth/login", {k: account[k] for k in ("email", "password")})["token"]
        readonly = http.api("GET", base)["data"]
        assert not readonly["can_write"], "viewer can_write remained true"
        assert http.call("PUT", http.base_url + "/api/v1" + base + "/rules", update)[0] == 403
        emit("[AUTHORIZATION] Missing login 401, machine key 403, foreign document 404, viewer read-only/write403 PASS")
        emit("[RESULT] M2 lifecycle management E2E PASS; real Core/Host/SQLite; deterministic local model fixture")
    except Exception as error:
        emit("[RESULT] FAIL: " + type(error).__name__ + (": " + str(error) if isinstance(error, AssertionError) else "") + "; private logs retained under tmp/m2-lifecycle-ui-*")
        raise SystemExit(1) from None
    finally:
        helpers.stop_owned(host)
        helpers.stop_owned(core)
        if model:
            model.close()
        for handle in handles:
            handle.close()
        output.close()


if __name__ == "__main__":
    main()
