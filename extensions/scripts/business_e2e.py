"""M4/M5 checks inside the governance runner's isolated native Core/Host fixture."""
import time
import uuid
import json
import sqlite3
import os


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
    emit("[M5-PUBLISH] PASS: activated feedback guidance -> reviewed proposal -> real native Markdown ingest/index -> supersedes; repeated publish retained target")
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
