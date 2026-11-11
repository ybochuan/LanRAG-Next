"""M4/M5 checks inside the governance runner's isolated native Core/Host fixture."""
import time
import uuid
import json
import sqlite3
import os
import hashlib
import subprocess
import urllib.request
from urllib.error import HTTPError
from datetime import datetime, timedelta, timezone

from startup_e2e import until as wait_until
from e2e_test import new_password


def read_family_management(http, kb, key, action, item_id=None, view=""):
    payload = {"action": "business_" + action, "tenant_id": kb["tenant_id"], "knowledge_base_id": kb["id"]}
    if item_id:
        payload["business"] = {"id": item_id}
    if view:
        payload["state"] = view
    req = urllib.request.Request("http://127.0.0.1:8090/management/lifecycle", data=json.dumps(payload).encode(), headers={
        "Content-Type": "application/json", "X-AgentRAG-Bridge-Version": "1", "Authorization": "Bearer " + key,
    })
    try:
        with http.opener.open(req, timeout=5) as response:
            return response.status, json.load(response)["data"]
    except HTTPError as error:
        with error:
            return error.code, None


def check_family_jwt(http, kb, other_kb_id, family_id, emit):
    route = "/knowledge-bases/" + kb["id"] + "/lifecycle/business/"
    actions = ("family_list", "family_members", "family_workspace", "family_replacements", "family_suggestions")
    for action in actions:
        http.api("GET", route + action + "?id=" + family_id)
    admin = http.api("GET", route + "family_workspace?id=" + family_id)["data"]
    if not admin["can_write"] or admin["family"]["business_family_id"] != family_id:
        raise RuntimeError("native family workspace did not preserve the administrator grant")

    workspace_url = http.base_url + "/api/v1" + route + "family_workspace?id=" + family_id
    anonymous = type(http)(http.base_url)
    if anonymous.call("GET", workspace_url)[0] != 401:
        raise RuntimeError("native family workspace accepted a missing JWT")
    foreign = workspace_url.replace("/knowledge-bases/" + kb["id"] + "/", "/knowledge-bases/" + other_kb_id + "/", 1)
    if http.call("GET", foreign)[0] not in (403, 404) or http.call("GET", workspace_url.replace(family_id, other_kb_id, 1))[0] != 404:
        raise RuntimeError("native family workspace accepted a foreign scope or unknown family")

    nonce = uuid.uuid4().hex[:12]
    account = {"username": "fam_viewer_" + nonce, "email": "fam-viewer-" + nonce + "@example.invalid", "password": new_password()}
    anonymous.api("POST", "/auth/register", account)
    http.api("POST", "/tenants/" + str(kb["tenant_id"]) + "/members", {"email": account["email"], "role": "viewer"})
    token = anonymous.api("POST", "/auth/login", {key: account[key] for key in ("email", "password")})["token"]

    def viewer(method, url, payload=None):
        request = urllib.request.Request(url, method=method, data=None if payload is None else json.dumps(payload).encode(), headers={
            "Authorization": "Bearer " + token, "X-Tenant-ID": str(kb["tenant_id"]), "Content-Type": "application/json",
        })
        try:
            response = http.opener.open(request, timeout=5)
        except HTTPError as error:
            response = error
        with response:
            return response.status, json.load(response)

    for action in actions:
        status, body = viewer("GET", http.base_url + "/api/v1" + route + action + "?id=" + family_id)
        if status != 200 or body.get("success") is not True:
            raise RuntimeError("native family read rejected the real viewer role: " + action)
        if action == "family_workspace" and body["data"]["can_write"]:
            raise RuntimeError("native family workspace granted write access to a viewer")
    write_url = http.base_url + "/api/v1" + route + "family_suggestion_scan"
    if viewer("PUT", write_url, {"expected_revision": 0, "operation_id": uuid.uuid4().hex, "business": {"id": family_id, "payload": {}}})[0] != 403:
        raise RuntimeError("native family workspace accepted a viewer write")
    if os.environ.get("AGENTRAG_QA_FEEDBACK_E2E") == "1":
        rows = http.api("GET", route + "feedback_list?state=active")["data"]
        reviewed = next(row for row in rows if row.get("topic") == "fixture-turn")
        status, result = viewer("GET", http.base_url + "/api/v1" + route + "feedback_list?id=" + reviewed["id"])
        if status != 200 or not result["data"] or any(key in result["data"][0] for key in (
            "feedback_context", "citation_document_ids", "reviewed_guidance", "text", "topic",
        )):
            raise RuntimeError("read-only member could inspect private feedback content")
    emit("[FAM-04-JWT] PASS: five scoped reads, administrator grant, real viewer role, write403, anonymous401 and cross-KB denial")
    if node := os.environ.get("AGENTRAG_FAMILY_BROWSER_NODE"):
        fixture = {"origin": http.base_url, "tenant": kb["tenant_id"], "kb": kb["id"], "other": other_kb_id,
                   "family": family_id, "admin": http.token, "viewer": token,
                   "hasFeedback": os.environ.get("AGENTRAG_QA_FEEDBACK_E2E") == "1",
                   "chromium": os.environ["AGENTRAG_FAMILY_BROWSER_CHROMIUM"],
                   "playwright": os.environ["AGENTRAG_FAMILY_BROWSER_PLAYWRIGHT"]}
        check = subprocess.run([node, os.environ["AGENTRAG_FAMILY_BROWSER_SCRIPT"], "--native"],
                               input=json.dumps(fixture), text=True, capture_output=True, timeout=90)
        if check.returncode:
            # Playwright emits a source line and locator diagnostics. Keep the
            # fixture credentials out of stdout and any private log.
            detail = check.stderr.replace(token, "[redacted]").replace(http.token, "[redacted]")
            lines = [line.strip() for line in detail.splitlines() if "Error:" in line or "waiting for" in line]
            summary = ". ".join(lines[:2])[:300] if lines else f"exit {check.returncode}"
            raise RuntimeError("native family browser failed: " + summary)
        emit("[FAM-04-BROWSER] PASS: built frontend, isolated Core/Host, native JWT roles and same-KB semantic suggestions")


