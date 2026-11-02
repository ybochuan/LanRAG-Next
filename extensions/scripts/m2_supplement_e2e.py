#!/usr/bin/env python3
"""Owned native uploads, T3 JWT declarations and real model-input governance gates."""
import json
import secrets
import sqlite3
import time
import urllib.request

import governance_e2e as governance
import lifecycle_ui_e2e as lifecycle

model = governance.Model()
# The shared lifecycle fixture expects the simple fixture lifetime interface.
model.start = model.base.start
model.close = model.base.close


def verify(http, key, kb, child, work):
    base = "/knowledge-bases/" + kb["id"] + "/lifecycle"
    child_route = base + "/documents/" + child["id"]
    route = child_route + "/supplements"
    canary = "SUPPLEMENTBODY" + secrets.token_hex(12)
    description = "SUPPLEMENTDESC" + secrets.token_hex(12)
    chunks = http.api("GET", "/chunks/" + child["id"] + "?page=1&page_size=1000")["data"]
    assert chunks
    for chunk in chunks:
        http.api("PUT", "/chunks/" + child["id"] + "/" + chunk["id"], {"content": "AGENTRAG_E2E_MARKER. Two reviewers required. " + canary})
    with sqlite3.connect(work / "core.db") as db:
        db.execute("UPDATE knowledges SET description=? WHERE id=?", (description, child["id"]))

    def upload(name):
        boundary = secrets.token_hex(16)
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{name}.txt"\r\nContent-Type: text/plain\r\n\r\nAGENTRAG_E2E_MARKER. Parent policy {name}.\r\n--{boundary}--\r\n').encode()
        request = urllib.request.Request(http.base_url + "/api/v1/knowledge-bases/" + kb["id"] + "/knowledge/file", data=body, headers={"Authorization": "Bearer " + http.token, "Content-Type": "multipart/form-data; boundary=" + boundary}, method="POST")
        with http.opener.open(request, timeout=30) as response:
            doc = json.load(response)["data"]
        lifecycle.startup.until(lambda: http.api("GET", "/knowledge/" + doc["id"])["data"]["parse_status"] == "completed")
        def observed_active():
            status, body = http.call("GET", http.base_url + "/api/v1" + base + "/documents/" + doc["id"])
            return status == 200 and json.loads(body)["data"]["snapshot"]["current"]["state"] == "active"
        lifecycle.startup.until(observed_active)
        return doc

    parent = upload("supplement-parent")
    # Actual native snapshot must be ready after asynchronous chunk edit indexing.
    snapshot = "/knowledge-bases/" + kb["id"] + "/knowledge/" + child["id"] + "/source-snapshot"
    lifecycle.startup.until(lambda: http.call("GET", http.base_url + "/api/v1" + snapshot)[0] == 200)
    initial = http.api("GET", route)["data"]["supplements"]
    assert initial["revision"] == 0 and not initial["parents"]
    command = {"expected_revision": 0, "operation_id": "declare-" + secrets.token_hex(10), "parents": [parent["id"]]}
    view = http.api("PUT", route, command)["data"]["supplements"]
    assert view["revision"] == 1 and view["parents"] == [parent["id"]]
    assert http.api("PUT", route, command)["data"]["receipt"]["revision"] == 1
    assert http.call("PUT", http.base_url + "/api/v1" + route, dict(command, operation_id="old-cas"))[0] == 409
    print("[SUPPLEMENT] Real uploads -> scoped current versions -> native JWT declaration/replay/CAS PASS", flush=True)

    qa_row = next(row for row in http.api("GET", "/models")["data"] if row["name"] == "lifecycle-KnowledgeQA")
    qa = qa_row["id"]
    rerank = http.api("POST", "/models", {"name": "supplement-rerank", "type": "Rerank", "source": "remote", "parameters": {"base_url": qa_row["parameters"]["base_url"], "api_key": "fixture-not-secret", "provider": "generic"}})["data"]["id"]
    for agent in ("builtin-quick-answer", "builtin-smart-reasoning"):
        data = http.api("GET", "/agents/" + agent)["data"]
        config = data["config"]
        config.update(model_id=qa, multi_turn_enabled=True, rerank_model_id=rerank, rerank_threshold=0.01)
        if agent == "builtin-smart-reasoning":
            config.update(agent_type="custom", allowed_tools=["search_knowledge", "read_document", "list_documents"])
        http.api("PUT", "/agents/" + agent, {"name": data["name"], "description": data.get("description", ""), "config": config})
    model.doc_id, model.kb_id = child["id"], kb["id"]
    sessions = {}

    def ask(phase, agent):
        model.phase, model.tool_step = phase, 0
        kind = "agent" if agent else "rag"
        if kind not in sessions:
            sessions[kind] = http.api("POST", "/sessions", {"title": "Supplement isolated validation"})["data"]["id"]
        offset = len(model.prompts)
        payload = {"query": "Retrieve AGENTRAG_E2E_MARKER: how many reviewers?", "knowledge_base_ids": [kb["id"]], "knowledge_ids": [child["id"]], "agent_enabled": agent, "agent_id": "builtin-smart-reasoning" if agent else "builtin-quick-answer", "summary_model_id": qa, "web_search_enabled": False, "disable_title": True}
        request = urllib.request.Request(http.base_url + "/api/v1/" + ("agent-chat/" if agent else "knowledge-chat/") + sessions[kind], data=json.dumps(payload).encode(), headers={"Authorization": "Bearer " + http.token, "Content-Type": "application/json"}, method="POST")
        response = []
        with http.opener.open(request, timeout=90) as stream:
            for raw in stream:
                line = raw.decode("utf-8", errors="replace"); response.append(line)
                if line.startswith("data:"):
                    try:
                        event = json.loads(line[5:])
                    except ValueError:
                        continue
                    if event.get("response_type") in ("complete", "error") and event.get("done"):
                        break
        captured = "\n".join(prompt for _, prompt in model.prompts[offset:])

        (work / (phase + "-private-prompts.json")).write_text(captured)
        (work / (phase + "-private.sse")).write_text("".join(response))
        return "".join(response), captured

    for phase, agent, marker in [("rag-active", False, canary), ("agent-search-active", True, canary), ("agent-read-active", True, canary), ("agent-list-active", True, description)]:
        response, prompts = ask(phase, agent)
        assert marker in prompts and model.old_answer in response, "active supplement positive control: " + phase
    print("[POSITIVE] Child body/description reached real RAG + Agent search/read/list model requests PASS", flush=True)

    # Change a parent while it remains active. No timer or lifecycle mutation is needed to deny.
    chunk = http.api("GET", "/chunks/" + parent["id"] + "?page=1&page_size=100")["data"][0]
    http.api("PUT", "/chunks/" + parent["id"] + "/" + chunk["id"], {"content": "Parent native revision independently edited."})
    for phase, agent in [("rag-version", False), ("agent-search-version", True), ("agent-read-version", True), ("agent-list-version", True)]:
        _, prompts = ask(phase, agent)
        assert all(marker not in prompts for marker in (canary, description, model.old_answer)), "changed parent leaked: " + phase
        assert not any(canary in body or description in body for label, body in model.rerank_inputs if label == phase), "revoked child sent to reranker"
    print("[VERSION] Active parent native edit -> fresh-root mismatch -> child absent from ALL RAG/Agent model requests PASS", flush=True)

    # Reconciliation may already have invalidated the original child. Never
    # revive it to reuse a fixture: use a fresh uploaded child for the expiry gate.
    current_state = http.api("GET", child_route)["data"]["snapshot"]["current"]["state"]
    if current_state == "stale":
        rejected = dict(command, expected_revision=1, operation_id="no-revival")
        assert http.call("PUT", http.base_url + "/api/v1" + route, rejected)[0] == 400
    child = upload("supplement-expiry-child")
    model.doc_id = child["id"]
    child_route = base + "/documents/" + child["id"]
    route = child_route + "/supplements"
    chunks = http.api("GET", "/chunks/" + child["id"] + "?page=1&page_size=1000")["data"]
    for chunk in chunks:
        http.api("PUT", "/chunks/" + child["id"] + "/" + chunk["id"], {"content": "AGENTRAG_E2E_MARKER. Two reviewers required. " + canary})
    with sqlite3.connect(work / "core.db") as db:
        db.execute("UPDATE knowledges SET description=? WHERE id=?", (description, child["id"]))
    snapshot = "/knowledge-bases/" + kb["id"] + "/knowledge/" + child["id"] + "/source-snapshot"
    lifecycle.startup.until(lambda: http.call("GET", http.base_url + "/api/v1" + snapshot)[0] == 200)
    http.api("PUT", route, {"expected_revision": 0, "operation_id": "expiry-declare", "parents": [parent["id"]]})
    _, prompts = ask("agent-read-active", True)
    assert canary in prompts, "fresh expiry child positive control failed"
    parent_route = base + "/documents/" + parent["id"]
    annotations = http.api("GET", parent_route)["data"]["snapshot"]["annotations"]
    http.api("PUT", parent_route, {"expected_revision": annotations["revision"], "annotations": dict(annotations, effective_until="2000-01-01T00:00:00Z", labels={"parent-user-label": "keep"})})
    for phase, agent in [("rag-expired", False), ("agent-search-expired", True), ("agent-read-expired", True), ("agent-list-expired", True)]:
        _, prompts = ask(phase, agent)
        assert all(marker not in prompts for marker in (canary, description, model.old_answer)), "expired parent leaked: " + phase
    # Exercise the real existing Machine against the owned DB, with no raw state write.
    import os
    import subprocess
    env = dict(os.environ, AGENTRAG_SUPPLEMENT_OWNED_DIR=str(work), AGENTRAG_SUPPLEMENT_TENANT=str(kb["tenant_id"]), AGENTRAG_SUPPLEMENT_KB=kb["id"], AGENTRAG_SUPPLEMENT_CHILD=child["id"], CGO_ENABLED="0", GOWORK="off")
    nonce = secrets.token_hex(24)
    (work / ".supplement-e2e-owned").write_text(nonce)
    env["AGENTRAG_SUPPLEMENT_NONCE"] = nonce
    subprocess.run(["/usr/local/go/bin/go", "test", "-mod=readonly", "./lineage", "-run", "^TestSupplementOwnedSweep$", "-count=1", "-v"], cwd=lifecycle.ROOT / "extensions", env=env, check=True)
    assert http.api("GET", child_route)["data"]["snapshot"]["current"]["state"] == "stale"
    assert http.api("GET", parent_route)["data"]["snapshot"]["annotations"]["labels"]["parent-user-label"] == "keep"
    print("[EXPIRY] Parent expiry -> immediate model exclusion -> actual Machine child stale/event/outbox; labels retained PASS", flush=True)


if __name__ == "__main__":
    lifecycle.main(verify, model_factory=lambda: model)
