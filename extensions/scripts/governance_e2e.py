#!/usr/bin/env python3
"""Real core/Host governance regression with independent body/description/history canaries.

Run in a private Linux network namespace if the user's Host owns port 8090.
No user password, API key or actual document content is written to this script.
"""
import io
import json
import os
from pathlib import Path
import re
import secrets
import signal
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.parse
import urllib.request
import uuid

import startup_e2e as startup

ROOT = Path(__file__).resolve().parents[2]
helpers = startup.helpers


class Model:
    def __init__(self):
        self.base = helpers.ModelFixture()
        self.phase = "setup"
        self.prompts = []
        self.rerank_inputs = []
        self.family_embedding_inputs = []
        self.tool_outputs = []
        self.tool_step = 0
        self.query = ""
        self.agent_requests = []
        self.doc_id = ""
        self.old_answer = "ANSWERONLY" + secrets.token_hex(12)
        self.no_match = "NOMATCH" + secrets.token_hex(12)
        model = self
        original = self.base.server.RequestHandlerClass.do_POST

        def handle(handler):
            raw = handler.rfile.read(int(handler.headers.get("Content-Length", "0")))
            request = json.loads(raw)
            if handler.path.endswith("/embeddings") and request.get("model") == "family-fixture-embedding":
                inputs = request["input"]
                if handler.headers.get("Authorization") or handler.headers.get("X-API-Key"):
                    handler.send_error(400)
                    return
                model.family_embedding_inputs.extend(inputs)
                handler.send_response(200); handler.send_header("Content-Type", "application/json"); handler.end_headers()
                handler.wfile.write(json.dumps({"model": "family-fixture-embedding", "data": [
                    {"index": i, "embedding": [1, 0] if "Employee handbook" in title else [0, 1]}
                    for i, title in enumerate(inputs)
                ]}).encode())
                return
            messages = request.get("messages", [])
            for message in messages:
                if message.get("role") == "tool":
                    model.tool_outputs.append((model.phase,json.dumps(message,ensure_ascii=False)))
            if handler.path.endswith("/rerank") or handler.path.endswith("/reranking"):
                documents=request.get("documents",[])
                model.rerank_inputs.append((model.phase,json.dumps(documents)))
                handler.send_response(200);handler.send_header("Content-Type","application/json");handler.end_headers()
                handler.wfile.write(json.dumps({"results":[{"index":i,"relevance_score":0.99} for i in range(len(documents))]}).encode())
                return
            if messages:
                model.prompts.append((model.phase, json.dumps(messages, ensure_ascii=False)))
            if handler.path.endswith("/embeddings") and model.phase.startswith(("rag-fallback", "rag-materials-reverse")):
                inputs=request.get("input",[])
                if not isinstance(inputs,list): inputs=[inputs]
                handler.send_response(200);handler.send_header("Content-Type","application/json");handler.end_headers()
                handler.wfile.write(json.dumps({"object":"list","data":[{"object":"embedding","index":i,"embedding":[-0.1]*8} for i in range(len(inputs))],"model":"governance-embedding","usage":{"prompt_tokens":1,"total_tokens":1}}).encode())
                return
            if model.phase.startswith("rag-materials-reverse") and ("rewrite_query" in json.dumps(messages) or "rewritten_query" in json.dumps(messages)):
                model.respond(handler, {"content": json.dumps({"rewrite_query": model.query, "intent": "kb_search", "entities": []})}, "stop", bool(request.get("stream")))
                return
            if model.phase == "rag-materials-followup-active" and ("rewrite_query" in json.dumps(messages) or "rewritten_query" in json.dumps(messages)):
                model.respond(handler, {"content": json.dumps({"rewrite_query": "AGENTRAG_E2E_MARKER how many reviewers are required?", "intent": "follow_up", "entities": []})}, "stop", bool(request.get("stream")))
                return
            if model.phase.startswith("rag-fallback") and ("rewrite_query" in json.dumps(messages) or "rewritten_query" in json.dumps(messages)):
                model.respond(handler, {"content": json.dumps({"rewrite_query": model.no_match, "intent": "kb_search", "entities": []})}, "stop", bool(request.get("stream")))
                return
            if request.get("tools") and model.phase.startswith("agent-search-materials"):
                model.agent_requests.append((model.phase, request.get("tool_choice"), messages))
                if model.phase == "agent-search-materials-multiround-active" and model.tool_step == 0:
                    model.tool_step += 1
                    call = {"id": "call-materials-second-search", "type": "function",
                            "function": {"name": "search_knowledge",
                                         "arguments": json.dumps({"query": "AGENTRAG_E2E_MARKER", "mode": "keyword"})}}
                    if request.get("stream"):
                        call["index"] = 0
                    model.respond(handler, {"tool_calls": [call]}, "tool_calls", bool(request.get("stream")))
                    return
                model.base.generation_calls += 1
                model.respond(handler, {"content": helpers.ANSWER + " " + model.old_answer}, "stop", bool(request.get("stream")))
                return
            if request.get("tools") and model.phase.startswith("agent-"):
                action = model.phase.split("-")[1]
                if action == "archive" and model.tool_step < 2:
                    if model.tool_step == 0:
                        name, args = "discover_mcp_tools", {"mode": "describe", "server_id": model.mcp_id, "tool_name": "search_archived_documents"}
                    else:
                        output = next((m.get("content", "") for m in reversed(messages) if m.get("role") == "tool"), "")
                        if isinstance(output, list):
                            output = "\n".join(part.get("text", "") for part in output)
                        ref = re.search(r'"tool_ref"\s*:\s*"([^"]+)"', output)
                        if not ref:
                            model.respond(handler, {"content": "Archive discovery did not return a tool reference."}, "stop", bool(request.get("stream")))
                            return
                        name, args = "call_mcp_tool", {"tool_ref": ref[1], "arguments": {"query": "AGENTRAG_E2E_MARKER"}}
                    model.tool_step += 1
                    call = {"id": "call-archive-" + str(model.tool_step), "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}
                    if request.get("stream"):
                        call["index"] = 0
                        model.respond(handler, {"tool_calls": [call]}, "tool_calls", True)
                    else:
                        model.respond(handler, {"content": None, "tool_calls": [call]}, "tool_calls", False)
                elif model.tool_step == 0:
                    model.tool_step += 1
                    name, args = {
                        "search": ("search_knowledge", {"query": "AGENTRAG_E2E_MARKER", "mode": "keyword"}),
                        "read": ("read_document", {"id": model.doc_id}),
                        "list": ("list_documents", {"knowledge_base_id": model.kb_id}),
                    }[action]
                    call = {"id": "call-governance", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}
                    if request.get("stream"):
                        call["index"] = 0
                        model.respond(handler, {"tool_calls": [call]}, "tool_calls", True)
                    else:
                        model.respond(handler, {"role": "assistant", "content": None, "tool_calls": [call]}, "tool_calls", False)
                else:
                    answer = helpers.ANSWER + (" " + model.old_answer if model.phase.endswith("active") else "")
                    model.base.generation_calls += 1
                    model.respond(handler, {"content": answer}, "stop", bool(request.get("stream")))
                return
            if model.phase.startswith("rag") and handler.path.endswith("/chat/completions") and "rewrite_query" not in json.dumps(messages) and "rewritten_query" not in json.dumps(messages):
                model.base.generation_calls += 1
                answer = helpers.ANSWER + (" " + model.old_answer if model.phase.endswith("active") else "")
                model.respond(handler, {"content": answer}, "stop", bool(request.get("stream")))
                return
            handler.rfile = io.BytesIO(raw)
            original(handler)
        self.base.server.RequestHandlerClass.do_POST = handle

    def respond(self, handler, message, finish, stream):
        handler.send_response(200)
        handler.send_header("Content-Type", "text/event-stream" if stream else "application/json")
        handler.end_headers()
        if stream:
            data = {"id": "gov", "object": "chat.completion.chunk", "created": 1, "model": "gov", "choices": [{"index": 0, "delta": message, "finish_reason": None}]}
            handler.wfile.write(("data: " + json.dumps(data) + "\n\n").encode())
            data["choices"] = [{"index": 0, "delta": {}, "finish_reason": finish}]
            handler.wfile.write(("data: " + json.dumps(data) + "\n\ndata: [DONE]\n\n").encode())
        else:
            message.setdefault("role", "assistant")
            handler.wfile.write(json.dumps({"id": "gov", "object": "chat.completion", "created": 1, "model": "gov", "choices": [{"index": 0, "message": message, "finish_reason": finish}], "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}}).encode())


def main():
    report = Path(os.environ.get("AGENTRAG_GOVERNANCE_REPORT", ROOT / "docs/evidence/m2-governance-leak-e2e-2026-11-01.log"))
    report.parent.mkdir(parents=True, exist_ok=True)
    scratch = Path(os.environ.get("AGENTRAG_E2E_TMPDIR", ROOT / "tmp"))
    scratch.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="governance-e2e-", dir=scratch))
    os.chmod(work, 0o700)
    log = report.open("w", encoding="utf-8")
    def emit(message):
        print(message, flush=True); log.write(message + "\n"); log.flush()
    core = host = model = None
    handles = []
    def interrupted(_signal, _frame):
        raise InterruptedError("governance E2E interrupted")
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        startup.free_port(8090)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0)); port = listener.getsockname()[1]
        model = Model(); model_url = model.base.start()
        env = {key: value for key, value in os.environ.items() if not key.startswith("AGENTRAG_")}
        env["AGENTRAG_MANAGEMENT_KEY"] = secrets.token_hex(32)
        env["AGENTRAG_CORRECTION_SOURCES_KEY"] = secrets.token_hex(32)
        env["AGENTRAG_BEFORE_GENERATE_KEY"] = secrets.token_hex(32)
        empty_env = work / "isolated.env"
        empty_env.touch(mode=0o600)
        env.update(ENV_FILE=str(empty_env), DB_DRIVER="sqlite", DB_PATH=str(work / "core.db"), RETRIEVE_DRIVER="sqlite", STORAGE_TYPE="local", LOCAL_STORAGE_BASE_DIR=str(work / "files"), STREAM_MANAGER_TYPE="memory", SERVER_HOST=os.environ.get("AGENTRAG_E2E_SERVER_HOST", "127.0.0.1"), SERVER_PORT=str(port), WEKNORA_WEB_DIR=str(ROOT / "web"), SSRF_DISABLE="true", NEO4J_ENABLE="false", ENABLE_GRAPH_RAG="false", WEKNORA_SANDBOX_MODE="disabled", LOG_PATH="", JWT_SECRET=secrets.token_hex(32), SYSTEM_SIGNING_KEY=secrets.token_hex(32))
        if os.environ.get("AGENTRAG_FAMILY_BROWSER_NODE"):
            env["WEKNORA_WEB_DIR"] = str(ROOT / "frontend/dist")
        if os.environ.get("AGENTRAG_E2E_WEB_DIR"):
            env["WEKNORA_WEB_DIR"] = os.environ["AGENTRAG_E2E_WEB_DIR"]
        binary = Path(os.environ.get("CORE_BINARY", ROOT / "tmp/governance-WeKnora-lite"))
        output = (work / "core-private.log").open("w"); handles.append(output)
        core = subprocess.Popen([str(binary)], cwd=ROOT, env=env, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        http = helpers.HTTP("http://127.0.0.1:" + str(port))
        helpers.wait_health(http, core, http.base_url + "/health", 60)
        emit("[SCOPE] Real patched core + real worker Host; isolated SQLite; local model fixture; original user documents untouched")
        nonce = uuid.uuid4().hex[:12]
        account = {"username": "gov_" + nonce, "email": "gov-" + nonce + "@example.invalid", "password": helpers.new_password()}
        http.api("POST", "/auth/register", account)
        http.token = http.api("POST", "/auth/login", {key: account[key] for key in ("email", "password")})["token"]
        models = []
        for kind in ("KnowledgeQA", "Embedding"):
            models.append(http.api("POST", "/models", {"name": "governance-" + kind, "type": kind, "source": "remote", "parameters": {"base_url": model_url, "api_key": "fixture-not-secret", "provider": "generic", "interface_type": "openai", "embedding_parameters": {"dimension": 8}}})["data"]["id"])
        rerank_model=http.api("POST","/models",{"name":"governance-rerank","type":"Rerank","source":"remote","parameters":{"base_url":model_url,"api_key":"fixture-not-secret","provider":"generic"}})["data"]["id"]
        for agent_id in ("builtin-quick-answer","builtin-smart-reasoning"):
            agent_data=http.api("GET","/agents/"+agent_id)["data"]
            config=agent_data["config"];config["model_id"]=models[0];config["multi_turn_enabled"]=True
            config["rerank_model_id"]=rerank_model;config["rerank_threshold"]=0.01
            if agent_id=="builtin-smart-reasoning":config.update(agent_type="custom",allowed_tools=["search_knowledge","read_document","list_documents"])
            http.api("PUT","/agents/"+agent_id,{"name":agent_data["name"],"description":agent_data.get("description",""),"config":config})
        kb = http.api("POST", "/knowledge-bases", {"name": "Governance isolated fixture", "type": "document", "embedding_model_id": models[1], "summary_model_id": models[0], "indexing_strategy": {"vector_enabled": True, "keyword_enabled": True, "graph_enabled": False, "wiki_enabled": False}, "auto_tag_config": {"enabled": False}, "profile_config": {"enabled": False}})["data"]
        if os.environ.get("AGENTRAG_BUSINESS_E2E") == "1":
            for lifecycle_state in ("pending", "active", "stale", "superseded"):
                http.api("POST", "/knowledge-bases/" + kb["id"] + "/tags", {"name": "gov:state:" + lifecycle_state})
        api_key = http.api("POST", "/tenants/" + str(kb["tenant_id"]) + "/api-keys", {"name": "Gov isolated Host", "full_access": False, "knowledge_base_ids": [kb["id"]], "capabilities": ["retrieve", "ingest", "manage_kbs"]})["data"]["token"]
        host_binary = Path(os.environ.get("AGENTRAG_E2E_HOST_BINARY", work / "extension-host")).resolve()
        if not os.environ.get("AGENTRAG_E2E_HOST_BINARY"):
            subprocess.run(["/usr/local/go/bin/go", "build", "-mod=readonly", "-o", str(host_binary), "./cmd/extension-host"], cwd=ROOT / "extensions", env=dict(env, CGO_ENABLED="0", GOWORK="off"), check=True)
        host_env = dict(env, AGENTRAG_MODE="worker", AGENTRAG_EXT_DB=str(work / "ext.db"), AGENTRAG_WEKNORA_URL=http.base_url, AGENTRAG_WEKNORA_API_KEY=api_key, AGENTRAG_TENANT_ID=str(kb["tenant_id"]), AGENTRAG_KB_IDS=kb["id"], AGENTRAG_RECONCILE_INTERVAL="250ms")
        host_env["AGENTRAG_CORRECTION_KB_IDS"] = kb["id"]
        if os.environ.get("AGENTRAG_FAMILY_SEMANTIC_E2E") == "1":
            host_env.update(AGENTRAG_FAMILY_EMBEDDING_ORIGIN=model_url.removesuffix("/v1"),
                            AGENTRAG_FAMILY_EMBEDDING_MODEL="family-fixture-embedding")
        def start_host():
            output = (work / "host-private.log").open("a"); handles.append(output)
            process = subprocess.Popen([str(host_binary)], cwd=ROOT / "extensions", env=host_env, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
            helpers.wait_health(http, process, "http://127.0.0.1:8090/health", 30)
            return process
        host = start_host()
        body_canary = "BODYONLY" + secrets.token_hex(12)
        desc_canary = "DESCONLY" + secrets.token_hex(12)
        def upload_text(filename, text):
            boundary = "gov-" + uuid.uuid4().hex
            payload = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{filename}"\r\nContent-Type: text/plain\r\n\r\n{text}\r\n--{boundary}--\r\n').encode()
            request = urllib.request.Request(http.base_url + "/api/v1/knowledge-bases/" + kb["id"] + "/knowledge/file", data=payload, headers={"Authorization": "Bearer " + http.token, "Content-Type": "multipart/form-data; boundary=" + boundary}, method="POST")
            with http.opener.open(request, timeout=30) as response:
                uploaded = json.load(response)["data"]
            startup.until(lambda: http.api("GET", "/knowledge/" + uploaded["id"])["data"]["parse_status"] == "completed", 30)
            return uploaded

        text = ("AGENTRAG_E2E_MARKER. Two reviewers are required. " + body_canary + " ") * 6
        publication_mode = os.environ.get("AGENTRAG_CORRECTION_PUBLICATION_E2E", "")
        if publication_mode == "manual":
            doc = http.api("POST", "/knowledge-bases/" + kb["id"] + "/knowledge/manual", {
                "title": "Reimbursement policy", "content": text, "status": "publish",
            })["data"]
            startup.until(lambda: http.api("GET", "/knowledge/" + doc["id"])["data"]["parse_status"] == "completed", 30)
        else:
            doc = upload_text("gov.txt", text)
        # Independent test-only metadata input; never changes production data.
        with sqlite3.connect(work / "core.db") as db:
            db.execute("UPDATE knowledges SET description=? WHERE id=?", (desc_canary, doc["id"]))
        def state(document_id=doc["id"]):
            with sqlite3.connect((work / "ext.db").as_uri() + "?mode=ro", uri=True) as db:
                row = db.execute("SELECT state FROM ext_lifecycle_current WHERE document_id=?", (document_id,)).fetchone()
                return row[0] if row else None
        startup.until(lambda: state() == "active", 20)
        if os.environ.get("AGENTRAG_BINARY_E2E") == "1":
            target_boundary = "binary-" + nonce
            target_text = "AGENTRAG_BINARY_TARGET. Formal revision file uploaded through the native file API."
            target_payload = (f'--{target_boundary}\r\nContent-Disposition: form-data; name="file"; filename="target.txt"\r\nContent-Type: text/plain\r\n\r\n{target_text}\r\n--{target_boundary}--\r\n').encode()
            target_request = urllib.request.Request(http.base_url + "/api/v1/knowledge-bases/" + kb["id"] + "/knowledge/file", data=target_payload, headers={"Authorization": "Bearer " + http.token, "Content-Type": "multipart/form-data; boundary=" + target_boundary}, method="POST")
            with http.opener.open(target_request, timeout=30) as response:
                target = json.load(response)["data"]
            startup.until(lambda: http.api("GET", "/knowledge/" + target["id"])["data"]["parse_status"] == "completed", 30)
            def target_state():
                with sqlite3.connect((work / "ext.db").as_uri() + "?mode=ro", uri=True) as db:
                    row = db.execute("SELECT state FROM ext_lifecycle_current WHERE document_id=?", (target["id"],)).fetchone()
                    return row[0] if row else None
            startup.until(lambda: target_state() == "active", 20)
            business_route = "/knowledge-bases/" + kb["id"] + "/lifecycle/business/"
            created = http.api("PUT", business_route + "revision_create_binary", {
                "operation_id": uuid.uuid4().hex, "expected_revision": 0,
                "business": {"payload": {"document_id": doc["id"]}},
            })["data"]
            approved = http.api("PUT", business_route + "revision_approve", {
                "operation_id": uuid.uuid4().hex, "expected_revision": created["revision"],
                "business": {"id": created["id"], "payload": {}},
            })["data"]
            bound = http.api("PUT", business_route + "revision_bind_binary", {
                "operation_id": uuid.uuid4().hex, "expected_revision": approved["revision"],
                "business": {"id": created["id"], "payload": {"target_document_id": target["id"]}},
            })["data"]
            published = http.api("PUT", business_route + "revision_publish", {
                "operation_id": uuid.uuid4().hex, "expected_revision": bound["revision"],
                "business": {"id": created["id"], "payload": {}},
            })["data"]
            if published.get("status") != "published" or published.get("target_document_id") != target["id"]:
                raise RuntimeError("binary revision did not publish atomically")
            emit("[M5-BINARY] PASS: native uploaded file bound to binary revision and published atomically")
        model.doc_id, model.kb_id = doc["id"], kb["id"]
        sessions = {}
        def ask(phase, agent, document_id=None, query=None):
            model.phase = phase; model.tool_step = 0
            kind = "agent" if agent else "rag"
            target = document_id or doc["id"]
            model.doc_id = target
            if document_id:
                kind += "-" + document_id
            if kind not in sessions:
                sessions[kind] = http.api("POST", "/sessions", {"title": "Private governance validation"})["data"]["id"]
            offset = len(model.prompts)
            generation_offset=model.base.generation_calls
            endpoint="/agent-chat/" if agent else "/knowledge-chat/"
            payload={"query": "Retrieve AGENTRAG_E2E_MARKER: how many reviewers are required?", "knowledge_base_ids": [kb["id"]], "knowledge_ids": [target], "agent_enabled": agent, "agent_id": "builtin-smart-reasoning" if agent else "builtin-quick-answer", "web_search_enabled": False, "summary_model_id": models[0], "disable_title": True}
            if query is not None:
                payload["query"] = query
            if phase.startswith("rag-repeal-"):
                payload["query"] = "RETENTIONDEADLINE"
            if phase.startswith("agent-archive-"):
                payload["query"] = "仅查阅已经失效的历史条款 AGENTRAG_E2E_MARKER，提醒我它是否仍有效。"
            model.query = payload["query"]
            request=urllib.request.Request(http.base_url+"/api/v1"+endpoint+sessions[kind],data=json.dumps(payload).encode(),headers={"Authorization":"Bearer "+http.token,"Content-Type":"application/json","X-Request-ID":"gov-e2e-"+phase+"-"+nonce},method="POST")
            fragments=[];status=0
            with http.opener.open(request,timeout=90) as stream:
                status=stream.status
                deadline=time.monotonic()+90
                for raw in stream:
                    line=raw.decode("utf-8",errors="replace");fragments.append(line)
                    if time.monotonic()>deadline: raise RuntimeError("SSE total deadline exceeded")
                    if line.startswith("data:"):
                        try:event=json.loads(line[5:])
                        except ValueError:continue
                        if event.get("response_type") in ("complete","error") and event.get("done"):break
            response="".join(fragments)
            captured = "\n".join(prompt for _, prompt in model.prompts[offset:])
            (work / (phase + "-private.sse")).write_text(response)
            (work / (phase + "-private-prompts.json")).write_text(captured)
            model.last_generations=model.base.generation_calls-generation_offset
            return status, response, captured
        status, response, captured = ask("rag-active", False)
        if status != 200 or body_canary not in captured or model.old_answer not in response:
            raise RuntimeError("active RAG body positive control failed")
        emit("[RAG-ACTIVE] PASS: active document body reached actual generation prompt")
        def provenance_counts():
            with sqlite3.connect(work / "ext.db") as db:
                runs = db.execute("SELECT count(*) FROM ext_provenance_runs").fetchone()[0]
                models = db.execute("SELECT count(*) FROM ext_provenance_steps WHERE kind='model'").fetchone()[0]
                inputs = db.execute("SELECT count(*) FROM ext_provenance_inputs").fetchone()[0]
                return runs, models, inputs
        startup.until(lambda: provenance_counts()[0] >= 1 and provenance_counts()[1] >= 1, 5)
        emit("[PROVENANCE-RAG] PASS: native identity and actual model call persisted by real worker Host")
        if os.environ.get("AGENTRAG_QA_FEEDBACK_E2E") == "1":
            quick_id = "/agents/builtin-quick-answer"
            quick = http.api("GET", quick_id)["data"]
            quick_update = {"name": quick["name"], "description": quick.get("description", ""), "config": quick["config"]}
            exact_config = dict(quick["config"], enable_rewrite=False, enable_query_expansion=False, rerank_model_id="", multi_turn_enabled=False)
            http.api("PUT", quick_id, dict(quick_update, config=exact_config))
            prior_session = sessions.pop("rag")
            status, _, captured = ask("rag-qa-feedback-active", False)
            feedback_session = sessions["rag"]
            sessions["rag"] = prior_session
            http.api("PUT", quick_id, quick_update)
            if status != 200 or body_canary not in captured:
                raise RuntimeError("exact-source feedback turn did not consume the document")
            def completed_answer():
                with sqlite3.connect((work / "core.db").as_uri() + "?mode=ro", uri=True) as db:
                    return db.execute("SELECT id, knowledge_references FROM messages WHERE session_id=? AND role='assistant' AND is_completed=1", (feedback_session,)).fetchone()
            startup.until(completed_answer, 10)
            answer_id, native_refs = completed_answer()
            native_ids = {row["id"] for row in json.loads(native_refs)}
            if not native_ids:
                raise RuntimeError("exact-source feedback turn did not persist native retrieval references")
            route = "/knowledge-bases/" + kb["id"] + "/lifecycle/business/feedback_capture"
            context = {"rating": "incorrect", "question": "What is the effective clause?", "answer_excerpt": "A shared answer excerpt.", "suggested_revision": "Cite the effective text."}
            command = {"expected_revision": 0, "operation_id": "qa-feedback-e2e", "business": {"payload": {"session_id": feedback_session, "message_id": answer_id, "topic": "fixture-turn", "text": "Review the effective clause", "negative": True, "feedback_context": context}}}
            receipt = http.api("PUT", route, command)["data"]
            replay = http.api("PUT", route, command)["data"]
            status, _ = http.call("PUT", http.base_url + "/api/v1" + route, dict(command, operation_id="qa-feedback-repeat"))
            if receipt != replay or receipt["status"] != "pending_review" or status != 409:
                raise RuntimeError("QA feedback receipt or duplicate handling failed")
            with sqlite3.connect((work / "ext.db").as_uri() + "?mode=ro", uri=True) as db:
                chunks, run_state = db.execute("SELECT b.retrieved_chunk_ids,r.state FROM ext_feedback_qa_bindings b JOIN ext_provenance_runs r USING(tenant_id,knowledge_base_id,run_id) WHERE b.feedback_id=?", (receipt["id"],)).fetchone()
                promoted, = db.execute("SELECT count(*) FROM ext_feedback_guidances WHERE feedback_id=?", (receipt["id"],)).fetchone()
            if set(json.loads(chunks)) != native_ids or run_state != "sealed" or promoted:
                raise RuntimeError("QA feedback was not bound to the exact retrieved chunk set and held for review")
            emit("[M5-QA-FEEDBACK] PASS: authorized Quick QA answer bound exact native chunk IDs; retries produced one staged feedback")
            review_route = route.removesuffix("feedback_capture")
            review_row = http.api("GET", review_route + "feedback_list?id=" + receipt["id"])["data"][0]
            if review_row["feedback_context"] != context or doc["id"] not in review_row["citation_document_ids"]:
                raise RuntimeError("review did not return the explicitly shared excerpt and verified citation")
            reviewed_text = "REVIEWEDGUIDE" + secrets.token_hex(12)
            def review_action(action, revision, payload):
                return http.api("PUT", review_route + action, {
                    "operation_id": uuid.uuid4().hex, "expected_revision": revision,
                    "business": {"id": receipt["id"], "payload": payload},
                })["data"]
            approved = review_action("feedback_approve", receipt["revision"], {"text": reviewed_text})
            if approved["status"] != "approved":
                raise RuntimeError("review did not preserve the cooling period")
            with sqlite3.connect(work / "ext.db") as db:
                if db.execute("SELECT 1 FROM ext_feedback_guidances WHERE feedback_id=?", (receipt["id"],)).fetchone():
                    raise RuntimeError("approval bypassed the activation gate")
                db.execute("UPDATE ext_feedback_staging SET ready_at='2020-01-01T00:00:00Z' WHERE feedback_id=?", (receipt["id"],))
            if review_action("feedback_activate", approved["revision"], {})["status"] != "active":
                raise RuntimeError("reviewed guidance did not activate")
            http.api("PUT", quick_id, dict(quick_update, config=exact_config))
            _, _, matching = ask("rag-workbench-guide-active", False, doc["id"])
            model.phase = "setup"
            unrelated = http.api("POST", "/knowledge-bases/" + kb["id"] + "/knowledge/manual", {
                "title": "Unrelated workbench source",
                "content": "AGENTRAG_E2E_MARKER appears in a different source. " * 8,
                "status": "publish",
            })["data"]["id"]
            def unrelated_active():
                url = http.base_url + "/api/v1/knowledge-bases/" + kb["id"] + "/lifecycle/documents/" + unrelated
                code, body = http.call("GET", url)
                return code == 200 and json.loads(body)["data"]["snapshot"]["current"]["state"] == "active"
            startup.until(unrelated_active, 20)
            _, _, irrelevant = ask("rag-workbench-guide-other-source", False, unrelated)
            http.api("PUT", quick_id, quick_update)
            if reviewed_text not in matching or reviewed_text in irrelevant or context["answer_excerpt"] in matching or context["suggested_revision"] in matching:
                raise RuntimeError("reviewed guidance was not limited to a matching source or included raw user feedback")
            # This temporary source only exercises the guidance negative
            # control. Remove it before the later Agent-search controls so it
            # cannot pollute the single-document provenance assertion.
            http.api("DELETE", "/knowledge/" + unrelated)
            def unrelated_deleted():
                with sqlite3.connect((work / "core.db").as_uri() + "?mode=ro", uri=True) as db:
                    row = db.execute("SELECT deleted_at FROM knowledges WHERE id=?", (unrelated,)).fetchone()
                return row is not None and bool(row[0])
            startup.until(unrelated_deleted, 30)
            emit("[DOCUMENT-FEEDBACK] PASS: shared excerpt, reviewed wording, controlled activation and exact citation match")
        if os.environ.get("AGENTRAG_ANCHORED_FEEDBACK_E2E") == "1":
            review_route = "/knowledge-bases/" + kb["id"] + "/lifecycle/business/"
            quick_id = "/agents/builtin-quick-answer"
            quick = http.api("GET", quick_id)["data"]
            quick_update = {"name": quick["name"], "description": quick.get("description", ""), "config": quick["config"]}
            original_sessions = dict(sessions)

            def feedback_route(kind, response):
                match = re.search(r'"assistant_message_id"\s*:\s*"([^"]+)"', response)
                if not match:
                    raise RuntimeError("feedback test question returned no native answer id")
                message = match[1]

                def saved():
                    with sqlite3.connect((work / "core.db").as_uri() + "?mode=ro", uri=True) as db:
                        return db.execute("SELECT 1 FROM messages WHERE id=? AND is_completed=1 AND length(trim(content))>0", (message,)).fetchone()

                startup.until(saved, 10)
                return "/messages/" + sessions[kind] + "/" + message + "/feedback"

            def ask_for_feedback(phase):
                sessions.pop("rag", None)
                code, response, _ = ask(phase, False)
                if code != 200 or any(data.get("response_type") == "error" for _, data in helpers.sse_records(response)):
                    raise RuntimeError("feedback test question failed before it could produce a saved answer: " + phase)
                return feedback_route("rag", response)

            default_feedback = ask_for_feedback("rag-annotation-default-active")
            snapshot = http.api("GET", default_feedback)["data"]
            if "how many reviewers" not in snapshot.get("question", "") or model.old_answer not in snapshot.get("answer_excerpt", ""):
                raise RuntimeError("default Quick QA feedback preview missed the saved question or answer")
            comment = {"operation_id": uuid.uuid4().hex, "rating": "correction", "text": "OPENCOMMENT" + secrets.token_hex(8),
                       "snapshot_digest": snapshot["snapshot_digest"]}
            receipt = http.api("POST", default_feedback, comment)["data"]
            if http.api("POST", default_feedback, comment)["data"] != receipt:
                raise RuntimeError("default Quick QA feedback did not return an idempotent correction receipt")
            listing = http.api("GET", review_route + "feedback_list")["data"]
            summary = next(row for row in listing if row["id"] == receipt["id"])
            if summary.get("feedback_context") or summary.get("citation_chunks") or summary.get("text") or "annotation_checks" in summary:
                raise RuntimeError("feedback list exposed private content before opening the record")
            detail = http.api("GET", review_route + "feedback_list?id=" + receipt["id"])["data"][0]
            if receipt["status"] != "pending_review" or detail["feedback_context"].get("question") != snapshot.get("question") or detail.get("text") != comment["text"]:
                raise RuntimeError("opened feedback did not contain its server-derived snapshot and correction")
            pins = detail.get("citation_chunks", [])
            if not pins or any(pin["document_id"] != doc["id"] or not pin.get("version_id") for pin in pins):
                raise RuntimeError("verified cited chunk was unavailable for review")
            pin = pins[0]
            annotation = "ANCHOREDGUIDE" + secrets.token_hex(12)
            condition = "For the reviewers headcount in this clause"
            def payload(revision, data):
                return {"operation_id": uuid.uuid4().hex, "expected_revision": revision,
                        "business": {"id": receipt["id"], "payload": data}}
            anchor = {key: pin[key] for key in ("chunk_id", "content_hash")}
            review = {"text": annotation, "applies_when": condition, "source_clause": anchor}
            bad_anchor = dict(anchor, content_hash="0" * 64)
            status, _ = http.call("PUT", http.base_url + "/api/v1" + review_route + "feedback_approve",
                                  payload(receipt["revision"], dict(review, source_clause=bad_anchor)))
            if status not in (400, 409):
                raise RuntimeError("review accepted a non-matching chunk hash")
            approved = http.api("PUT", review_route + "feedback_approve", payload(receipt["revision"], review))["data"]
            if approved["status"] != "approved":
                raise RuntimeError("chunk annotation bypassed its cooling period")
            def diagnostic(expected):
                def observed():
                    record = http.api("GET", review_route + "feedback_list?id=" + receipt["id"])["data"][0]
                    recent = record.get("annotation_checks", [])
                    return recent and recent[0].get("outcome") == expected and set(recent[0]) == {"outcome", "checked_at"}
                startup.until(observed, 5)

            sessions.pop("rag", None)
            status, response, cooling = ask("rag-annotation-cooling-active", False)
            if status != 200 or model.old_answer not in response or annotation in cooling:
                raise RuntimeError("cooling annotation control failed")
            diagnostic("cooling_down")
            with sqlite3.connect(work / "ext.db") as db:
                db.execute("UPDATE ext_feedback_staging SET ready_at='2020-01-01T00:00:00Z' WHERE feedback_id=?", (receipt["id"],))
            active = http.api("PUT", review_route + "feedback_activate", payload(approved["revision"], {}))["data"]
            def check_annotation(phase, agent=False, new_session=True, query=None, guided_calls=1):
                if new_session:
                    sessions.pop("agent" if agent else "rag", None)
                status, response, captured = ask(phase, agent, query=query)
                if status != 200 or model.old_answer not in response:
                    raise RuntimeError("matching source did not produce an answer: " + phase)
                saved_answer = feedback_route("agent" if agent else "rag", response).split("/")[-2]
                with sqlite3.connect((work / "core.db").as_uri() + "?mode=ro", uri=True) as db:
                    saved_refs = db.execute("SELECT knowledge_references FROM messages WHERE id=?", (saved_answer,)).fetchone()[0]
                saved_refs = json.dumps(json.loads(saved_refs or "[]"))
                if receipt["id"] not in saved_refs or pin["version_id"] not in saved_refs:
                    raise RuntimeError("saved answer did not preserve the exact correction and source version: " + phase)
                prompts = [json.loads(body) for name, body in model.prompts if name == phase]
                guided = [messages for messages in prompts if messages
                          and "【已审核勘误】\n" in messages[-1].get("content", "")
                          and annotation in messages[-1]["content"].split("【已审核勘误】\n", 1)[1]]
                if status != 200 or len(guided) != guided_calls or comment["text"] in captured:
                    raise RuntimeError("matching chunk did not receive only the reviewed correction: " + phase)
                messages = guided[-1]
                system = messages[0].get("content", "")
                evidence = messages[-1].get("content", "")
                if messages[0].get("role") != "system" or annotation in system or "【已审核勘误】" not in system:
                    raise RuntimeError("trusted correction policy was missing or its system message included the evidence body")
                if messages[-1].get("role") != "user" or condition not in evidence or "【已审核勘误】" not in evidence:
                    raise RuntimeError("reviewed correction did not follow native evidence with its applicability data")
                reference_events = [data for _, data in helpers.sse_records(response) if data.get("response_type") == "references"]
                if not any(receipt["id"] in json.dumps(data) and pin["chunk_id"] in json.dumps(data) for data in reference_events):
                    raise RuntimeError("answer stream did not include the reviewed correction and original chunk: " + phase)
                if agent and not any(m.get("role") == "tool" and body_canary in m.get("content", "") for m in messages):
                    raise RuntimeError("Agent annotation was issued without the current matching search result")
                return prompts, messages

            if active["status"] != "active":
                raise RuntimeError("annotation did not activate")
            default_prompts, guided_default = check_annotation("rag-annotation-default-matching-active")
            rewrites = [messages for messages in default_prompts
                        if "rewrite_query" in json.dumps(messages) or "rewritten_query" in json.dumps(messages)]
            if len(rewrites) != 1 or annotation in json.dumps(rewrites) or body_canary not in json.dumps(guided_default):
                raise RuntimeError("default Quick QA did not separate query rewrite from verified answer generation")
            diagnostic("sent")
            parallel_marker = "PARALLELEVIDENCE" + secrets.token_hex(12)
            model.phase = "upload-parallel-source"
            other = upload_text("gov-parallel.txt", ("AGENTRAG_E2E_MARKER. Parallel clause. " + parallel_marker + " ") * 7)
            startup.until(lambda: state(other["id"]) == "active", 20)
            status, response, other_source = ask("rag-annotation-mismatch-active", False, other["id"])
            if status != 200 or model.old_answer not in response or parallel_marker not in other_source or body_canary in other_source or annotation in other_source:
                raise RuntimeError("Quick QA did not exercise a different exact source in the same knowledge base")
            diagnostic("source_mismatch")
            document_route = review_route + "correction_document?" + urllib.parse.urlencode({"document_id": doc["id"], "version_id": pin["version_id"]})
            markdown_route = document_route.replace("correction_document?", "correction_markdown?")
            history_route = document_route.replace("correction_document?", "correction_history?") + "&id=" + urllib.parse.quote(receipt["id"])
            page = http.api("GET", review_route + "correction_documents")["data"]
            if page.get("total") != 1 or page["items"][0]["active_count"] != 1 or page["items"][0]["version_id"] != pin["version_id"]:
                raise RuntimeError("workbench did not show a single versioned correction document")
            materials = http.api("GET", document_route)["data"]
            if len(materials["items"]) != 1 or annotation not in materials["markdown"] or comment["text"] in json.dumps(materials):
                raise RuntimeError("version preview did not contain exclusively the published correction")
            if os.environ.get("AGENTRAG_CORRECTION_ACL_E2E") == "1":
                history = http.api("GET", history_route)["data"]
                if history["current_state"] != "active" or not history["events"] or any(
                    field in json.dumps(history) for field in (annotation, comment["text"], "payload_json")
                ):
                    raise RuntimeError("governance audit exported invalid state, decisions or private fields")
                reader = helpers.HTTP(http.base_url)
                reader_account = {"username": "reader_" + nonce, "email": "reader-" + nonce + "@example.invalid",
                                  "password": helpers.new_password()}
                reader.api("POST", "/auth/register", reader_account)
                auth = reader.api("POST", "/auth/login", {key: reader_account[key] for key in ("email", "password")})
                reader.token = auth["token"]
                reader_tenant = auth["active_tenant"]["id"]
                if reader_tenant == kb["tenant_id"]:
                    raise RuntimeError("reader fixture requires a distinct workspace")

                def deny_reader(route):
                    code, body = reader.call("GET", http.base_url + "/api/v1" + route)
                    if code not in (403, 404) or annotation in body or comment["text"] in body:
                        raise RuntimeError("unauthorized reader received correction material")

                for route in (review_route + "correction_documents", document_route, markdown_route, history_route):
                    deny_reader(route)
                org = http.api("POST", "/organizations", {"name": "Correction readers"})["data"]
                http.api("POST", "/organizations/" + org["id"] + "/invite", {"tenant_id": reader_tenant, "role": "viewer"})
                shares_route = "/knowledge-bases/" + kb["id"] + "/shares"
                share = http.api("POST", shares_route, {"organization_id": org["id"], "permission": "viewer"})["data"]
                listed = reader.api("GET", review_route + "correction_documents")["data"]
                readable = reader.api("GET", document_route)["data"]
                downloadable = reader.api("GET", markdown_route)["data"]
                if listed["total"] != 1 or readable["active_count"] != 1 or annotation not in readable["markdown"] or comment["text"] in json.dumps(readable):
                    raise RuntimeError("shared reader did not receive only the published correction projection")
                if downloadable["markdown"] != readable["markdown"] or "items" in downloadable:
                    raise RuntimeError("active download did not return the authorized Markdown")
                deny_reader(document_route + "&state=all")
                deny_reader(markdown_route + "&state=all")
                deny_reader(history_route)
                status, body = http.call("DELETE", http.base_url + "/api/v1" + shares_route + "/" + share["id"])
                if status not in (200, 204):
                    raise RuntimeError("could not revoke the fixture’s read share")
                for route in (review_route + "correction_documents", document_route, markdown_route, history_route):
                    deny_reader(route)
                emit("[FEEDBACK-MATERIALS-ACL] PASS: cross-tenant and revoked shares denied; viewer active preview, counts and download; governance-only archive and redacted audit export")
            check_annotation("rag-materials-reverse-active", query=annotation)
            if any(phase == "rag-materials-reverse-active" and body_canary in passages for phase, passages in model.rerank_inputs):
                raise RuntimeError("reverse recall positive did not exclude ordinary source retrieval")
            check_annotation("rag-materials-first-active")
            check_annotation("rag-materials-second-active", new_session=False, query="How many reviewers does AGENTRAG_E2E_MARKER require?")
            followup_prompts, _ = check_annotation("rag-materials-followup-active", new_session=False, query="And the headcount?")
            if model.old_answer in json.dumps(followup_prompts) or "How many reviewers does AGENTRAG_E2E_MARKER require?" not in json.dumps(followup_prompts):
                raise RuntimeError("follow-up did not reuse the prior user question exclusively")
            status, response, unrelated_prompts = ask("rag-materials-other-topic-active", False, query="Where is the finance desk?")
            if status != 200 or annotation in unrelated_prompts:
                raise RuntimeError("unrelated topic consumed the previous correction")
            check_annotation("rag-materials-fifth-active", new_session=False)
            diagnostic("sent")
            natural_query = "Under the reimbursement policy, how many reviewers are required?"
            check_annotation("agent-search-materials-matching-active", agent=True, query=natural_query)
            first_agent_request = next((item for item in model.agent_requests if item[0] == "agent-search-materials-matching-active"), None)
            if not first_agent_request or first_agent_request[1] is not None or not any(message.get("role") == "tool" for message in first_agent_request[2]):
                raise RuntimeError("Agent did not obtain native search evidence before the first provider generation")
            multiround_phase = "agent-search-materials-multiround-active"
            _, final_messages = check_annotation(multiround_phase, agent=True, query=natural_query, guided_calls=2)
            requests = [item for item in model.agent_requests if item[0] == multiround_phase]
            tool_messages = [message for message in final_messages if message.get("role") == "tool"]
            if len(requests) != 2 or any(request[1] is not None for request in requests) or len(tool_messages) != 2 or not any(
                message.get("tool_call_id") == "call-materials-second-search" for message in tool_messages
            ):
                raise RuntimeError("Agent did not consume native and model-initiated search results in the final model call")
            emit("[AGENT-CORRECTION-MULTIROUND] PASS: native preflight, autonomous second search, exact evidence and persisted citations")
            http.api("PUT", quick_id, quick_update)
            if publication_mode in ("manual", "binary"):
                def publish_action(action, data=None, proposal=None):
                    command = {"operation_id": uuid.uuid4().hex, "expected_revision": proposal["revision"] if proposal else 0,
                               "business": {"payload": data or {}}}
                    if proposal:
                        command["business"]["id"] = proposal["id"]
                    return http.api("PUT", review_route + action, command)["data"]

                def original_chunk():
                    with sqlite3.connect((work / "core.db").as_uri() + "?mode=ro", uri=True) as db:
                        return db.execute("SELECT content FROM chunks WHERE id=?", (pin["chunk_id"],)).fetchone()

                original = original_chunk()
                revised_marker = "REVISIONEVIDENCE" + secrets.token_hex(12)
                new_text = f"# Reimbursement policy\n\nAGENTRAG_E2E_MARKER. Three reviewers are required. {revised_marker}"
                source = {"document_id": doc["id"], "guidance_ids": [receipt["id"]]}
                model.phase = "upload-formal-revision"
                if publication_mode == "manual":
                    proposal = publish_action("revision_create", dict(source, text=new_text))
                    proposal = publish_action("revision_approve", proposal=proposal)
                else:
                    proposal = publish_action("revision_create_binary", source)
                    proposal = publish_action("revision_approve", proposal=proposal)
                    uploaded = upload_text("gov-revised.txt", new_text)
                    startup.until(lambda: state(uploaded["id"]) == "active", 20)
                    proposal = publish_action("revision_bind_binary", {"target_document_id": uploaded["id"]}, proposal)
                deadline = time.monotonic() + 60
                while proposal["status"] != "published":
                    proposal = publish_action("revision_publish", proposal=proposal)
                    if proposal["status"] != "published":
                        if proposal["status"] != "publishing" or time.monotonic() >= deadline:
                            raise RuntimeError("correction-linked revision did not complete native publication")
                        time.sleep(0.3)
                target = proposal.get("target_document_id")
                if not target or target == doc["id"] or state() != "superseded" or state(target) != "active" or not original or original_chunk() != original:
                    raise RuntimeError("formal correction publication did not switch versions and preserve the original")
                archived = http.api("GET", document_route + "&state=all")["data"]
                if archived["active_count"] != 0 or archived["items"][0]["id"] != receipt["id"] or annotation not in archived["markdown"]:
                    raise RuntimeError("former version correction was not available to the authorized reviewer as history")
                status, _, captured = ask("rag-annotation-retired", False)
                if status != 200 or body_canary in captured or annotation in captured:
                    raise RuntimeError("retired source or its correction still reached a new model call")
                status, response, captured = ask("rag-correction-published-active", False, target)
                if status != 200 or model.old_answer not in response or revised_marker not in captured or body_canary in captured or annotation in captured or receipt["id"] in response:
                    raise RuntimeError("new version did not supply the active answer evidence independently of the old correction")
                emit("[FEEDBACK-MATERIALS-PUBLISH] PASS: " + publication_mode + " active correction -> formal revision -> native indexing -> new version evidence; original bytes unchanged and authorized old correction archived")
                return 0
            revoked = http.api("PUT", review_route + "feedback_withdraw", payload(active["revision"], {}))["data"]
            for kind in ("rag", "agent"):
                phase = "agent-search-materials-withdrawn-active" if kind == "agent" else "rag-annotation-withdrawn-active"
                status, response, after_withdrawal = ask(phase, kind == "agent", query=natural_query if kind == "agent" else None)
                if status != 200 or model.old_answer not in response or revoked["status"] != "withdrawn" or annotation in after_withdrawal or receipt["id"] in response:
                    raise RuntimeError("withdrawn annotation still reached a new model call")
            diagnostic("withdrawn")
            helpers.stop_owned(host)
            host = None
            host_env["AGENTRAG_CORRECTION_KB_IDS"] = "[]"
            host = start_host()
            status, response, captured = ask("rag-materials-rollback-active", False)
            if status != 200 or model.old_answer not in response or annotation in captured or receipt["id"] in response or not any(
                data.get("data", {}).get("correction_check") == "disabled"
                for _, data in helpers.sse_records(response) if data.get("response_type") == "answer" and isinstance(data.get("data"), dict)
            ):
                raise RuntimeError("per-KB rollback did not preserve withdrawal or declare the disabled correction path")
            helpers.stop_owned(host)
            host = None
            host_env["AGENTRAG_CORRECTION_KB_IDS"] = kb["id"]
            host = start_host()
            emit("[CORRECTION-ROLLBACK] PASS: KB rollout disabled and restored; withdrawn correction stayed out of generation and citations")
            http.api("PUT", quick_id, quick_update)
            sessions.clear()
            sessions.update(original_sessions)
            emit("[FEEDBACK-MATERIALS] PASS: version preview, correction-only recall, native citations, five default Quick QA turns, same-session withdrawal and Agent")
        status, response, captured = ask("rag-fallback-active", False)
        if status != 200 or desc_canary not in captured or model.old_answer not in response:
            raise RuntimeError("active fallback description positive control failed")
        emit("[RAG-FALLBACK-ACTIVE] PASS: independent description canary reached actual empty-search fallback prompt")
        for action in ("search", "read", "list"):
            status, response, captured = ask("agent-" + action + "-active", True)
            marker = body_canary if action != "list" else desc_canary
            if status != 200 or model.tool_step != 1 or marker not in captured or model.old_answer not in response or not any(marker in output for phase,output in model.tool_outputs if phase=="agent-"+action+"-active"):
                raise RuntimeError("active Agent " + action + " positive control failed")
            emit("[AGENT-" + action.upper() + "-ACTIVE] PASS: real tool ran and permitted source reached actual follow-up prompt")
            if action == "search" and os.environ.get("AGENTRAG_ANCHORED_FEEDBACK_E2E") == "1":
                agent_feedback = feedback_route("agent", response)
                agent_snapshot = http.api("GET", agent_feedback)["data"]
                agent_rating = http.api("POST", agent_feedback, {"operation_id": uuid.uuid4().hex,
                    "rating": "like", "snapshot_digest": agent_snapshot["snapshot_digest"]})["data"]
                if agent_rating["status"] != "received":
                    raise RuntimeError("persisted Agent answer did not accept a rating")
                emit("[AGENT-ANSWER-RATING] PASS: owner submitted feedback on a completed Agent answer")
            if action == "search" and os.environ.get("AGENTRAG_QA_FEEDBACK_E2E") == "1":
                answer = re.search(r'"assistant_message_id"\s*:\s*"([^"]+)"', response)
                if not answer or not any(phase == "agent-search-active" and body_canary in passages for phase, passages in model.rerank_inputs):
                    raise RuntimeError("Agent feedback control did not send the source to the reranker")
                route = "/knowledge-bases/" + kb["id"] + "/lifecycle/business/feedback_capture"
                payload = {"session_id": sessions["agent"], "message_id": answer[1], "topic": "reranked-turn", "text": "Check source eligibility"}
                command = {"expected_revision": 0, "operation_id": "agent-reranked-feedback", "business": {"payload": payload}}
                receipt = http.api("PUT", route, command)["data"]
                with sqlite3.connect((work / "ext.db").as_uri() + "?mode=ro", uri=True) as db:
                    chunks, run_state = db.execute("SELECT b.retrieved_chunk_ids,r.state FROM ext_feedback_qa_bindings b JOIN ext_provenance_runs r USING(tenant_id,knowledge_base_id,run_id) WHERE b.feedback_id=?", (receipt["id"],)).fetchone()
                def completed_agent_steps():
                    with sqlite3.connect((work / "core.db").as_uri() + "?mode=ro", uri=True) as db:
                        return db.execute("SELECT agent_steps FROM messages WHERE id=? AND is_completed=1 AND json_array_length(agent_steps)>0", (answer[1],)).fetchone()
                startup.until(completed_agent_steps, 10)
                steps = json.loads(completed_agent_steps()[0])
                native_ids = {
                    row["chunk_id"] for step in steps for call in (step.get("tool_calls") or [])
                    if call["name"] == "search_knowledge" and call["result"]["success"]
                    for row in call["result"]["data"]["results"]
                }
                if not native_ids or set(json.loads(chunks)) != native_ids or run_state != "sealed" or receipt["status"] != "pending_review":
                    raise RuntimeError("reranked Agent feedback did not bind the source chunk of a sealed answer")
                emit("[M5-QA-RERANK] PASS: reranker consumed the active source and the sealed Agent answer accepted staged feedback")
        if os.environ.get("AGENTRAG_GUIDANCE_E2E") == "1":
            route = "/knowledge-bases/" + kb["id"] + "/lifecycle/business/"
            topic = "guide-" + nonce
            instruction = "GUIDEONLY" + secrets.token_hex(12)
            foreign = "FOREIGNGUIDE" + secrets.token_hex(12)
            http.api("PUT", route + "guidance_seed", {"expected_revision": 0, "operation_id": "guide-seed", "business": {"payload": {"topic": topic, "text": instruction}}})
            with sqlite3.connect(work / "ext.db") as db:
                db.execute("INSERT INTO ext_feedback_guidances VALUES(?, 'foreign-kb','foreign-guide',NULL,'seed','foreign',?,1.0,'active',1,'now')", (str(kb["tenant_id"]), foreign))
            quick = http.api("GET", "/agents/builtin-quick-answer")["data"]
            quick_update = {"name": quick["name"], "description": quick.get("description", ""), "config": quick["config"]}
            original_sessions = dict(sessions)
            def wait_for_guided_run(response):
                answer = re.search(r'"assistant_message_id"\s*:\s*"([^"]+)"', response)
                if not answer:
                    raise RuntimeError("guidance turn lacks a native answer ID")
                def guided_run_sealed():
                    with sqlite3.connect((work / "ext.db").as_uri() + "?mode=ro", uri=True) as db:
                        row = db.execute("SELECT r.state,count(g.step_id) FROM ext_provenance_runs r LEFT JOIN ext_guidance_step_inputs g USING(tenant_id,knowledge_base_id,run_id) WHERE json_extract(r.payload_json,'$.identity.attempt_id')=? GROUP BY r.run_id", (answer[1],)).fetchone()
                        return row and row[0] == "sealed" and row[1] > 0
                startup.until(guided_run_sealed, 10)

            sessions.pop("rag", None)
            status, response, captured = ask("rag-guidance-default", False)
            if status != 200 or instruction not in captured or foreign in captured:
                raise RuntimeError("default Quick QA did not receive authorized guidance")
            wait_for_guided_run(response)
            exact_config = dict(quick["config"], enable_rewrite=False, enable_query_expansion=False, rerank_model_id="", multi_turn_enabled=False)
            http.api("PUT", "/agents/builtin-quick-answer", dict(quick_update, config=exact_config))
            for kind in ("rag", "agent"):
                sessions.pop(kind, None)
                phase = "agent-search-guidance" if kind == "agent" else "rag-guidance"
                status, response, captured = ask(phase, kind == "agent")
                if status != 200 or instruction not in captured or foreign in captured:
                    raise RuntimeError(kind + " did not receive only the authorized active guidance")
                wait_for_guided_run(response)
            for index in (1, 2):
                http.api("PUT", route + "feedback_create", {"expected_revision": 0, "operation_id": "guide-negative-" + str(index), "business": {"payload": {"topic": topic, "text": "Quarantine this test instruction", "negative": True}}})
            status, _, captured = ask("rag-guidance-quarantined", False)
            if status != 200 or instruction in captured or foreign in captured:
                raise RuntimeError("quarantined guidance replayed into the next model request")
            sessions.clear()
            sessions.update(original_sessions)
            http.api("PUT", "/agents/builtin-quick-answer", quick_update)
            with sqlite3.connect((work / "ext.db").as_uri() + "?mode=ro", uri=True) as db:
                max_budget, = db.execute("SELECT max(n) FROM (SELECT sum(units) n FROM ext_guidance_calls GROUP BY tenant_id,user_id,session_id)").fetchone()
                max_uses, = db.execute("SELECT max(n) FROM (SELECT count(*) n FROM ext_guidance_call_items i JOIN ext_guidance_calls c USING(tenant_id,knowledge_base_id,run_id,call_id) GROUP BY i.tenant_id,i.knowledge_base_id,c.user_id,c.session_id,i.guidance_id)").fetchone()
                if max_budget > 800 or max_uses > 2 or db.execute("PRAGMA foreign_key_check").fetchone():
                    raise RuntimeError("guidance session budget or referential integrity failed")
            emit("[M5-GUIDANCE] PASS: default Quick QA, exact RAG and Agent received authorized guidance and sealed; foreign/quarantined instructions omitted; persistent budgets respected")
        startup.until(lambda: provenance_counts()[0] >= 5 and provenance_counts()[1] >= 5 and provenance_counts()[2] > 0, 5)
        with sqlite3.connect(work / "ext.db") as db:
            unknown = db.execute("SELECT count(*) FROM ext_provenance_runs WHERE state='unattributed'").fetchone()[0]
            sealed = db.execute("SELECT count(*) FROM ext_provenance_runs WHERE state='sealed'").fetchone()[0]
            if unknown < 1:
                raise RuntimeError("unsupported composition incorrectly sealed")
            if sealed < 1:
                raise RuntimeError("no actual native model execution sealed")
            if db.execute("PRAGMA foreign_key_check").fetchone():
                raise RuntimeError("provenance foreign key violation")
        emit("[PROVENANCE-AGENT] PASS: actual native execution sealed; real fields verified against native source snapshots; unsupported composition remains unattributed; FK check clean")
        repeal = None
        if os.environ.get("AGENTRAG_BUSINESS_E2E") == "1":
            import business_e2e
            business_guidance, family_history = business_e2e.run(http, kb, doc, emit, work / "ext.db")
            with sqlite3.connect(work / "ext.db") as db:
                if db.execute("PRAGMA foreign_key_check").fetchone():
                    raise RuntimeError("business foreign key violation")
                if db.execute("SELECT count(*) FROM ext_revision_proposals WHERE status='published'").fetchone()[0] != 1:
                    raise RuntimeError("business publication count mismatch")
                if db.execute("SELECT count(*) FROM ext_projection_outbox WHERE projection_type='family_tag'").fetchone()[0] != 2:
                    raise RuntimeError("family projection intent missing")
            emit("[M5-FACTS] PASS: source superseded, single published proposal, two family projection intents and FK integrity")
            if os.environ.get("AGENTRAG_FAMILY_CUTOVER_E2E") == "1":
                business_e2e.run_family_cutover(http, kb, emit, env["AGENTRAG_MANAGEMENT_KEY"], family_history, work / "ext.db")
            if os.environ.get("AGENTRAG_SPLIT_E2E") == "1":
                business_e2e.run_split(http, kb, emit, work / "ext.db", business_guidance)
            if os.environ.get("AGENTRAG_REPEAL_E2E") == "1":
                repeal = business_e2e.run_repeal(http, kb, emit, ask)
            if os.environ.get("AGENTRAG_CLAUSE_E2E") == "1":
                business_e2e.run_clause(http, kb, emit)
        helpers.stop_owned(host); host = None
        with sqlite3.connect(work / "ext.db") as db:
            db.execute("UPDATE ext_document_annotations SET effective_until='2000-01-01T00:00:00Z', revision=revision+1 WHERE document_id=?", (doc["id"],))
        host = start_host(); startup.until(lambda: state() == "stale", 10)
        emit("[LIFECYCLE] PASS: real startup sweep active -> stale from annotation input")
        for phase, agent in [("rag-stale", False), ("rag-fallback-stale", False)] + [("agent-" + action + "-stale", True) for action in ("search", "read", "list")]:
            status, response, captured = ask(phase, agent)
            if status != 200:
                raise RuntimeError(phase + " did not complete: HTTP " + str(status))
            if any(marker in captured for marker in (body_canary, desc_canary, model.old_answer)):
                raise RuntimeError(phase + " leaked body/description/old-answer canary")
            if any(body_canary in inputs or desc_canary in inputs for label,inputs in model.rerank_inputs if label==phase):
                raise RuntimeError(phase+" leaked revoked content to reranker")
            emit("[" + phase.upper() + "] PASS: same session/known IDs; no body, description or old-answer canary in ANY model request")
        def open_runs():
            with sqlite3.connect(work / "ext.db") as db:
                return db.execute("SELECT count(*) FROM ext_provenance_runs WHERE state='open'").fetchone()[0]
        startup.until(lambda: open_runs() == 0, 5)
        emit("[PROVENANCE-TERMINAL] PASS: finished requests leave no open run")
        overview = "/knowledge-bases/" + kb["id"] + "/lifecycle?title=gov.txt&state="
        listed = http.api("GET", overview + "stale")["data"]
        if listed["total"] != 1 or listed["documents"][0]["document_id"] != doc["id"] or http.api("GET", overview + "active")["data"]["total"] != 0:
            raise RuntimeError("scoped governance title/state filter failed")
        emit("[GOVERNANCE-LIST] PASS: authenticated Core API forwarded title and state to the read-only join")
        model.mcp_id = http.api("POST", "/mcp-services", {"name": "AgentRAG", "enabled": True, "transport_type": "http-streamable", "url": "http://127.0.0.1:8090/mcp", "headers": {"Authorization": "Bearer " + env["AGENTRAG_MANAGEMENT_KEY"]}})["data"]["id"]
        agent = http.api("GET", "/agents/builtin-smart-reasoning")["data"]
        agent["config"]["mcp_selection_mode"] = "selected"
        agent["config"]["mcp_services"] = [model.mcp_id]
        http.api("PUT", "/agents/builtin-smart-reasoning", {"name": agent["name"], "description": agent.get("description", ""), "config": agent["config"]})
        status, _, captured = ask("agent-archive-history", True)
        warning = "【系统提示：以下内容来自历史归档文档，已失效，请在回答时提醒用户】"
        if status != 200 or model.tool_step != 2 or warning not in captured or body_canary not in captured or desc_canary in captured or model.old_answer in captured:
            raise RuntimeError("native on-demand MCP archive discovery or follow-up prompt failed")
        emit("[AGENT-ARCHIVE] PASS: native discovery and MCP call delivered only the authorized stale excerpt with the warning")
        if repeal:
            old, canary = repeal
            status, _, captured = ask("agent-archive-repeal", True, old)
            if status != 200 or model.tool_step != 2 or warning not in captured or canary not in captured:
                raise RuntimeError("archived repeal was not available through authorized historical search")
            emit("[REL-06-HISTORY] PASS: native MCP found the archived policy with the historical warning")
        if os.environ.get("AGENTRAG_FAMILY_E2E") == "1":
            import business_e2e
            business_e2e.run_families(http, kb, emit, env["AGENTRAG_MANAGEMENT_KEY"])
            if os.environ.get("AGENTRAG_FAMILY_SEMANTIC_E2E") == "1":
                required = {f"Employee handbook {year}" for year in (2024, 2025, 2026)}
                sent = set(model.family_embedding_inputs)
                retired_title = http.api("GET", "/knowledge/" + doc["id"])["data"]["title"]
                if not required.issubset(sent) or retired_title in sent or "Foreign handbook" in sent:
                    raise RuntimeError("family embeddings missed allowed titles or received stale/foreign inputs")
                emit("[FAM-04-SEMANTIC] PASS: real Host sent scoped title embeddings without forwarding Core credentials")
        if os.environ.get("AGENTRAG_FAMILY_HISTORY_E2E") == "1":
            import business_e2e
            business_e2e.run_family_history(http, kb, emit)
        helpers.stop_owned(host); host = None
        for phase, agent in (("rag-offline", False), ("agent-search-offline", True)):
            status, response, captured = ask(phase, agent)
            if model.last_generations!=0 or any(marker in captured for marker in (body_canary,desc_canary,model.old_answer)):
                raise RuntimeError(phase + " generated an answer or sent revoked content with Host unavailable")
            if not agent and helpers.GATE.encode("utf-8") not in (work/"core-private.log").read_bytes():
                raise RuntimeError("missing native fail-closed Bridge log")
            emit("[" + phase.upper() + "] PASS: Host unavailable; answer_generation_calls=0; no governed source content in query-understanding requests")
        emit("[RESULT] Governance leak E2E PASS; exit=0; local fixtures only, no cloud model claim")
        return 0
    except Exception as error:
        emit("[RESULT] FAIL: " + str(error))
        emit("[PRIVATE-DIAGNOSTICS] " + str(work))
        return 1
    finally:
        helpers.stop_owned(host); helpers.stop_owned(core)
        if model is not None:
            model.base.close()
        for handle in handles:
            handle.close()
        log.close()


if __name__ == "__main__":
    raise SystemExit(main())
