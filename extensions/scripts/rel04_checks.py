"""REL-04 checks for the synthetic, isolated attachment runner."""

import hashlib
import json
import sqlite3
import urllib.error
import urllib.request
import uuid
from contextlib import closing
from pathlib import Path

import startup_e2e as startup


def seed_v16(path, migrations):
    if path.exists() or not path.parent.name.startswith("attachment-e2e-"):
        raise RuntimeError("REL-04 requires a fresh isolated extension database")
    with closing(sqlite3.connect(path)) as db:
        db.execute("PRAGMA foreign_keys=ON")
        db.execute("CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY)")
        for file in sorted(migrations.glob("*.sql")):
            version = int(file.name.split("_", 1)[0])
            if version <= 16:
                db.executescript(file.read_text(encoding="utf-8"))
                db.execute("INSERT INTO ext_schema_migrations VALUES(?)", (version,))
        stamp = "2026-11-08T00:00:00Z"
        for node, doc in (("v16-a", "a"), ("v16-b", "b")):
            db.execute("INSERT INTO ext_lineage_node VALUES(?,'9001','legacy-fixture','document_version',?,?)", (node, doc, stamp))
        db.execute("INSERT INTO ext_lineage_edge VALUES('v16-edge','9001','legacy-fixture','v16-a','v16-b','amends','governance_critical',?)", (stamp,))
        db.execute("INSERT INTO ext_document_relations VALUES('9001','legacy-fixture','v16-rel','a','b','v16-a','v16-b','amends','active',1,4,'v16-edge')")
        db.execute("""INSERT INTO ext_attachment_links(import_id,tenant_id,knowledge_base_id,parent_document_id,
            parent_file_hash,attachment_sha256,attachment_file_hash,attachment_id,submitted,created_at)
            VALUES('v16-receipt','9001','legacy-fixture','a',?,?,?,'v16-child',1,?)""",
                   ("a" * 32, "b" * 64, "c" * 32, stamp))
        db.commit()
        with closing(sqlite3.connect(path.with_name("ext-v16-backup.db"))) as backup:
            db.backup(backup)


def check_upgrade(path, emit):
    migrations = Path(__file__).resolve().parents[1] / "store" / "migrations"
    expected_versions = sorted(int(file.name.split("_", 1)[0]) for file in migrations.glob("*.sql"))
    with closing(sqlite3.connect(path.as_uri() + "?mode=ro", uri=True)) as db:
        versions = [version for version, in db.execute("SELECT version FROM ext_schema_migrations ORDER BY version")]
        integrity = db.execute("PRAGMA integrity_check").fetchall()
        violations = db.execute("PRAGMA foreign_key_check").fetchall()
        row = db.execute("SELECT edge_type,revision,edge_id FROM ext_document_relations WHERE relation_id='v16-rel'").fetchone()
        owner = db.execute("SELECT attachment_id,evidence_kind,retired_at FROM ext_attachment_links WHERE import_id='v16-receipt'").fetchone()
    with closing(sqlite3.connect(path.with_name("ext-v16-backup.db").as_uri() + "?mode=ro", uri=True)) as db:
        backup_version, = db.execute("SELECT MAX(version) FROM ext_schema_migrations").fetchone()
    if versions != expected_versions or (backup_version, integrity, violations, row, owner) != (
        16, [("ok",)], [], ("amends", 4, "v16-edge"), ("v16-child", "upload_receipt", None),
    ):
        raise RuntimeError("schema upgrade, preservation or rollback snapshot verification failed")
    emit(f"[REL04-MIGRATION] PASS: candidate migrated schema 16 to {versions[-1]}; preserved relation and attachment rows; integrity/FK clean; v16 backup preserved")