def run_families(http, kb, emit, management_key):
    base = "/knowledge-bases/" + kb["id"]
    route = base + "/lifecycle/business/"

    def mutation(revision, payload, family_id=None):
        business = {"payload": payload}
        if family_id:
            business["id"] = family_id
        return {"expected_revision": revision, "operation_id": uuid.uuid4().hex, "business": business}

    def read(action, family_id=None, key=management_key):
        return read_family_management(http, kb, key, action, family_id)

    def state(doc):
        status, body = http.call("GET", http.base_url + "/api/v1" + base + "/lifecycle/documents/" + doc)
        return json.loads(body)["data"]["snapshot"]["current"]["state"] if status == 200 else None

    documents = []
    for year in (2024, 2025, 2026):
        title = "Employee handbook " + str(year)
        documents.append(http.api("POST", base + "/knowledge/manual", {
            "title": title, "content": "# " + title + "\n\nSynthetic family acceptance text.", "status": "publish",
        })["data"]["id"])
    wait_until(lambda: all(state(doc) == "active" for doc in documents), 35)

    create = mutation(0, {"family_label": "Employee handbook"})
    family = http.api("PUT", route + "family_create", create)["data"]
    if http.api("PUT", route + "family_create", create)["data"] != family:
        raise RuntimeError("family creation replay changed the receipt")
    for doc in documents:
        add = mutation(family["revision"], {"document_id": doc}, family["id"])
        family = http.api("PUT", route + "family_member_add", add)["data"]

    other_kb = http.api("POST", "/knowledge-bases", {
        "name": "Family scope fixture", "type": "document", "embedding_model_id": kb["embedding_model_id"],
    })["data"]["id"]
    foreign = http.api("POST", "/knowledge-bases/" + other_kb + "/knowledge/manual", {
        "title": "Foreign handbook", "content": "A document from a different knowledge base.", "status": "publish",
    })["data"]["id"]
    foreign_add = mutation(family["revision"], {"document_id": foreign}, family["id"])
    status, _ = http.call("PUT", http.base_url + "/api/v1" + route + "family_member_add", foreign_add)
    if status != 409:
        raise RuntimeError(f"native family add accepted or misreported a foreign document: HTTP {status}")

    status, items = read("family_members", family["id"])
    if status != 200 or {item["document_id"] for item in items} != set(documents) or len(items) != 3:
        raise RuntimeError("the three real documents are missing from the same business family")
    if len({item["observation_chain_id"] for item in items}) != 3 or any(
        item["business_family_id"] != family["id"] or not item["observed_head_version_node_id"] for item in items
    ):
        raise RuntimeError("business membership overwrote an observation chain")
    if read("family_list", family["id"]) != (200, [{
        "business_family_id": family["id"], "label": "Employee handbook", "revision": 4,
    }]) or read("family_list", key="wrong")[0] != 401:
        raise RuntimeError("family list scope or authorization is incorrect")

    remove = mutation(family["revision"], {"document_id": documents[0]}, family["id"])
    family = http.api("PUT", route + "family_member_remove", remove)["data"]
    status, unassigned = read("family_members")
    if status != 200 or not any(item["document_id"] == documents[0] for item in unassigned):
        raise RuntimeError("removed member is missing from the observed unassigned list")
    if any(state(doc) != "active" for doc in documents):
        raise RuntimeError("membership changed native lifecycle")
    emit("[FAM-01] PASS: native writes, scoped Host reads, cross-KB 409, three observation chains in one family, idempotent receipt, removal and unchanged lifecycle")
    def workbench(view):
        status, result = read_family_management(http, kb, management_key, "family_workspace", family["id"], view)
        if status != 200 or result["family"]["business_family_id"] != family["id"] or result["can_write"]:
            raise RuntimeError(f"read-only family workspace {view} returned HTTP {status} or the wrong scope")
        return result

    removed = workbench("removed").get("members", [])
    if [row["document_id"] for row in removed] != documents[:1] or not removed[0]["removed_at"]:
        raise RuntimeError("workbench omitted the removed member record")
    relation = http.api("PUT", route + "relation_declare", mutation(0, {
        "document_id": documents[0], "target_document_id": documents[2], "edge_type": "amends",
    }))["data"]
    scoped_query = "?document_id=" + documents[2]
    record_path = route + "related" + scoped_query
    doc_records = http.api("GET", record_path + "&state=workbench-relations")["data"]
    graph = http.api("GET", route + "lineage_graph?mode=relations&id=" + documents[2])["data"]
    graph_docs = {node["id"]: node["document_id"] for node in graph["nodes"]}
    if {row["id"] for row in doc_records} != {relation["id"]} or not any(
        (graph_docs[edge["from"]], graph_docs[edge["to"]], edge["type"]) == (documents[0], documents[2], "amends") for edge in graph["edges"]
    ) or any("observation_chain_id" in node or "family_id" in node for node in graph["nodes"]):
        raise RuntimeError("document workbench graph or records lost a directed, scoped relation")
    overview = http.api("GET", base + "/lifecycle?title=Employee+handbook")["data"]
    labels = {row["document_id"]: row.get("business_family") for row in overview["documents"]}
    if labels.get(documents[0]) or (labels.get(documents[2]) or {}).get("label") != "Employee handbook":
        raise RuntimeError("document workbench did not distinguish confirmed and unassigned documents")
    links = workbench("relations").get("relations", [])
    if len(links) != 1 or links[0]["id"] != relation["id"] or links[0]["scope"] != "external":
        raise RuntimeError("workbench omitted the incoming external relation")
    http.api("PUT", route + "relation_revoke", mutation(relation["revision"], {}, relation["id"]))
    activity = http.api("GET", record_path + "&state=workbench-activity")["data"]
    if not any(row["action"] == "relation_revoke" and row["aggregate_id"] == relation["id"] for row in activity):
        raise RuntimeError("document workbench audit lost the revocation")
    if http.call("GET", http.base_url + "/api/v1" + route + "related?state=workbench-relations&document_id=" + foreign)[0] != 404:
        raise RuntimeError("document workbench accepted a document outside the active knowledge base")
    emit("[DOCUMENT-WORKBENCH] PASS: current family, scoped graph, document audit and foreign document rejection")
    if workbench("relations").get("relations") or not any(
        row["id"] == relation["id"] and row["status"] == "revoked" for row in workbench("history").get("relations", [])
    ):
        raise RuntimeError("workbench did not separate current and revoked relations")
    if not any(row["action"] == "relation_revoke" for row in workbench("activity").get("activity", [])):
        raise RuntimeError("workbench omitted the relation audit receipt")
    emit("[FAM-04-WORKSPACE] PASS: real native writes, read-only Host workbench, external relation, revocation history and scoped audit")
    supplement_path = base + "/lifecycle/documents/" + documents[0] + "/supplements"
    supplement = http.api("PUT", supplement_path, {
        "expected_revision": 0, "operation_id": uuid.uuid4().hex, "parents": documents[1:],
    })["data"]["supplements"]
    for doc in documents:
        path = route + "related?state=workbench-relations&document_id=" + doc
        rows = [row for row in http.api("GET", path)["data"] if row["edge_type"] == "supplements"]
        expected = {(documents[0], parent) for parent in documents[1:] if doc in (documents[0], parent)}
        if {(row["document_id"], row["target_document_id"]) for row in rows} != expected or len(rows) != len(expected):
            raise RuntimeError("document workbench omitted a directed supplement declaration")
        for row in rows:
            if http.api("GET", path + "&id=" + row["id"])["data"] != [row]:
                raise RuntimeError("document workbench could not reopen a supplement record")
    http.api("PUT", supplement_path, {
        "expected_revision": supplement["revision"], "operation_id": uuid.uuid4().hex, "parents": [],
    })
    if any(row["edge_type"] == "supplements" for row in http.api("GET", record_path + "&state=workbench-relations")["data"]):
        raise RuntimeError("revoked supplement remained in the current document relations")
    if not any(row["action"] == "supplements_revoked" for row in http.api("GET", record_path + "&state=workbench-activity")["data"]):
        raise RuntimeError("parent document omitted the supplement revocation audit")
    emit("[WORKBENCH-SUPPLEMENTS] PASS: both endpoints list and reopen each declaration, and parent audit retains revocation")
    if os.environ.get("AGENTRAG_FAMILY_SUGGESTION_E2E") == "1":
        def scan():
            request = mutation(0, {}, family["id"])
            if http.api("PUT", route + "family_suggestion_scan", request)["data"]["status"] != "queued":
                raise RuntimeError("native suggestion scan did not queue work")

            def completed():
                status, result = read("family_suggestions", family["id"])
                if status != 200 or result["scan_status"] == "failed":
                    raise RuntimeError("Host suggestion scan did not complete")
                return result if result["scan_status"] == "completed" else None

            wait_until(completed, 30)
            return completed()

        first = scan()
        member = next((row for row in first["items"] if row["kind"] == "membership" and row["document_id"] == documents[0]), None)
        replacement = next((row for row in first["items"] if row.get("edge_type") == "supersedes"), None)
        if not member or "year_sequence" not in member["evidence"]["reasons"] or not replacement:
            raise RuntimeError("native source data did not yield explainable family and relationship suggestions")
        if os.environ.get("AGENTRAG_FAMILY_SEMANTIC_E2E") == "1" and (
            replacement["evidence"].get("semantic_similarity") != 1
            or replacement["evidence"].get("semantic_model") != "family-fixture-embedding"
        ):
            raise RuntimeError("replacement suggestion is missing the separate local vector evidence")
        if (replacement["document_id"], replacement["target_document_id"]) != tuple(reversed(documents[1:])):
            raise RuntimeError("automatic replacement suggestion reversed new and old")
        if len(workbench("members").get("members", [])) != 2 or any(state(doc) != "active" for doc in documents):
            raise RuntimeError("unconfirmed suggestions changed governance")
        receipt = http.api("PUT", route + "family_suggestion_ignore", mutation(member["revision"], {}, member["id"]))["data"]
        if receipt["status"] != "ignored" or any(row["id"] == member["id"] for row in scan()["items"]):
            raise RuntimeError("rescan reopened a previously ignored suggestion")
        for doc in {member["document_id"], member["target_document_id"]}:
            path = route + "related?state=workbench-activity&document_id=" + doc
            audits = [row for row in http.api("GET", path)["data"] if row["aggregate_id"] == receipt["id"]]
            if len(audits) != 1 or audits[0]["action"] != "family_suggestion_ignore":
                raise RuntimeError("document workbench omitted or duplicated the suggestion review receipt")
        emit("[FAM-04-SUGGESTIONS] PASS: real worker scan with observed native titles, evidence and direction; ignored candidate suppressed on rescan")
    if os.environ.get("AGENTRAG_FAMILY_JWT_E2E") == "1":
        check_family_jwt(http, kb, other_kb, family["id"], emit)
    if os.environ.get("AGENTRAG_FAMILY_REPLACEMENT_E2E") != "1":
        return

    old, new = documents[1:]
    def permits(allowed, excluded):
        for mode in ("rag", "agent"):
            payload = {"request_id": uuid.uuid4().hex, "session_id": "family-fixture", "mode": mode,
                       "tenant_id": kb["tenant_id"], "knowledge_base_ids": [kb["id"]], "knowledge_ids": [old, new]}
            req = urllib.request.Request("http://127.0.0.1:8090/bridge/before-retrieve",
                data=json.dumps(payload).encode(), headers={"Content-Type": "application/json", "X-AgentRAG-Bridge-Version": "1"})
            with http.opener.open(req, timeout=5) as response:
                permit = json.load(response)
            if permit["knowledge_ids"] != allowed or permit["excluded_document_ids"] != excluded:
                raise RuntimeError(f"{mode} family replacement permit does not match the committed state")

    due = datetime.now(timezone.utc) + timedelta(seconds=6)
    draft = http.api("PUT", route + "family_replace_create", mutation(0, {
        "business_family_id": family["id"], "family_revision": family["revision"],
        "document_id": new, "target_document_id": old, "effective_at": due.isoformat(),
    }))["data"]
    if draft["status"] != "pending_review":
        raise RuntimeError("family replacement skipped review")
    permits([old, new], [])
    approval = mutation(draft["revision"], {"second_confirmation": True}, draft["id"])
    scheduled = http.api("PUT", route + "family_replace_approve", approval)["data"]
    if scheduled["status"] != "scheduled" or http.api("PUT", route + "family_replace_approve", approval)["data"] != scheduled:
        raise RuntimeError("family replacement was not scheduled idempotently")
    permits([old], [new])
    def applied():
        code, plans = read("family_replacements", family["id"])
        if code != 200 or len(plans) != 1 or plans[0]["status"] == "needs_review":
            raise RuntimeError("scheduled replacement could not verify its locked versions")
        return plans[0] if plans[0]["status"] == "applied" else None
    wait_until(applied, 25)
    plan = applied()
    permits([new], [old])
    code, families = read("family_list", family["id"])
    if (code != 200 or families[0]["published_document_id"] != new
            or families[0]["published_version_node_id"] != plan["new_version_node_id"] or state(old) != "superseded"):
        raise RuntimeError("business head and lifecycle did not cut over together")
    graph = http.api("GET", route + "lineage_graph?mode=version-ancestry&id=" + plan["new_version_node_id"])["data"]
    if not any(edge["from"] == plan["new_version_node_id"] and edge["to"] == plan["old_version_node_id"]
               and edge["type"] == "supersedes" for edge in graph["edges"]):
        raise RuntimeError("family cutover did not write the directed critical edge")
    emit("[FAM-02] PASS: review, scheduled hold, real Host cutover, published version, supersedes direction and replay")


