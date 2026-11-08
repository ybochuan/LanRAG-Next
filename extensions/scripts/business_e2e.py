"""M4/M5 checks inside the governance runner's isolated native Core/Host fixture."""
import time
import uuid
import json
import sqlite3
import os
import urllib.request

from startup_e2e import until as wait_until


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
    return guidance["id"]


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
