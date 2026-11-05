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
        self.tool_outputs = []
        self.tool_step = 0
        self.doc_id = ""
        self.old_answer = "ANSWERONLY" + secrets.token_hex(12)
        self.no_match = "NOMATCH" + secrets.token_hex(12)
        model = self
        original = self.base.server.RequestHandlerClass.do_POST

        def handle(handler):
            raw = handler.rfile.read(int(handler.headers.get("Content-Length", "0")))
            request = json.loads(raw)
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
            if handler.path.endswith("/embeddings") and model.phase.startswith("rag-fallback"):
                inputs=request.get("input",[])
                if not isinstance(inputs,list): inputs=[inputs]
                handler.send_response(200);handler.send_header("Content-Type","application/json");handler.end_headers()
                handler.wfile.write(json.dumps({"object":"list","data":[{"object":"embedding","index":i,"embedding":[-0.1]*8} for i in range(len(inputs))],"model":"governance-embedding","usage":{"prompt_tokens":1,"total_tokens":1}}).encode())
                return
            if model.phase.startswith("rag-fallback") and ("rewrite_query" in json.dumps(messages) or "rewritten_query" in json.dumps(messages)):
                model.respond(handler, {"content": json.dumps({"rewrite_query": model.no_match, "intent": "kb_search", "entities": []})}, "stop", bool(request.get("stream")))
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
        env.update(ENV_FILE=str(ROOT / ".env"), DB_DRIVER="sqlite", DB_PATH=str(work / "core.db"), RETRIEVE_DRIVER="sqlite", STORAGE_TYPE="local", LOCAL_STORAGE_BASE_DIR=str(work / "files"), STREAM_MANAGER_TYPE="memory", SERVER_HOST="127.0.0.1", SERVER_PORT=str(port), WEKNORA_WEB_DIR=str(ROOT / "web"), SSRF_DISABLE="true", NEO4J_ENABLE="false", ENABLE_GRAPH_RAG="false", WEKNORA_SANDBOX_MODE="disabled", LOG_PATH="", JWT_SECRET=secrets.token_hex(32), SYSTEM_SIGNING_KEY=secrets.token_hex(32))
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
        host_binary = work / "extension-host"
        subprocess.run(["/usr/local/go/bin/go", "build", "-mod=readonly", "-o", str(host_binary), "./cmd/extension-host"], cwd=ROOT / "extensions", env=dict(env, CGO_ENABLED="0", GOWORK="off"), check=True)
        host_env = dict(env, AGENTRAG_MODE="worker", AGENTRAG_EXT_DB=str(work / "ext.db"), AGENTRAG_WEKNORA_URL=http.base_url, AGENTRAG_WEKNORA_API_KEY=api_key, AGENTRAG_TENANT_ID=str(kb["tenant_id"]), AGENTRAG_KB_IDS=kb["id"], AGENTRAG_RECONCILE_INTERVAL="250ms")
        def start_host():
            output = (work / "host-private.log").open("a"); handles.append(output)
            process = subprocess.Popen([str(host_binary)], cwd=ROOT / "extensions", env=host_env, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
            helpers.wait_health(http, process, "http://127.0.0.1:8090/health", 30)
            return process
        host = start_host()
        body_canary = "BODYONLY" + secrets.token_hex(12)
        desc_canary = "DESCONLY" + secrets.token_hex(12)
        boundary = "gov-" + nonce
        text = ("AGENTRAG_E2E_MARKER. Two reviewers are required. " + body_canary + " ") * 6
        payload = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="gov.txt"\r\nContent-Type: text/plain\r\n\r\n{text}\r\n--{boundary}--\r\n').encode()
        request = urllib.request.Request(http.base_url + "/api/v1/knowledge-bases/" + kb["id"] + "/knowledge/file", data=payload, headers={"Authorization": "Bearer " + http.token, "Content-Type": "multipart/form-data; boundary=" + boundary}, method="POST")
        with http.opener.open(request, timeout=30) as response:
            doc = json.load(response)["data"]
        startup.until(lambda: http.api("GET", "/knowledge/" + doc["id"])["data"]["parse_status"] == "completed")
        # Independent test-only metadata input; never changes production data.
        with sqlite3.connect(work / "core.db") as db:
            db.execute("UPDATE knowledges SET description=? WHERE id=?", (desc_canary, doc["id"]))
        def state():
            with sqlite3.connect((work / "ext.db").as_uri() + "?mode=ro", uri=True) as db:
                row = db.execute("SELECT state FROM ext_lifecycle_current WHERE document_id=?", (doc["id"],)).fetchone()
                return row[0] if row else None
        startup.until(lambda: state() == "active", 20)
        model.doc_id, model.kb_id = doc["id"], kb["id"]
        sessions = {}
        def ask(phase, agent):
            model.phase = phase; model.tool_step = 0
            kind = "agent" if agent else "rag"
            if kind not in sessions:
                sessions[kind] = http.api("POST", "/sessions", {"title": "Private governance validation"})["data"]["id"]
            offset = len(model.prompts)
            generation_offset=model.base.generation_calls
            endpoint="/agent-chat/" if agent else "/knowledge-chat/"
            payload={"query": "Retrieve AGENTRAG_E2E_MARKER: how many reviewers are required?", "knowledge_base_ids": [kb["id"]], "knowledge_ids": [doc["id"]], "agent_enabled": agent, "agent_id": "builtin-smart-reasoning" if agent else "builtin-quick-answer", "web_search_enabled": False, "summary_model_id": models[0], "disable_title": True}
            if phase == "agent-archive-history":
                payload["query"] = "仅查阅已经失效的历史条款 AGENTRAG_E2E_MARKER，提醒我它是否仍有效。"
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
            command = {"expected_revision": 0, "operation_id": "qa-feedback-e2e", "business": {"payload": {"session_id": feedback_session, "message_id": answer_id, "topic": "fixture-turn", "text": "Review the effective clause", "negative": True}}}
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
            sessions.pop("rag", None)
            status, response, captured = ask("rag-guidance-default", False)
            if status != 200 or instruction not in captured or foreign in captured:
                raise RuntimeError("default Quick QA did not receive authorized guidance")
            answer = re.search(r'"assistant_message_id"\s*:\s*"([^"]+)"', response)
            if not answer:
                raise RuntimeError("default Quick QA guidance turn lacks an answer ID")
            with sqlite3.connect((work / "ext.db").as_uri() + "?mode=ro", uri=True) as db:
                run_state, = db.execute("SELECT state FROM ext_provenance_runs WHERE json_extract(payload_json,'$.identity.attempt_id')=?", (answer[1],)).fetchone()
            if run_state != "unattributed":
                raise RuntimeError("guidance incorrectly completed default Quick QA provenance")
            exact_config = dict(quick["config"], enable_rewrite=False, enable_query_expansion=False, rerank_model_id="", multi_turn_enabled=False)
            http.api("PUT", "/agents/builtin-quick-answer", dict(quick_update, config=exact_config))
            for kind in ("rag", "agent"):
                sessions.pop(kind, None)
                phase = "agent-search-guidance" if kind == "agent" else "rag-guidance"
                status, response, captured = ask(phase, kind == "agent")
                if status != 200 or instruction not in captured or foreign in captured:
                    raise RuntimeError(kind + " did not receive only the authorized active guidance")
                answer = re.search(r'"assistant_message_id"\s*:\s*"([^"]+)"', response)
                if not answer:
                    raise RuntimeError(kind + " guidance turn lacks a native answer ID")
                def guided_run_sealed():
                    with sqlite3.connect((work / "ext.db").as_uri() + "?mode=ro", uri=True) as db:
                        row = db.execute("SELECT r.state,count(g.step_id) FROM ext_provenance_runs r LEFT JOIN ext_guidance_step_inputs g USING(tenant_id,knowledge_base_id,run_id) WHERE json_extract(r.payload_json,'$.identity.attempt_id')=? GROUP BY r.run_id", (answer[1],)).fetchone()
                        return row and row[0] == "sealed" and row[1] > 0
                startup.until(guided_run_sealed, 10)
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
            emit("[M5-GUIDANCE] PASS: default Quick QA received guidance and remained unattributed; exact RAG and Agent sealed; foreign/quarantined instructions omitted; persistent budgets respected")
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
        if os.environ.get("AGENTRAG_BUSINESS_E2E") == "1":
            import business_e2e
            business_e2e.run(http, kb, doc, emit, work / "ext.db")
            with sqlite3.connect(work / "ext.db") as db:
                if db.execute("PRAGMA foreign_key_check").fetchone():
                    raise RuntimeError("business foreign key violation")
                if db.execute("SELECT count(*) FROM ext_revision_proposals WHERE status='published'").fetchone()[0] != 1:
                    raise RuntimeError("business publication count mismatch")
                if db.execute("SELECT count(*) FROM ext_projection_outbox WHERE projection_type='family_tag'").fetchone()[0] != 2:
                    raise RuntimeError("family projection intent missing")
            emit("[M5-FACTS] PASS: source superseded, single published proposal, two family projection intents and FK integrity")
        helpers.stop_owned(host); host = None
        with sqlite3.connect(work / "ext.db") as db:
            db.execute("UPDATE ext_document_annotations SET effective_until='2000-01-01T00:00:00Z', revision=revision+1 WHERE document_id=?", (doc["id"],))
        host = start_host(); startup.until(lambda: state() == "stale", 10)
        emit("[LIFECYCLE] PASS: real startup sweep active -> stale from annotation input")
        for phase, agent in [("rag-stale", False), ("rag-fallback-stale", False)] + [("agent-" + action + "-stale", True) for action in ("search", "read", "list")]:
            status, response, captured = ask(phase, agent)
            if status != 200 or any(marker in captured for marker in (body_canary, desc_canary, model.old_answer)):
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
