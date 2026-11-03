#!/usr/bin/env python3
"""Isolated native upload -> Host extraction -> native list/chunks.

Real Core/worker Host and SQLite. Deterministic model and HTTP DocReader
fixtures; DocReader extracts actual DOCX paragraph/table text using stdlib XML.
This does not certify a production Office engine or MinerU. No user content
is printed. Set CORE_BINARY; --sample optionally reads a local DOCX unchanged.
Run in a network namespace if the production loopback 8090 is occupied.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import sqlite3
import subprocess
import tempfile
import threading
import urllib.request
import uuid
import xml.etree.ElementTree as ET
import zipfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import startup_e2e as startup

helpers = startup.helpers
ROOT = Path(__file__).resolve().parents[2]
W = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"


def synthetic_docx():
    source = ROOT / "internal/infrastructure/docparser/anydoc/testdata/upstream/docx/text.docx"
    out = io.BytesIO()
    xml = ('<w:document xmlns:w="' + W[1:-1] + '"><w:body>'
           '<w:p><w:r><w:t>AGENTRAG_E2E_MARKER. Main document; see 附件1.</w:t></w:r></w:p>'
           '<w:p><w:r><w:t>附件1</w:t></w:r></w:p>'
           '<w:p><w:r><w:t>AGENTRAG_E2E_MARKER. Independent annex one text.</w:t></w:r></w:p>'
           '<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Annex table cell.</w:t></w:r></w:p></w:tc></w:tr></w:tbl>'
           '<w:p><w:r><w:t>附件2</w:t></w:r></w:p>'
           '<w:p><w:r><w:t>AGENTRAG_E2E_MARKER. Independent annex two text.</w:t></w:r></w:p>'
           '<w:sectPr/></w:body></w:document>').encode()
    with zipfile.ZipFile(source) as original, zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
        for item in original.infolist():
            z.writestr(item, xml if item.filename == "word/document.xml" else original.read(item))
    return out.getvalue(), 2, 1


class OfficeReader:
    """Test-only HTTP DocReader; real extracted bytes, deterministic conversion."""
    def __init__(self):
        self.calls = 0
        self.lock = threading.Lock()
        outer = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))))
                if self.path.endswith("engines"):
                    reply = {"engines": []}
                else:
                    import base64
                    raw = base64.b64decode(body["file_content"])
                    with zipfile.ZipFile(io.BytesIO(raw)) as z:
                        document = ET.fromstring(z.read("word/document.xml"))
                    text = "\n\n".join("".join(p.itertext()) for p in document.iter(W + "p"))
                    with outer.lock:
                        outer.calls += 1
                    reply = {"markdown_content": text, "metadata": {"parser": "e2e-office-fixture"}, "image_refs": []}
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps(reply).encode())
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = "http://127.0.0.1:" + str(self.server.server_port)

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sample", type=Path)
    parser.add_argument("--expected-annexes", type=int, default=6)
    parser.add_argument("--expected-tables", type=int, default=4)
    parser.add_argument("--governance", action="store_true", help="Verify RAG/Agent links and click-time parent lifecycle enforcement")
    args = parser.parse_args()
    raw, count, tables = synthetic_docx() if args.sample is None else (args.sample.read_bytes(), args.expected_annexes, args.expected_tables)
    source_hash = hashlib.sha256(raw).digest()
    ROOT.joinpath("tmp").mkdir(exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="attachment-e2e-", dir=ROOT / "tmp"))
    os.chmod(work, 0o700)
    report = Path(os.environ.get("AGENTRAG_ATTACHMENT_REPORT", ROOT / "tmp/attachment-e2e.log"))
    report.parent.mkdir(parents=True, exist_ok=True)
    output = report.open("w", encoding="utf-8")
    def emit(message):
        print(message, flush=True)
        output.write(message + "\n")
        output.flush()
    def interrupted(_signal, _frame):
        raise InterruptedError("attachment E2E interrupted")
    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    core = host = reader = model = None
    handles = []
    try:
        startup.free_port(8090)
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
        agent_model = None
        if args.governance:
            from governance_e2e import Model
            agent_model = Model()
            model = agent_model.base
        else:
            model = helpers.ModelFixture()
        model_url = model.start()
        reader = OfficeReader()
        key = secrets.token_hex(32)
        env = {k: v for k, v in os.environ.items() if not k.startswith("AGENTRAG_")}
        env.update(ENV_FILE=str(ROOT / ".env"), DB_DRIVER="sqlite", DB_PATH=str(work / "core.db"), RETRIEVE_DRIVER="sqlite", STORAGE_TYPE="local", LOCAL_STORAGE_BASE_DIR=str(work / "files"), STREAM_MANAGER_TYPE="memory", SERVER_HOST="127.0.0.1", SERVER_PORT=str(port), WEKNORA_WEB_DIR=str(ROOT / "web"), SSRF_DISABLE="true", NEO4J_ENABLE="false", ENABLE_GRAPH_RAG="false", WEKNORA_SANDBOX_MODE="disabled", LOG_PATH="", JWT_SECRET=secrets.token_hex(32), SYSTEM_SIGNING_KEY=secrets.token_hex(32), DOCREADER_ADDR=reader.url, DOCREADER_TRANSPORT="http", AGENTRAG_PARSER_PROXY_ADDR="http://127.0.0.1:8090", AGENTRAG_PARSER_PROXY_KEY=key)
        env["AGENTRAG_MANAGEMENT_KEY"] = secrets.token_hex(32)
        binary = Path(os.environ.get("CORE_BINARY", ROOT / "tmp/WeKnora-lite-attachments-check"))
        log = (work / "core-private.log").open("w"); handles.append(log)
        core = subprocess.Popen([str(binary)], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        http = helpers.HTTP("http://127.0.0.1:" + str(port))
        helpers.wait_health(http, core, http.base_url + "/health", 60)
        nonce = uuid.uuid4().hex[:12]
        account = {"username": "attach_" + nonce, "email": "attach-" + nonce + "@example.invalid", "password": helpers.new_password()}
        http.api("POST", "/auth/register", account)
        http.token = http.api("POST", "/auth/login", {k: account[k] for k in ("email", "password")})["token"]
        models = []
        for kind in ("KnowledgeQA", "Embedding"):
            models.append(http.api("POST", "/models", {"name": "attachments-" + kind, "type": kind, "source": "remote", "parameters": {"base_url": model_url, "api_key": "fixture", "provider": "generic", "interface_type": "openai", "embedding_parameters": {"dimension": 8}}})["data"]["id"])
        kb = http.api("POST", "/knowledge-bases", {"name": "Attachment isolated fixture", "type": "document", "embedding_model_id": models[1], "summary_model_id": models[0], "indexing_strategy": {"vector_enabled": True, "keyword_enabled": True, "graph_enabled": False, "wiki_enabled": False}, "auto_tag_config": {"enabled": False}, "profile_config": {"enabled": False}})["data"]
        api_key = http.api("POST", "/tenants/" + str(kb["tenant_id"]) + "/api-keys", {"name": "Attachment Host", "full_access": False, "knowledge_base_ids": [kb["id"]], "capabilities": ["retrieve", "ingest", "manage_kbs"]})["data"]["token"]
        host_binary = work / "extension-host"
        subprocess.run(["/usr/local/go/bin/go", "build", "-mod=readonly", "-o", str(host_binary), "./cmd/extension-host"], cwd=ROOT / "extensions", env=dict(env, CGO_ENABLED="0", GOWORK="off"), check=True)
        host_env = dict(env, AGENTRAG_MODE="worker", AGENTRAG_EXT_DB=str(work / "ext.db"), AGENTRAG_WEKNORA_URL=http.base_url, AGENTRAG_WEKNORA_API_KEY=api_key, AGENTRAG_TENANT_ID=str(kb["tenant_id"]), AGENTRAG_KB_IDS=kb["id"], AGENTRAG_RECONCILE_INTERVAL="500ms", AGENTRAG_PARSER_UPSTREAM_URL=reader.url, AGENTRAG_PARSER_ARTIFACT_ROOT=str(work / "artifacts"), AGENTRAG_PARSER_SCOPES=json.dumps([{"tenant_id": kb["tenant_id"], "knowledge_base_id": kb["id"]}]))
        def start_host():
            log = (work / "host-private.log").open("a"); handles.append(log)
            process = subprocess.Popen([str(host_binary)], cwd=ROOT / "extensions", env=host_env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            helpers.wait_health(http, process, "http://127.0.0.1:8090/health", 30)
            return process
        host = start_host()
        emit("[SCOPE] Real candidate Core + worker Host + private SQLite; local model/Office DocReader fixtures; original sample read-only")
        boundary = "attachment-" + nonce
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="attachment-parent.docx"\r\nContent-Type: application/vnd.openxmlformats-officedocument.wordprocessingml.document\r\n\r\n').encode() + raw + f'\r\n--{boundary}--\r\n'.encode()
        request = urllib.request.Request(http.base_url + "/api/v1/knowledge-bases/" + kb["id"] + "/knowledge/file", data=body, headers={"Authorization": "Bearer " + http.token, "Content-Type": "multipart/form-data; boundary=" + boundary}, method="POST")
        with http.opener.open(request, timeout=30) as response:
            parent = json.load(response)["data"]["id"]
        def documents():
            return http.api("GET", "/knowledge-bases/" + kb["id"] + "/knowledge?page=1&page_size=100")["data"]
        def completed():
            docs = documents()
            if any(d["parse_status"] == "failed" for d in docs):
                raise RuntimeError("native parsing failed; inspect private diagnostics")
            return len(docs) == count + 1 and all(d["parse_status"] == "completed" for d in docs)
        startup.until(completed, 120)
        docs = documents()
        children = [d for d in docs if d["id"] != parent]
        def lifecycle_state(document_id):
            with sqlite3.connect((work / "ext.db").as_uri() + "?mode=ro", uri=True) as db:
                row = db.execute("SELECT state FROM ext_lifecycle_current WHERE document_id=?", (document_id,)).fetchone()
                return row[0] if row else None
        startup.until(lambda: all(lifecycle_state(d["id"]) == "active" for d in docs), 30)
        if len({d["id"] for d in children}) != count:
            raise RuntimeError("independent native IDs missing")
        for child in children:
            metadata = child.get("metadata") or {}
            if metadata.get("is_attachment") != "true" or metadata.get("parent_document_id") != parent or not metadata.get("attachment_sha256"):
                raise RuntimeError("native attachment metadata mismatch")
        emit(f"[NATIVE-LIST] PASS: one parent + {count} independent DOCX annexes visible through native list API; forced parent metadata verified")
        with sqlite3.connect(work / "core.db") as db:
            for document in docs:
                if db.execute("SELECT COUNT(*) FROM chunks WHERE knowledge_id=?", (document["id"],)).fetchone()[0] < 1:
                    raise RuntimeError("native chunks missing")
        table_count = 0
        for child in children:
            request = urllib.request.Request(http.base_url + "/api/v1/knowledge/" + child["id"] + "/download", headers={"Authorization": "Bearer " + http.token})
            with http.opener.open(request, timeout=30) as response:
                independent = response.read()
            with zipfile.ZipFile(io.BytesIO(independent)) as z:
                table_count += len(list(ET.fromstring(z.read("word/document.xml")).iter(W + "tbl")))
        if table_count != tables:
            raise RuntimeError("annex tables not preserved")
        emit(f"[CONTENT] PASS: {tables} tables preserved in independent DOCX packages; parent and every child completed native chunking/indexing")
        headers = {"Authorization": "Bearer " + key, "X-AgentRAG-Bridge-Version": "1", "X-AgentRAG-Tenant-ID": str(kb["tenant_id"]), "X-AgentRAG-KB-ID": kb["id"], "X-AgentRAG-Doc-ID": parent, "Content-Type": "application/octet-stream", "Content-Disposition": 'attachment; filename="attachment-parent.docx"'}
        request = urllib.request.Request("http://127.0.0.1:8090/parser/native/v1/attachments", data=raw, headers=headers, method="POST")
        with http.opener.open(request, timeout=120) as response:
            ack = json.load(response)
        if {a["document_id"] for a in ack["attachments"]} != {d["id"] for d in children} or len(documents()) != count + 1:
            raise RuntimeError("repeat extraction created new documents")
        emit("[REPLAY] PASS: repeat extraction reconciled same independent IDs; no extra documents; child extraction did not recurse")
        if args.governance:
            rerank = http.api("POST", "/models", {"name": "attachment-rerank", "type": "Rerank", "source": "remote", "parameters": {"base_url": model_url, "api_key": "fixture", "provider": "generic"}})["data"]["id"]
            for agent_id in ("builtin-quick-answer", "builtin-smart-reasoning"):
                agent = http.api("GET", "/agents/" + agent_id)["data"]
                config = agent["config"]
                config.update(model_id=models[0], multi_turn_enabled=False)
                if agent_id == "builtin-smart-reasoning":
                    config.update(agent_type="custom", allowed_tools=["search_knowledge"], rerank_model_id=rerank, rerank_threshold=0.01)
                http.api("PUT", "/agents/" + agent_id, {"name": agent["name"], "description": agent.get("description", ""), "config": config})
            def attachment_ids(value):
                found = set()
                if isinstance(value, dict):
                    entries = value.get("related_attachments")
                    if isinstance(entries, str):
                        entries = json.loads(entries)
                    if isinstance(entries, list):
                        found.update(a["knowledge_id"] for a in entries)
                    for item in value.values():
                        found.update(attachment_ids(item))
                elif isinstance(value, list):
                    for item in value:
                        found.update(attachment_ids(item))
                return found
            def ask(document_id, agent=False):
                agent_model.phase = "agent-search" if agent else "rag"
                agent_model.tool_step = 0
                agent_model.doc_id = document_id
                agent_model.kb_id = kb["id"]
                session = http.api("POST", "/sessions", {"title": "Attachment verification"})["data"]["id"]
                payload = {"query": "Retrieve AGENTRAG_E2E_MARKER", "knowledge_base_ids": [kb["id"]], "knowledge_ids": [document_id], "agent_enabled": agent, "agent_id": "builtin-smart-reasoning" if agent else "builtin-quick-answer", "web_search_enabled": False, "summary_model_id": models[0], "disable_title": True}
                endpoint = "/agent-chat/" if agent else "/knowledge-chat/"
                request = urllib.request.Request(http.base_url + "/api/v1" + endpoint + session, data=json.dumps(payload).encode(), headers={"Authorization": "Bearer " + http.token, "Content-Type": "application/json"}, method="POST")
                lines = []
                with http.opener.open(request, timeout=45) as response:
                    for line in response:
                        text = line.decode("utf-8")
                        lines.append(text)
                        if text.startswith("data:"):
                            value = text[5:].strip()
                            if value == "[DONE]": break
                            data = json.loads(value)
                            kind = data.get("response_type", data.get("type", ""))
                            if kind in ("error", "failed") or data.get("error"):
                                raise RuntimeError("attachment chat emitted error")
                            if kind in ("complete", "done") or data.get("is_completed") is True: break
                body = "".join(lines)
                (work / (("agent-" if agent else "rag-") + document_id + ".sse")).write_text(body)
                return attachment_ids([data for _, data in helpers.sse_records(body)])
            child_ids = {d["id"] for d in children}
            if ask(parent) != child_ids or ask(children[0]["id"]) != {children[0]["id"]}:
                raise RuntimeError("RAG reference metadata missing parent/direct-hit downloads")
            agent_links = ask(children[0]["id"], True)
            if children[0]["id"] not in agent_links or not agent_links.issubset(child_ids):
                raise RuntimeError("Agent search metadata missing attachment download")
            emit("[RETRIEVAL] PASS: real RAG parent hit, direct attachment hit and Agent search emitted native download metadata")
            old_url = http.base_url + "/api/v1/knowledge/" + children[0]["id"] + "/download"
            def download_status():
                request = urllib.request.Request(old_url, headers={"Authorization": "Bearer " + http.token})
                try:
                    response = http.opener.open(request, timeout=15)
                except urllib.error.HTTPError as error:
                    response = error
                with response:
                    return response.status
            if download_status() != 200:
                raise RuntimeError("active attachment download denied")
            with sqlite3.connect(work / "ext.db") as db:
                changed = db.execute("UPDATE ext_document_annotations SET effective_until='2000-01-01T00:00:00Z', revision=revision+1 WHERE document_id=?", (parent,)).rowcount
                if changed != 1:
                    raise RuntimeError("parent annotation missing")
            helpers.stop_owned(host); host = start_host()
            startup.until(lambda: lifecycle_state(parent) == "stale", 15)
            if any(lifecycle_state(d["id"]) != "active" for d in children):
                raise RuntimeError("test requires child active while parent stale")
            if ask(children[0]["id"]) or download_status() != 403:
                raise RuntimeError("stale parent leaked attachment link/download")
            emit("[PARENT-STALE] PASS: actual expiry scan made parent stale; child remained active; links absent and old native download URL returned 403")
            helpers.stop_owned(host); host = None
            if download_status() != 403:
                raise RuntimeError("Host outage allowed attachment download")
            emit("[HOST-OFFLINE] PASS: previously issued attachment download URL blocked; HTTP 403")
        if args.sample and hashlib.sha256(args.sample.read_bytes()).digest() != source_hash:
            raise RuntimeError("source sample changed")
        emit("[RESULT] Attachment " + ("stage-two/three" if args.governance else "stage-one") + " E2E PASS; exit=0; no lineage edges or production services changed")
        return 0
    except Exception as error:
        emit("[RESULT] FAIL: " + str(error))
        emit("[PRIVATE-DIAGNOSTICS] " + str(work))
        return 1
    finally:
        helpers.stop_owned(host)
        helpers.stop_owned(core)
        if reader:
            reader.close()
        if model:
            model.close()
        for handle in handles:
            handle.close()
        output.close()


if __name__ == "__main__":
    raise SystemExit(main())