def run(http, kb, work, original, replacement, ask, emit):
    if not work.name.startswith("attachment-e2e-"):
        raise RuntimeError("REL-04 fixture cannot run outside its private data directory")
    kb_id = kb["id"]
    base = "/knowledge-bases/" + kb_id + "/lifecycle/business/"
    native_path, ext_path = work / "core.db", work / "ext.db"

    def upload(raw):
        boundary = uuid.uuid4().hex
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="rel04-policy.docx"\r\n'
                'Content-Type: application/vnd.openxmlformats-officedocument.wordprocessingml.document\r\n\r\n').encode()
        body += raw + f'\r\n--{boundary}--\r\n'.encode()
        req = urllib.request.Request(http.base_url + "/api/v1/knowledge-bases/" + kb_id + "/knowledge/file",
                                    data=body, headers={"Authorization": "Bearer " + http.token,
                                                        "Content-Type": "multipart/form-data; boundary=" + boundary}, method="POST")
        with http.opener.open(req, timeout=30) as response:
            return json.load(response)["data"]["id"]

    def manual(title, body):
        return http.api("POST", "/knowledge-bases/" + kb_id + "/knowledge/manual",
                        {"title": title, "content": body, "status": "publish"})["data"]["id"]

    def ready(ids):
        with closing(sqlite3.connect(ext_path.as_uri() + "?mode=ro", uri=True)) as db:
            return all(db.execute("SELECT state FROM ext_lifecycle_current WHERE document_id=?", (doc,)).fetchone() == ("active",)
                       and http.api("GET", "/knowledge/" + doc)["data"]["parse_status"] == "completed" for doc in ids)

    def attachments(owner):
        with closing(sqlite3.connect(ext_path.as_uri() + "?mode=ro", uri=True)) as db:
            return {row[0]: row[1:] for row in db.execute(
                """SELECT attachment_id,attachment_sha256,retired_at,removed_at FROM ext_attachment_links
                   WHERE tenant_id=? AND knowledge_base_id=? AND parent_document_id=? AND attachment_id IS NOT NULL""",
                (str(kb["tenant_id"]), kb_id, owner),
            )}

    def action(name, payload=None, record=None):
        business = {"payload": payload or {}}
        if record:
            business["id"] = record["id"]
        return http.api("PUT", base + name, {"operation_id": uuid.uuid4().hex,
                                            "expected_revision": record["revision"] if record else 0,
                                            "business": business})["data"]

    def declare(kind, source, target):
        return action("relation_declare", {"document_id": source, "target_document_id": target, "edge_type": kind})

    def related(doc):
        return {(row["edge_type"], row["direction"], row["document_id"])
                for row in http.api("GET", base + "related?document_id=" + doc)["data"]}

    def graph_edges():
        graph = http.api("GET", base + "lineage_graph?mode=relations")["data"]
        nodes = {node["id"]: node["document_id"] for node in graph["nodes"]}
        return {(edge["type"], nodes[edge["from"]], nodes[edge["to"]]) for edge in graph["edges"]}

    def download(doc):
        req = urllib.request.Request(http.base_url + "/api/v1/knowledge/" + doc + "/download",
                                     headers={"Authorization": "Bearer " + http.token})
        try:
            response = http.opener.open(req, timeout=15)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status

    parent = upload(original)
    policy = manual("REL-04 reference policy", "# Policy\n\nIndependent reference policy for form use.")
    template = manual("REL-04 shared template", "# Template\n\nIndependent template used by two documents.")
    startup.until(lambda: len(attachments(parent)) == 2 and ready(set(attachments(parent)) | {parent, policy, template}), 90)
    initial = attachments(parent)
    citations = declare("cites", parent, policy)
    declare("implements", parent, policy)
    declare("template_for", template, parent)
    declare("template_for", template, policy)
    if related(template) != {("template_for", "outgoing", parent), ("template_for", "outgoing", policy)}:
        raise RuntimeError("shared template associations were not both present")
    expected = {("cites", parent, policy), ("implements", parent, policy),
                ("template_for", template, parent), ("template_for", template, policy)}
    if not expected.issubset(graph_edges()) or ("cites", "incoming", parent) not in related(policy):
        raise RuntimeError("new relation query or graph direction mismatch")
    action("relation_revoke", record=citations)
    if ("cites", "incoming", parent) in related(policy) or ("cites", parent, policy) in graph_edges():
        raise RuntimeError("revoked citation remained visible")
    emit("[REL04-RELATIONS] PASS: JWT declare/query/revoke and real graph direction for cites/implements/template_for; two associations share one template")

    if ask(parent) != set(initial):
        raise RuntimeError("initial parent RAG links do not match its attachment ownership")
    if hashlib.sha256(original).digest() == hashlib.sha256(replacement).digest():
        raise RuntimeError("REL-04 replacement fixture is unchanged")
    # v0.9.0 has no public file replacement upload route in this checkout.
    # Install the revised file and identity only in the private fixture, then
    # invoke the real native reparse API and let Core/Host produce every effect.
    filename = "rel04-revision.docx"
    path = work / "files" / str(kb["tenant_id"]) / parent / filename
    path.write_bytes(replacement)
    with closing(sqlite3.connect(native_path)) as db:
        n = db.execute("""UPDATE knowledges SET file_path=?,file_hash=?,file_size=?
                          WHERE id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL""",
                       (f"local://{kb['tenant_id']}/{parent}/{filename}", hashlib.md5(replacement).hexdigest(),
                        len(replacement), parent, kb["tenant_id"], kb_id)).rowcount
        if n != 1:
            raise RuntimeError("could not stage the isolated replacement file")
        db.commit()
    http.api("POST", "/knowledge/" + parent + "/reparse")

    def revision_ready():
        links = attachments(parent)
        current = {doc for doc, (_, retired, removed) in links.items() if retired is None and removed is None}
        return len(links) == 3 and len(current) == 2 and len(current.intersection(initial)) == 1 and ready(current | {parent})

    startup.until(revision_ready, 90)
    links = attachments(parent)
    current = {doc for doc, (_, retired, removed) in links.items() if retired is None and removed is None}
    retired, = set(initial) - current
    reused, = set(initial) & current
    if links[reused][0] != initial[reused][0] or download(retired) != 403 or any(download(doc) != 200 for doc in current):
        raise RuntimeError("attachment reuse or version-aware download gate failed")
    if ask(parent) != current or ("implements", "outgoing", policy) in related(parent):
        raise RuntimeError("RAG or business query retained old-version associations")
    emit("[REL04-REVISION] PASS: isolated source replacement + real reparse reused one child, added one, retired one; RAG and downloads use only current attachments")

    http.api("DELETE", "/knowledge/" + parent)
    startup.until(lambda: all(row[2] is not None for row in attachments(parent).values()), 45)
    with closing(sqlite3.connect(native_path.as_uri() + "?mode=ro", uri=True)) as db:
        for doc in set(links) | {parent}:
            if not db.execute("SELECT deleted_at FROM knowledges WHERE id=?", (doc,)).fetchone()[0]:
                raise RuntimeError("main file or managed attachment still live")
            if db.execute("SELECT COUNT(*) FROM chunks WHERE knowledge_id=? AND deleted_at IS NULL", (doc,)).fetchone()[0]:
                raise RuntimeError("live chunks remain after cleanup")
            if db.execute("SELECT COUNT(*) FROM lite_embeddings WHERE knowledge_id=?", (doc,)).fetchone()[0]:
                raise RuntimeError("live embeddings remain after cleanup")
    if http.api("GET", "/knowledge/" + template)["data"]["id"] != template or related(template) != {("template_for", "outgoing", policy)}:
        raise RuntimeError("independent template was lost or still related to a deleted document")
    emit("[REL04-CLEANUP] PASS: main, current and retired attachments and their live indexes cleaned; the shared template and its other relation remain")