def run_family_history(http, kb, emit):
    base = "/knowledge-bases/" + kb["id"]
    route = base + "/lifecycle/business/"

    def put(action, revision, payload, item_id=None):
        body = {"expected_revision": revision, "operation_id": uuid.uuid4().hex, "business": {"payload": payload}}
        if item_id:
            body["business"]["id"] = item_id
        return http.api("PUT", route + action, body)["data"]

    docs = [http.api("POST", base + "/knowledge/manual", {
        "title": f"Historical chain fixture {i}", "content": f"# Historical chain {i}\n\nSynthetic policy.", "status": "publish",
    })["data"]["id"] for i in range(3)]

    def state(doc):
        code, body = http.call("GET", http.base_url + "/api/v1" + base + "/lifecycle/documents/" + doc)
        return json.loads(body)["data"]["snapshot"]["current"]["state"] if code == 200 else None

    wait_until(lambda: all(state(doc) == "active" for doc in docs), 35)
    family = put("family_create", 0, {"family_label": "Historical chain fixture"})
    for doc in docs:
        family = put("family_member_add", family["revision"], {"document_id": doc}, family["id"])
    first_due = datetime.now(timezone.utc) - timedelta(days=2)
    plans = [put("family_replace_create", 0, {
        "business_family_id": family["id"], "family_revision": family["revision"],
        "target_document_id": old, "document_id": new, "effective_at": (first_due + timedelta(days=i)).isoformat(),
    }) for i, (old, new) in enumerate(zip(docs, docs[1:]))]
    put("family_replace_approve", plans[0]["revision"], {"second_confirmation": True}, plans[0]["id"])
    wait_until(lambda: state(docs[0]) == "superseded", 25)
    path = route + "family_replacements?id=" + family["id"] + "&state="
    preview = http.api("GET", path + "history-preview")["data"]
    if preview.get("published_document_id") != docs[1] or preview.get("final_document_id") != docs[2] or not preview.get("fingerprint"):
        raise RuntimeError("JWT history preview omitted the approved starting point or final member")
    confirm = {"expected_revision": preview["family_revision"], "operation_id": uuid.uuid4().hex, "business": {
        "id": family["id"], "payload": {"second_confirmation": True, "history_fingerprint": preview["fingerprint"],
                                        "replacement_ids": [plans[1]["id"]]},
    }}
    result = http.api("PUT", route + "family_replace_history", confirm)["data"]
    if result.get("target_document_id") != docs[2] or http.api("PUT", route + "family_replace_history", confirm)["data"] != result:
        raise RuntimeError("JWT history confirmation did not retain its committed result")
    timeline = http.api("GET", path + "timeline")["data"]
    if [(row["id"], row["status"]) for row in timeline] != [(plan["id"], "applied") for plan in plans]:
        raise RuntimeError("JWT timeline lost the committed chronology")
    if [state(doc) for doc in docs] != ["superseded", "superseded", "active"]:
        raise RuntimeError("historical cutover did not preserve the final active member")
    emit("[WORKBENCH-HISTORY] PASS: real JWT preview, confirmed suffix, chronological timeline and replay")


def run_repeal(http, kb, emit, ask):
    base = "/knowledge-bases/" + kb["id"]
    route = base + "/lifecycle/business/"
    def put(action, payload=None, previous=None):
        body = {"payload": payload or {}}
        if previous:
            body["id"] = previous["id"]
        return http.api("PUT", route + action, {
            "operation_id": uuid.uuid4().hex, "expected_revision": previous["revision"] if previous else 0,
            "business": body,
        })["data"]
    def upload(title, extra=""):
        return http.api("POST", base + "/knowledge/manual", {
            "title": title, "content": "# " + title + "\n\nSynthetic repeal acceptance clause. " + extra, "status": "publish",
        })["data"]["id"]
    def state(doc):
        status, body = http.call("GET", http.base_url + "/api/v1" + base + "/lifecycle/documents/" + doc)
        return json.loads(body)["data"]["snapshot"]["current"]["state"] if status == 200 else None
    def permitted(allowed, denied):
        for mode in ("rag", "agent"):
            payload = {"request_id": uuid.uuid4().hex, "session_id": "repeal-fixture", "mode": mode,
                       "tenant_id": kb["tenant_id"], "knowledge_base_ids": [kb["id"]], "knowledge_ids": allowed + denied}
            request = urllib.request.Request("http://127.0.0.1:8090/bridge/before-retrieve",
                data=json.dumps(payload).encode(), headers={"Content-Type": "application/json", "X-AgentRAG-Bridge-Version": "1"})
            with http.opener.open(request, timeout=5) as response:
                result = json.load(response)
            if result["knowledge_ids"] != allowed or result["excluded_document_ids"] != denied:
                raise RuntimeError("repeal permit differs from the approved state")
    canary = "REPEALONLY" + uuid.uuid4().hex
    notice = upload("Repeal notice fixture")
    old = upload("Prior policy fixture", "AGENTRAG_E2E_MARKER RETENTIONDEADLINE " + canary)
    withdrawn_notice, unaffected = upload("Withdrawn repeal notice"), upload("Unaffected policy fixture")
    wait_until(lambda: all(state(doc) == "active" for doc in (notice, old, withdrawn_notice, unaffected)), 40)
    status, _, captured = ask("rag-repeal-active", False, old)
    if status != 200 or canary not in captured:
        raise RuntimeError("repeal old policy did not reach the active RAG positive control")
    due = (datetime.now(timezone.utc) + timedelta(seconds=10)).isoformat()
    draft = put("repeal_create", {"document_id": notice, "target_document_id": old, "effective_at": due})
    if draft["status"] != "pending_review" or state(old) != "active":
        raise RuntimeError("unapproved repeal affected source eligibility")
    scheduled = put("repeal_approve", previous=draft)
    if scheduled["status"] != "scheduled" or state(old) != "active":
        raise RuntimeError("future repeal archived its target before the deadline")
    permitted([notice, old], [])
    withdrawn = put("repeal_create", {"document_id": withdrawn_notice, "target_document_id": unaffected, "effective_at": due})
    if put("repeal_approve", previous=withdrawn)["status"] != "scheduled":
        raise RuntimeError("second repeal was not scheduled")
    http.api("PUT", "/knowledge/manual/" + withdrawn_notice, {
        "title": "Withdrawn repeal notice", "content": "# Draft copy", "status": "draft",
    })
    def record(record_id):
        return http.api("GET", route + "relation_list?id=" + record_id)["data"][0]
    wait_until(lambda: state(old) == "archived", 35)
    wait_until(lambda: record(withdrawn["id"])["status"] == "needs_review", 20)
    permitted([notice], [old])
    permitted([unaffected], [])
    original = http.api("GET", "/knowledge/" + old)["data"]
    if original["id"] != old or original["parse_status"] != "completed":
        raise RuntimeError("repeal changed or removed the native original")
    for phase, agent in (("rag-repeal-archived", False), ("agent-read-repeal", True)):
        status, _, captured = ask(phase, agent, old)
        if status != 200 or canary in captured:
            raise RuntimeError("repealed content reached a regular model request")
    records = http.api("GET", route + "relation_list?id=" + draft["id"])["data"]
    if len(records) != 1 or records[0]["status"] != "applied" or not records[0]["applied_at"]:
        raise RuntimeError("completed repeal was not visible in the association list")
    graph = http.api("GET", route + "lineage_graph?mode=relations")["data"]
    nodes = {node["id"]: node["document_id"] for node in graph["nodes"]}
    if not any(e["type"] == "repeals" and nodes[e["from"]] == notice and nodes[e["to"]] == old for e in graph["edges"]):
        raise RuntimeError("repeal notice did not retain the old version in the relation graph")
    emit("[REL-06] PASS: real JWT approval, timed archive, RAG/Agent exclusion, retained repeal edge, native draft drift sent to review")
    return old, canary


def run_clause(http, kb, emit):
    base = "/knowledge-bases/" + kb["id"]
    route = base + "/lifecycle/business/"

    def put(action, payload=None, previous=None, expected=200):
        business = {"payload": payload or {}}
        if previous:
            business["id"] = previous["id"]
        status, body = http.call("PUT", http.base_url + "/api/v1" + route + action, {
            "business": business, "operation_id": uuid.uuid4().hex,
            "expected_revision": previous["revision"] if previous else 0,
        })
        if status != expected:
            raise RuntimeError(f"{action}: expected HTTP {expected}, got {status}")
        return json.loads(body)["data"] if status == 200 else None

    def clause_fixture(title, content):
        doc = http.api("POST", base + "/knowledge/manual", {
            "title": title, "content": "# " + title + "\n\n" + content, "status": "publish",
        })["data"]["id"]

        def ready():
            status, body = http.call("GET", http.base_url + "/api/v1" + base + "/lifecycle/documents/" + doc)
            return status == 200 and json.loads(body)["data"]["snapshot"]["current"]["state"] == "active"

        wait_until(ready, 40)
        chunks = http.api("GET", "/chunks/" + doc + "?page=1&page_size=20")["data"]
        row = next(chunk for chunk in chunks if chunk["knowledge_id"] == doc and content in chunk["content"])
        row = http.api("GET", "/chunks/by-id/" + row["id"])["data"]
        return doc, {"chunk_id": row["id"], "content_hash": hashlib.sha256(row["content"].encode()).hexdigest()}, row

    def graph_edges():
        graph = http.api("GET", route + "lineage_graph?mode=relations")["data"]
        nodes = {node["id"]: node["document_id"] for node in graph["nodes"]}
        return {edge["id"]: (nodes[edge["from"]], nodes[edge["to"]], edge["status"])
                for edge in graph["edges"] if edge["type"] in ("exception_to", "conflicts_with")}

    source, anchor, _ = clause_fixture("Clause exception fixture", "Priority cases have two review days.")
    target, target_anchor, row = clause_fixture("Clause general fixture", "Standard cases have five review days.")
    other, other_anchor, _ = clause_fixture("Clause alternate fixture", "Weekend cases have four review days.")
    relation = {"document_id": source, "target_document_id": target, "edge_type": "exception_to",
                "applies_when": "Priority cases", "source_clause": anchor, "target_clause": target_anchor}
    put("clause_create", {**relation, "source_clause": target_anchor}, expected=409)
    put("clause_create", {**relation, "source_clause": {**anchor, "content_hash": "a" * 64}}, expected=409)
    draft = put("clause_create", relation)
    if draft["status"] != "pending_review" or "clause:" + draft["id"] in graph_edges():
        raise RuntimeError("unapproved clause appeared in the graph")
    exception = put("clause_confirm", previous=draft)
    conflict = put("clause_confirm", previous=put("clause_create", {**relation, "edge_type": "conflicts_with"}))
    outstanding = put("clause_confirm", previous=put("clause_create", {
        **relation, "document_id": other, "source_clause": other_anchor, "edge_type": "conflicts_with",
    }))
    edges = graph_edges()
    if edges.get("clause:" + exception["id"]) != (source, target, "active") or edges.get("clause:" + conflict["id"]) != (source, target, "awaiting_resolution"):
        raise RuntimeError("confirmed clause statuses missing from the graph")
    decision = {"resolution": "prefer_source", "text": "Priority cases follow the special rule."}
    resolved = put("clause_resolve", decision, conflict)
    if graph_edges().get("clause:" + resolved["id"]) != (source, target, "resolved"):
        raise RuntimeError("resolved conflict was not reflected in the graph")

    http.api("PUT", "/chunks/" + target + "/" + row["id"], {
        "content": row["content"] + " The original review period was amended.",
        "expected_revision": row["content_revision"],
    })
    wait_until(lambda: http.api("GET", "/chunks/by-id/" + row["id"])["data"]["index_status"] == "ready", 30)
    put("clause_resolve", decision, outstanding, expected=409)
    saved = http.api("GET", route + "relation_list?id=" + resolved["id"])["data"][0]
    if saved["status"] != "resolved" or saved["pins_current"] or saved["decision_note"] != decision["text"] or not saved["resolved_by"] or not saved["resolved_at"]:
        raise RuntimeError("original decision or pin was overwritten after chunk edit")
    if any("clause:" + record["id"] in graph_edges() for record in (exception, resolved, outstanding)):
        raise RuntimeError("drifted clause still appeared in the current relation graph")
    if put("clause_revoke", previous=saved)["status"] != "revoked":
        raise RuntimeError("historical relation could not be revoked")
    emit("[REL-07] PASS: native chunk evidence, approval, graph status, manual adjudication, source edit rejection and retained history")


def run(http, kb, source, emit, extension_db):
    base = "/knowledge-bases/" + kb["id"] + "/lifecycle/business/"
    revision_source = http.api("POST", "/knowledge-bases/" + kb["id"] + "/knowledge/manual", {"title": "Business revision source", "content": "# Original business fixture\n\nInitial effective clause.", "status": "publish"})["data"]
    until = time.monotonic() + 30
    while True:
        snap = None
        if http.api("GET", "/knowledge/" + revision_source["id"])["data"]["parse_status"] == "completed":
            code, body = http.call("GET", http.base_url + "/api/v1/knowledge-bases/" + kb["id"] + "/lifecycle/documents/" + revision_source["id"])
            if code == 200:
                snap = json.loads(body)["data"]
            elif code != 404:
                raise RuntimeError("manual source lifecycle lookup failed")
        if snap and snap["snapshot"]["current"]["state"] == "active":
            break
        if time.monotonic() >= until:
            raise RuntimeError("manual source observer did not become active")
        time.sleep(0.3)

    tag_route = "/knowledge-bases/" + kb["id"] + "/tags"
    user_tag = http.api("POST", tag_route, {"name": "fixture-business-label"})["data"]

    def keep_user_tag(doc):
        tag_ids = {tag["id"] for tag in (doc.get("tags") or [])}
        http.api("PUT", "/knowledge/tags", {"kb_id": kb["id"], "knowledge_ids": [doc["id"]], "tag_ids": sorted(tag_ids | {user_tag["id"]})})

    keep_user_tag(http.api("GET", "/knowledge/" + revision_source["id"])["data"])

    def put(action, payload=None, record=None, operation=None):
        business = {"payload": payload or {}}
        if record:
            business["id"] = record["id"]
        return http.api("PUT", base + action, {
            "expected_revision": record["revision"] if record else 0,
            "operation_id": operation or uuid.uuid4().hex,
            "business": business,
        })["data"]

    feedback = put("feedback_create", {"text": "Use the current effective revision", "topic": "e2e-revision", "risk": "medium"})
    if feedback["status"] != "pending_review":
        raise RuntimeError("feedback was not staged")
    feedback = put("feedback_approve", record=feedback)
    if feedback["status"] != "approved":
        raise RuntimeError("feedback approval failed")
    activation = {"expected_revision": feedback["revision"], "operation_id": uuid.uuid4().hex, "business": {"id": feedback["id"], "payload": {}}}
    code, _ = http.call("PUT", http.base_url + "/api/v1" + base + "feedback_activate", activation)
    if code != 409:
        raise RuntimeError("real cooling period was bypassed")
    # Advance only this isolated fixture's stored deadline; no runtime clock
    # override or production API can bypass the 48h rule.
    if not extension_db.parent.name.startswith("governance-e2e-") or extension_db.name != "ext.db":
        raise RuntimeError("business fixture database scope mismatch")
    with sqlite3.connect(extension_db) as db:
        db.execute("UPDATE ext_feedback_staging SET ready_at='2000-01-01T00:00:00Z' WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?", (str(kb["tenant_id"]), kb["id"], feedback["id"]))
    feedback = put("feedback_activate", record=feedback)
    if feedback["status"] != "active":
        raise RuntimeError("ready feedback did not activate")
    emit("[M5-FEEDBACK] PASS: real staging/approval -> early activation409 -> isolated fixture deadline advance -> real API activation/guidance; no claim of waiting 48h")
    guidance = put("guidance_seed", {"text": "Check the effective date before applying a clause", "topic": "e2e-revision"}, operation="business-seed")
    replay = put("guidance_seed", {"text": "Check the effective date before applying a clause", "topic": "e2e-revision"}, operation="business-seed")
    if replay != guidance:
        raise RuntimeError("business receipt replay changed")
    proposal = put("revision_create", {"document_id": revision_source["id"], "guidance_ids": [feedback["id"]], "text": "# E2E revision\n\nThe controlled validation revision is effective for this fixture."})
    proposal = put("revision_approve", record=proposal)
    proposal = put("revision_publish", record=proposal)
    target_id = proposal.get("target_document_id")
    if proposal["status"] != "publishing" or not target_id or target_id == revision_source["id"]:
        raise RuntimeError("revision did not bind a distinct draft before indexing")
    draft = http.api("GET", "/knowledge/" + target_id)["data"]
    if draft["parse_status"] != "draft" or draft["enable_status"] != "disabled":
        raise RuntimeError("revision draft is natively searchable before binding")
    proposal = put("revision_publish", record=proposal)
    if proposal["status"] != "publishing" or proposal["target_document_id"] != target_id:
        raise RuntimeError("revision index submission changed its bound target")

    def revision_target_ready(document_id):
        if http.api("GET", "/knowledge/" + document_id)["data"]["parse_status"] != "completed":
            return False
        status, body = http.call("GET", http.base_url + "/api/v1" + base.removesuffix("/business/") + "/documents/" + document_id)
        return status == 200 and json.loads(body)["data"]["snapshot"]["current"]["state"] == "active"

    def revision_permits(allowed, excluded):
        for mode in ("rag", "agent"):
            payload = {"request_id": uuid.uuid4().hex, "session_id": "revision-fixture", "mode": mode,
                       "tenant_id": kb["tenant_id"], "knowledge_base_ids": [kb["id"]], "knowledge_ids": allowed + excluded}
            request = urllib.request.Request("http://127.0.0.1:8090/bridge/before-retrieve",
                data=json.dumps(payload).encode(), headers={"Content-Type": "application/json", "X-AgentRAG-Bridge-Version": "1"})
            with http.opener.open(request, timeout=5) as response:
                result = json.load(response)
            if result["knowledge_ids"] != allowed or result["excluded_document_ids"] != excluded:
                raise RuntimeError(f"revision {mode} permit does not match the committed publication state")

    wait_until(lambda: revision_target_ready(target_id), 35)
    revision_permits([revision_source["id"]], [target_id])
    deadline = time.monotonic() + 35
    while True:
        proposal = put("revision_publish", record=proposal)
        if proposal["status"] == "published":
            break
        if time.monotonic() >= deadline:
            raise RuntimeError("native publish did not reach published")
        time.sleep(0.3)
    target = http.api("GET", "/knowledge/" + proposal["target_document_id"])["data"]
    if target["parse_status"] != "completed" or target["type"] != "manual" or target["id"] == source["id"]:
        raise RuntimeError("native sidecar not indexed")
    repeated = put("revision_publish", record=proposal)
    if repeated != proposal:
        raise RuntimeError("published replay changed target")
    old_view = http.api("GET", "/knowledge-bases/" + kb["id"] + "/lifecycle/documents/" + revision_source["id"])["data"]
    if old_view["snapshot"]["current"]["state"] != "superseded":
        raise RuntimeError("published original remained active")
    revision_permits([target_id], [revision_source["id"]])
    emit("[M5-PUBLISH-GATE] PASS: indexed but unreleased target withheld in RAG and Agent; permit switched only after governance commit")
    emit("[M5-PUBLISH] PASS: activated feedback guidance -> reviewed proposal -> real native Markdown ingest/index -> supersedes; repeated publish retained target")

    # Simulate the old Host's committed native POST with an unreceived ACK.
    legacy_content = "# Legacy publication fixture\n\nUncommitted old-format sidecar."
    legacy = put("revision_create", {"document_id": target_id, "guidance_ids": [feedback["id"]], "text": legacy_content})
    legacy = put("revision_approve", record=legacy)
    legacy_target = http.api("POST", "/knowledge-bases/" + kb["id"] + "/knowledge/manual", {
        "title": "AgentRAG revision " + legacy["publication_key"], "content": legacy_content + "\n",
        "status": "publish", "channel": "api",
    })["data"]["id"]
    wait_until(lambda: revision_target_ready(legacy_target), 35)
    revision_permits([target_id], [legacy_target])
    bound_legacy = put("revision_publish", record=legacy)
    if bound_legacy["status"] != "publishing" or bound_legacy["target_document_id"] != legacy_target:
        raise RuntimeError("legacy receipt was not reconciled to the existing native target")
    revision_permits([target_id], [legacy_target])
    emit("[M5-PUBLISH-LEGACY] PASS: indexed old-format sidecar withheld before binding; retry reconciled the same native target")
    http.api("PUT", base.removesuffix("/business/") + "/documents/" + legacy_target + "/supplements", {
        "expected_revision": 0, "operation_id": uuid.uuid4().hex, "parents": [target_id],
    })
    status, _ = http.call("PUT", http.base_url + "/api/v1" + base + "revision_publish", {
        "expected_revision": bound_legacy["revision"], "operation_id": uuid.uuid4().hex,
        "business": {"id": bound_legacy["id"], "payload": {}},
    })
    if status != 409:
        raise RuntimeError(f"publishing a replacement that depends on its retiring source returned HTTP {status}")
    revision_permits([target_id], [legacy_target])
    emit("[M5-PUBLISH-DEPENDENCY] PASS: an ineligible replacement rolls back cutover and the old document remains retrievable")
    keep_user_tag(target)
    with sqlite3.connect(extension_db) as db:
        family, = db.execute("SELECT family_id FROM ext_revision_proposals WHERE tenant_id=? AND knowledge_base_id=? AND proposal_id=?", (str(kb["tenant_id"]), kb["id"], proposal["id"])).fetchone()
    family_name = "gov:family:" + family
    deadline = time.monotonic() + 25
    while True:
        with sqlite3.connect(extension_db) as db:
            projected, = db.execute("SELECT count(*) FROM ext_projection_outbox WHERE tenant_id=? AND knowledge_base_id=? AND projection_type='family_tag' AND aggregate_id IN (?,?) AND status='done'", (str(kb["tenant_id"]), kb["id"], revision_source["id"], target["id"])).fetchone()
        if projected == 2:
            break
        if time.monotonic() >= deadline:
            raise RuntimeError("real family projection did not complete for both versions")
        time.sleep(0.4)
    ids = set()
    for document_id in (revision_source["id"], target["id"]):
        tags = {tag["name"]: tag["id"] for tag in http.api("GET", "/knowledge/" + document_id)["data"]["tags"]}
        if tags.get(user_tag["name"]) != user_tag["id"] or not tags.get(family_name):
            raise RuntimeError("native family projection lost a business tag or did not attach the family")
        ids.add(tags[family_name])
    if len(ids) != 1:
        raise RuntimeError("old/new native documents do not share the same family tag ID")
    emit("[M5-FAMILY-TAG] PASS: real worker projected a shared native family ID to both versions and preserved the business tag")
    edge = put("relation_declare", {"document_id": source["id"], "target_document_id": target["id"], "edge_type": "amends"})
    persisted = http.api("GET", base + "relation_list?id=" + edge["id"])["data"]
    if len(persisted) != 1 or persisted[0]["id"] != edge["id"] or persisted[0]["target_document_id"] != target["id"]:
        raise RuntimeError("management relation list is not persisted/scoped")
    related = http.api("GET", base + "related?document_id=" + source["id"])["data"]
    if not any(v["document_id"] == target["id"] and v["edge_type"] == "amends" for v in related):
        raise RuntimeError("native related query missing active declaration")
    if os.environ.get("AGENTRAG_LINEAGE_E2E") == "1":
        graph = http.api("GET", base + "lineage_graph?mode=version-ancestry")["data"]
        nodes = {node["id"]: node for node in graph["nodes"]}
        if not any(link["type"] == "supersedes" and nodes[link["from"]]["document_id"] == target["id"] and nodes[link["to"]]["document_id"] == revision_source["id"] for link in graph["edges"]):
            raise RuntimeError("lineage graph lost the actual published supersedes direction")
        target_node = next(node["id"] for node in graph["nodes"] if node["document_id"] == target["id"] and node["current"])
        history = http.api("GET", base + "lineage_graph?mode=version-ancestry&id=" + target_node)["data"]
        provenance = http.api("GET", base + "lineage_graph?mode=provenance&id=" + target_node)["data"]
        if not history["edges"] or any(link["type"] != "derived_from" for link in provenance["edges"]):
            raise RuntimeError("lineage graph mixed version history and content provenance")
        graph = http.api("GET", base + "lineage_graph?mode=relations")["data"]
        nodes = {node["id"]: node for node in graph["nodes"]}
        if not any(link["type"] == "amends" and nodes[link["from"]]["document_id"] == source["id"] and nodes[link["to"]]["document_id"] == target["id"] for link in graph["edges"]):
            raise RuntimeError("lineage graph omitted the declared document relation")
        emit("[M6-LINEAGE] PASS: JWT graph API preserved real published supersedes direction; ancestry and provenance remained separate; declared relation visible")
    put("relation_revoke", record=edge)
    if http.api("GET", base + "related?document_id=" + source["id"])["data"]:
        raise RuntimeError("revoked declaration remains visible")
    emit("[M4-RELATED] PASS: native JWT declaration/query/revoke; current native version + active permit + critical edge transaction")
    # The existing runner restarts Host for the real stale sweep below. Expire
    # only this newly created fixture target so it cannot change negative controls.
    target_route = "/knowledge-bases/" + kb["id"] + "/lifecycle/documents/" + target["id"]
    current = http.api("GET", target_route)["data"]["snapshot"]["annotations"]
    http.api("PUT", target_route, {
        "expected_revision": current["revision"], "annotations": {"effective_until": "2000-01-01T00:00:00Z"},
    })
    return guidance["id"], (family, revision_source["id"], target["id"], user_tag["id"])


def run_family_cutover(http, kb, emit, management_key, history, extension_db):
    legacy, old, new, user_tag = history
    base = "/knowledge-bases/" + kb["id"] + "/lifecycle/business/"

    def read(action, item_id=None):
        status, data = read_family_management(http, kb, management_key, action, item_id)
        if status != 200:
            raise RuntimeError(f"{action} failed: HTTP {status}")
        return data

    def write(action, revision, payload, item_id=None):
        body = {"expected_revision": revision, "operation_id": uuid.uuid4().hex, "business": {"payload": payload}}
        if item_id:
            body["business"]["id"] = item_id
        response = http.api("PUT", base + action, body)["data"]
        if http.api("PUT", base + action, body)["data"] != response:
            raise RuntimeError(f"{action} was not idempotent")
        return response

    def check_native_tags(expected):
        for doc in (old, new):
            tags = http.api("GET", "/knowledge/" + doc)["data"]["tags"]
            if user_tag not in {tag["id"] for tag in tags} or {tag["name"] for tag in tags if tag["name"].startswith("gov:family:")} != {expected}:
                raise RuntimeError("cutover changed unrelated labels or left a stale family tag")

    def check_graph(family_id, version):
        graph = http.api("GET", base + "lineage_graph?mode=version-ancestry&id=" + version)["data"]
        nodes = {node["document_id"]: node for node in graph["nodes"]}
        if set(nodes) != {old, new} or any(node.get("business_family_id", "") != family_id
                or node["observation_chain_id"] != node["family_id"] for node in nodes.values()):
            raise RuntimeError("scope cutover misrepresented the two family identities")

    def settled():
        status = read("family_projection")
        if status["failed"]:
            raise RuntimeError("a family projection entered terminal failure")
        return status["pending"] == 0

    plans = read("family_backfill_plans", legacy)
    if len(plans) != 1 or plans[0]["status"] != "ready" or set(plans[0]["document_ids"]) != {old, new}:
        raise RuntimeError("published history did not produce a complete backfill preview")
    plan = plans[0]
    check_graph("", plan["head_version_node_id"])
    family = write("family_create", 0, {"family_label": "Reviewed historical family"})
    applied = write("family_backfill_apply", family["revision"], {
        "legacy_family_id": legacy, "backfill_fingerprint": plan["fingerprint"], "second_confirmation": True,
    }, family["id"])
    if applied["status"] != "applied" or read("family_list", family["id"])[0]["published_version_node_id"] != plan["head_version_node_id"]:
        raise RuntimeError("historical publish head was not preserved")
    if read("family_backfill_plans", legacy)[0]["mapped_business_family_id"] != family["id"]:
        raise RuntimeError("legacy ID mapping was not recorded")
    check_native_tags("gov:family:" + legacy)
    enabled = write("family_projection_set", 0, {"projection_enabled": True, "second_confirmation": True})
    wait_until(settled, 30)
    check_graph(family["id"], plan["head_version_node_id"])
    check_native_tags("gov:family:" + family["id"])
    write("family_projection_set", enabled["revision"], {"projection_enabled": False, "second_confirmation": True})
    wait_until(settled, 30)
    check_graph("", plan["head_version_node_id"])
    check_native_tags("gov:family:" + legacy)
    with sqlite3.connect(extension_db) as db:
        if db.execute("PRAGMA foreign_key_check").fetchone():
            raise RuntimeError("family cutover left invalid foreign keys")
    emit("[FAM-03] PASS: historical receipt, explicit replay-safe backfill, business head, reversible graph/tag cutover and preserved user labels")


def run_split(http, kb, emit, extension_db, guidance_id):
    """Exercise publication and the real retrieval permit inside the private fixture."""
    if extension_db.name != "ext.db" or not extension_db.parent.name.startswith("governance-e2e-"):
        raise RuntimeError("split fixture database scope mismatch")
    tenant, kb_id = kb["tenant_id"], kb["id"]
    base = "/knowledge-bases/" + kb_id
    business = base + "/lifecycle/business/"
    bridge = "http://127.0.0.1:8090/bridge/before-retrieve"

    def command(record=None, payload=None):
        body = {"payload": payload or {}}
        if record:
            body["id"] = record["id"]
        return {"expected_revision": record["revision"] if record else 0, "operation_id": uuid.uuid4().hex, "business": body}

    def put(action, record=None, payload=None):
        return http.api("PUT", business + action, command(record, payload))["data"]

    def lifecycle(document_id):
        status, body = http.call("GET", http.base_url + "/api/v1" + base + "/lifecycle/documents/" + document_id)
        if status == 404:
            return None
        if status != 200:
            raise RuntimeError(f"split lifecycle lookup returned HTTP {status}")
        return json.loads(body)["data"]["snapshot"]["current"]["state"]

    def indexed_and_active(document_id):
        status = http.api("GET", "/knowledge/" + document_id)["data"]["parse_status"]
        if status == "failed":
            raise RuntimeError("split document indexing failed")
        return status == "completed" and lifecycle(document_id) == "active"

    def permits(allowed, excluded):
        for mode in ("rag", "agent"):
            trace = uuid.uuid4().hex
            payload = {"request_id": trace, "session_id": "split-fixture", "mode": mode,
                       "tenant_id": tenant, "knowledge_base_ids": [kb_id],
                       "knowledge_ids": allowed + excluded}
            req = urllib.request.Request(bridge, data=json.dumps(payload).encode(), method="POST",
                                         headers={"Content-Type": "application/json", "X-AgentRAG-Bridge-Version": "1"})
            with http.opener.open(req, timeout=5) as response:
                permit = json.load(response)
            expected = {"request_id": trace, "tenant_id": tenant, "knowledge_base_ids": [kb_id],
                        "knowledge_ids": allowed, "excluded_document_ids": excluded, "deny_all": not allowed}
            if permit != expected:
                raise RuntimeError(f"split {mode} permit does not match the authorized publication state")

    def batch_view(batch_id):
        records = http.api("GET", business + "revision_list?id=" + batch_id)["data"]
        if len(records) != 1 or records[0]["id"] != batch_id or records[0].get("record_kind") != "split_batch":
            raise RuntimeError("split batch is not visible in the native revision list")
        return records[0]

    old = http.api("POST", base + "/knowledge/manual", {
        "title": "Original split fixture",
        "content": "# Original split fixture\n\nThe source contains both governed sections.",
        "status": "publish",
    })["data"]["id"]
    wait_until(lambda: indexed_and_active(old), 35)
    permits([old], [])

    inputs = [
        {"title": "Split fixture A", "text": "# Split fixture A\n\nSection A governs publication."},
        {"title": "Split fixture B", "text": "# Split fixture B\n\nSection B governs review."},
    ]
    incompatible = {"title": "Native rewrite fixture", "text": "~~~html\nonload = fixture\n~~~"}
    status, _ = http.call("PUT", http.base_url + "/api/v1" + business + "split_create", command(payload={
        "document_id": old, "guidance_ids": [guidance_id], "split_parts": [incompatible, inputs[1]],
    }))
    if status != 400:
        raise RuntimeError(f"split creation must reject Markdown the native endpoint would rewrite, got HTTP {status}")
    batch = put("split_create", payload={"document_id": old, "guidance_ids": [guidance_id], "split_parts": inputs})
    view = batch_view(batch["id"])
    pending = view["split_parts"]
    source_version = view["source_version_id"]
    if (batch["status"] != "pending_review" or view["source_document_id"] != old or not source_version
            or len(pending) != len(inputs) or any(
                part["status"] != "pending_review" or part.get("target_document_id")
                or part["title"] != requested["title"] or part["proposed_patch"].strip() != requested["text"]
                for part, requested in zip(pending, inputs))):
        raise RuntimeError("split review did not lock the source version and complete proposed parts")

    for action in ("revision_approve", "revision_publish"):
        status, _ = http.call("PUT", http.base_url + "/api/v1" + business + action, command(pending[0]))
        if status != 409:
            raise RuntimeError(f"{action} did not require approval of the entire split batch")
    batch = put("split_approve", batch)
    view = batch_view(batch["id"])
    parts = view["split_parts"]
    if (batch["status"] != "approved" or view["status"] != "approved"
            or view["source_version_id"] != source_version
            or [part["id"] for part in parts] != [part["id"] for part in pending]
            or any(part["status"] != "approved" or part.get("target_document_id") for part in parts)):
        raise RuntimeError("split approval did not atomically approve the original member list")
    emit("[REL-05-APPROVAL] PASS: real JWT API reviewed both full Markdown parts and rejected approval/publication of a single unapproved member")

    def prepare(part, allowed, excluded):
        draft = put("revision_publish", part)
        target = draft.get("target_document_id")
        if draft["status"] != "publishing" or not target or target == old:
            raise RuntimeError("split publication did not persist a new draft before submitting the index")
        native_draft = http.api("GET", "/knowledge/" + target)["data"]
        if native_draft["parse_status"] != "draft" or native_draft["enable_status"] != "disabled":
            raise RuntimeError("split draft was already searchable before its publication receipt")
        permits(allowed, excluded + [target])

        submitted = put("revision_publish", draft)
        if submitted["status"] != "publishing" or submitted.get("target_document_id") != target:
            raise RuntimeError("split indexing changed the target or skipped the governance confirmation")
        wait_until(lambda: indexed_and_active(target), 35)
        permits(allowed, excluded + [target])
        return submitted, target

    # Recreate the remote side effect of a lost draft ACK, then use the
    # ordinary native editor before the Host learns the document's ID.
    first_input = inputs[0]
    channel = "agentrag-split-" + parts[0]["id"]
    native_body = {"title": first_input["title"], "content": first_input["text"] + "\n", "channel": channel}
    first_id = http.api("POST", base + "/knowledge/manual", {**native_body, "status": "draft"})["data"]["id"]
    http.api("PUT", "/knowledge/manual/" + first_id, {**native_body, "title": "Renamed split fixture", "status": "publish"})
    wait_until(lambda: indexed_and_active(first_id), 35)
    permits([old], [first_id])
    unbound = batch_view(batch["id"])["split_parts"][0]
    if unbound.get("target_document_id"):
        raise RuntimeError("the draft receipt test bound the target before the permit check")
    http.api("PUT", "/knowledge/manual/" + first_id, {**native_body, "status": "publish"})
    wait_until(lambda: indexed_and_active(first_id), 35)
    first = put("revision_publish", parts[0])
    if first["status"] != "publishing" or first.get("target_document_id") != first_id:
        raise RuntimeError("split reconciliation must bind the existing indexed document")
    permits([old], [first_id])
    revision_path = business + "related?state=workbench-revisions&document_id="
    if http.api("GET", revision_path + first_id)["data"]:
        raise RuntimeError("an unpublished split target received the batch history")
    if lifecycle(old) != "active":
        raise RuntimeError("old split source was disabled before a first publication committed")
    emit("[REL-05-PREPARE] PASS: RAG/Agent reject the native-indexed draft before target binding, even after rename; reconcile binds the existing ID")

    first = put("revision_publish", first)
    if first["status"] != "published" or first.get("target_document_id") != first_id or lifecycle(old) != "superseded":
        raise RuntimeError("the first split publication did not replace the old retrieval state")
    permits([first_id], [old])
    if put("revision_publish", first) != first:
        raise RuntimeError("replaying the first publication changed its persisted result")
    emit("[REL-05-FIRST] PASS: first confirmed part became retrievable and the original became superseded; retry kept the same result")

    second, second_id = prepare(parts[1], [first_id], [old])
    if second_id == first_id:
        raise RuntimeError("split parts reused the same native target")
    emit("[REL-05-BOUND] PASS: normal draft registration precedes native indexing; the indexed second part stays excluded until publication")
    second = put("revision_publish", second)
    if (second["status"] != "published" or second.get("target_document_id") != second_id
            or lifecycle(old) != "superseded" or put("revision_publish", second) != second):
        raise RuntimeError("the second split publication could not use the locked superseded source")
    permits([first_id, second_id], [old])
    final = batch_view(batch["id"])
    if (final["source_version_id"] != source_version
            or {part["target_document_id"] for part in final["split_parts"] if part["status"] == "published"}
            != {first_id, second_id}):
        raise RuntimeError("split batch lost a published member or changed its locked source")
    for doc in (old, first_id, second_id):
        path = revision_path + doc
        rows = http.api("GET", path)["data"]
        if [row["id"] for row in rows] != [batch["id"]]:
            raise RuntimeError("document workbench omitted or duplicated the published split batch")
        if http.api("GET", path + "&id=" + batch["id"])["data"] != [final]:
            raise RuntimeError("document workbench could not reopen a scoped split batch")
    emit("[WORKBENCH-SPLIT] PASS: one batch opens from the original and each published child")

    graph = http.api("GET", business + "lineage_graph?mode=version-ancestry")["data"]
    nodes = {node["id"]: node for node in graph["nodes"]}
    ancestors = {(nodes[edge["from"]]["document_id"], edge["to"]) for edge in graph["edges"] if edge["type"] == "split_from"}
    if ancestors != {(first_id, source_version), (second_id, source_version)} or nodes[source_version]["document_id"] != old:
        raise RuntimeError("both split branches must point to the same original version")
    with sqlite3.connect(extension_db.as_uri() + "?mode=ro", uri=True) as db:
        transitions, = db.execute(
            "SELECT count(*) FROM ext_lifecycle_events WHERE tenant_id=? AND knowledge_base_id=? "
            "AND document_id=? AND from_state='active' AND to_state='superseded'",
            (str(tenant), kb_id, old),
        ).fetchone()
        if transitions != 1 or db.execute("PRAGMA foreign_key_check").fetchone():
            raise RuntimeError("split source expired more than once or the migration broke referential integrity")
    emit("[REL-05-ANCESTRY] PASS: second branch published from the locked superseded source, both split_from edges share that version, and exactly one source transition persisted")
