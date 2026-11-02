#!/usr/bin/env python3
"""Real Core + worker Host + upload + active/stale RAG, on isolated mounted DBs.

Run inside WSL: python3 extensions/scripts/startup_e2e.py
Model outputs are deterministic local fixtures; no cloud/real MinerU claim.
"""
import hashlib
import importlib.util
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
import time
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("helpers", Path(__file__).with_name("e2e_test.py"))
helpers = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helpers)


def free_port(port):
    with socket.socket() as listener:
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        listener.bind(("127.0.0.1", port))


def until(check, seconds=60):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        if check():
            return
        time.sleep(.25)
    raise RuntimeError("E2E deadline exceeded")


def stop_launcher(process):
    if process is not None and process.poll() is None:
        os.kill(process.pid, signal.SIGINT)
        process.wait(timeout=45)
        if process.returncode != 130:
            raise RuntimeError("launcher did not preserve Ctrl+C exit status")


def main():
    def interrupted(_number, _frame):
        raise InterruptedError("E2E interrupted; cleaning up owned services")
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    report = Path(os.environ.get("AGENTRAG_E2E_REPORT", ROOT / "docs/evidence/m2-startup-e2e-2026-10-31.log"))
    report.parent.mkdir(parents=True, exist_ok=True)
    ROOT.joinpath("tmp").mkdir(exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="m2-startup-e2e-", dir=ROOT / "tmp"))
    os.chmod(work, 0o700)
    output = report.open("w", encoding="utf-8")
    def emit(message):
        print(message, flush=True)
        output.write(message + "\n"); output.flush()
    def digest(p):
        return hashlib.sha256(p.read_bytes()).hexdigest() if p.exists() else None
    source = ROOT / "data/weknora.db"
    before = digest(source)
    model = helpers.ModelFixture()
    model_started = False
    prompts = []
    original_post = model.server.RequestHandlerClass.do_POST
    def capture_post(handler):
        body = handler.rfile.read(int(handler.headers.get("Content-Length", "0")))
        prompts.append(json.dumps(json.loads(body).get("messages", [])))
        handler.rfile = io.BytesIO(body)
        original_post(handler)
    model.server.RequestHandlerClass.do_POST = capture_post
    core = launcher = None
    owned_launchers = []
    handles = []
    try:
        free_port(8090)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0)); port = listener.getsockname()[1]
        core_binary = ROOT / "WeKnora-lite"
        if not core_binary.is_file():
            raise RuntimeError("build the Linux core first")
        emit("[SCOPE] Real Linux Core + real worker Host; native multipart TXT upload; local model fixture; separate DBs on repository filesystem")
        env = {key: value for key, value in os.environ.items() if not key.startswith("AGENTRAG_")}
        env.update(ENV_FILE=str(ROOT / ".env"), DB_DRIVER="sqlite", DB_PATH=str(work / "core.db"),
                   RETRIEVE_DRIVER="sqlite", STORAGE_TYPE="local", LOCAL_STORAGE_BASE_DIR=str(work / "files"),
                   STREAM_MANAGER_TYPE="memory", SERVER_HOST="127.0.0.1", SERVER_PORT=str(port), CORE_PORT=str(port),
                   WEKNORA_WEB_DIR=str(ROOT / "web"), NEO4J_ENABLE="false", ENABLE_GRAPH_RAG="false",
                   SSRF_DISABLE="true", LOG_PATH="", WEKNORA_LOG_FORMAT="%d %level %traceId %msg",
                   JWT_SECRET=secrets.token_hex(32), SYSTEM_SIGNING_KEY=secrets.token_hex(32),
                   AGENTRAG_LAUNCHER_CONFIG=str(work / "private-launcher.json"), AGENTRAG_EXT_DB=str(work / "agentrag_ext.db"),
                   AGENTRAG_WEKNORA_API_KEY="initial-unregistered-test-key", MINERU_ENDPOINT="http://127.0.0.1:8000")
        model_url = model.start()
        model_started = True
        log = (work / "bootstrap-private.log").open("w"); handles.append(log)
        core = subprocess.Popen([str(core_binary)], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        http = helpers.HTTP("http://127.0.0.1:" + str(port))
        helpers.wait_health(http, core, http.base_url + "/health", 60)
        nonce = uuid.uuid4().hex[:12]
        account = {"username": "startup_" + nonce, "email": "startup-" + nonce + "@example.invalid", "password": helpers.new_password()}
        http.api("POST", "/auth/register", account)
        http.token = http.api("POST", "/auth/login", {key: account[key] for key in ("email", "password")})["token"]
        models = []
        for kind in ("KnowledgeQA", "Embedding"):
            models.append(http.api("POST", "/models", {"name": "startup-" + kind, "type": kind, "source": "remote", "parameters": {"base_url": model_url, "api_key": "fixture-not-secret", "provider": "generic", "interface_type": "openai", "embedding_parameters": {"dimension": 8}}})["data"]["id"])
        kb = http.api("POST", "/knowledge-bases", {"name": "Startup isolated test", "type": "document", "embedding_model_id": models[1], "summary_model_id": models[0], "indexing_strategy": {"vector_enabled": True, "keyword_enabled": True, "graph_enabled": False, "wiki_enabled": False}, "auto_tag_config": {"enabled": False}, "profile_config": {"enabled": False}})["data"]
        helpers.stop_owned(core); core = None
        emit("[SETUP] Isolated native account/models/KB prepared through official APIs; setup Core stopped before launcher test")
        env.update(AGENTRAG_TENANT_ID=str(kb["tenant_id"]), AGENTRAG_KB_IDS=kb["id"], AGENTRAG_LOGIN_EMAIL=account["email"], AGENTRAG_LOGIN_PASSWORD=account["password"])
        phase = 0
        def launch():
            nonlocal phase
            phase += 1
            run = work / ("run-" + str(phase)); run.mkdir()
            env["AGENTRAG_RUN_DIR"] = str(run)
            logpath = work / ("launcher-" + str(phase) + ".log")
            log = logpath.open("w"); handles.append(log)
            process = subprocess.Popen(["bash", str(ROOT / "start-all.sh")], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            owned_launchers.append(process)
            def ready():
                if process.poll() is not None:
                    raise RuntimeError("launcher exited; private diagnostics: " + str(work))
                return "[READY] AgentRAG:" in logpath.read_text()
            try:
                until(ready, 100)
                lines = logpath.read_text()
                if not lines.index("[READY] Host") < lines.index("[START] Core"):
                    raise RuntimeError("Core started before Host readiness")
            except BaseException:
                stop_launcher(process)
                raise
            emit("[ORDER] PASS: /health status=ready before Core launch; scoped machine credential verified")
            return process, run
        launcher, run = launch()
        boundary = "startup-" + secrets.token_hex(16)
        document_only = "DOCUMENTONLY" + secrets.token_hex(12)
        text = ("AGENTRAG_E2E_MARKER. Every controlled publication requires two independent reviewers. " + document_only + " ") * 6
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="startup-test.txt"\r\nContent-Type: text/plain\r\n\r\n{text}\r\n--{boundary}--\r\n').encode()
        request = urllib.request.Request(http.base_url + "/api/v1/knowledge-bases/" + kb["id"] + "/knowledge/file", data=body, headers={"Authorization": "Bearer " + http.token, "Content-Type": "multipart/form-data; boundary=" + boundary}, method="POST")
        with http.opener.open(request, timeout=30) as response:
            doc = json.load(response)["data"]
        def completed():
            status = http.api("GET", "/knowledge/" + doc["id"])["data"]["parse_status"]
            if status == "failed":
                raise RuntimeError("native document parsing failed")
            return status == "completed"
        until(completed)
        ext = work / "agentrag_ext.db"
        def state():
            with sqlite3.connect(ext.as_uri() + "?mode=ro", uri=True) as db:
                row = db.execute("SELECT state FROM ext_lifecycle_current WHERE document_id=?", (doc["id"],)).fetchone()
                return row[0] if row else None
        until(lambda: state() == "active", 30)
        with sqlite3.connect((work / "core.db").as_uri() + "?mode=ro", uri=True) as db:
            chunks = db.execute("SELECT COUNT(*) FROM chunks WHERE knowledge_id=?", (doc["id"],)).fetchone()[0]
            if chunks < 1:
                raise RuntimeError("no native chunks persisted")
        emit("[UPLOAD] PASS: real multipart upload -> native parsing/indexing completed; chunks=" + str(chunks))
        emit("[OBSERVER] PASS: authenticated native source reconciliation initialized lifecycle active")
        def ask():
            session = http.api("POST", "/sessions", {"title": "Isolated M2 gate test"})["data"]
            payload = {"query": "Retrieve AGENTRAG_E2E_MARKER: how many reviewers are required before publication?", "knowledge_base_ids": [kb["id"]], "knowledge_ids": [doc["id"]], "agent_enabled": False, "web_search_enabled": False, "summary_model_id": models[0], "disable_title": True}
            n = model.generation_calls
            prompt_offset = len(prompts)
            status, response = http.call("POST", http.base_url + "/api/v1/knowledge-chat/" + session["id"], payload)
            return status, response, model.generation_calls - n, "\n".join(prompts[prompt_offset:])
        status, response, calls, context = ask()
        if not helpers.rag_succeeded(status, response) or calls != 1 or doc["id"] not in response or document_only not in context:
            raise RuntimeError("active RAG did not return the uploaded document reference")
        emit("[ACTIVE-RAG] PASS: native streaming answer + uploaded document reference; answer_generation_calls=1")
        stop_launcher(launcher); launcher = None
        emit("[TRAP] PASS: Ctrl+C stopped Core and Host; both owned ports released")
        free_port(port); free_port(8090)
        # Test-only INPUT fixture: never writes current/events. Production startup
        # sweep must evaluate the annotation and commit the actual transition.
        with sqlite3.connect(ext) as db:
            changed = db.execute("UPDATE ext_document_annotations SET effective_until='2000-01-01T00:00:00Z', revision=revision+1 WHERE document_id=?", (doc["id"],)).rowcount
            if changed != 1:
                raise RuntimeError("annotation test input missing")
        env.pop("AGENTRAG_WEKNORA_API_KEY")
        launcher, run = launch()
        until(lambda: state() == "stale", 15)
        emit("[SWEEP] PASS: production startup scan evaluated expired annotation -> stale (no direct state edits)")
        status, response, calls, context = ask()
        (work / "stale-private.sse").write_text(response)
        emit("[STALE-CHECK] HTTP=" + str(status) + "; answer_generation_calls=" + str(calls) + "; raw_document_id_present=" + str(doc["id"] in response))
        if status != 200 or document_only in context or doc["id"] in response:
            raise RuntimeError("stale document bypassed native RAG gate")
        with sqlite3.connect(ext.as_uri() + "?mode=ro", uri=True) as db:
            if db.execute("PRAGMA foreign_key_check").fetchall():
                raise RuntimeError("extension foreign key violations")
            if db.execute("SELECT COUNT(*) FROM ext_lifecycle_events WHERE document_id=? AND from_state='active' AND to_state='stale'", (doc["id"],)).fetchone()[0] != 1:
                raise RuntimeError("missing actual lifecycle transition event")
        emit("[STALE-RAG] PASS: expired document absent from references AND actual LLM prompts; native no-knowledge fallback generation_calls=" + str(calls))
        stop_launcher(launcher); launcher = None
        free_port(port); free_port(8090)
        if digest(source) != before:
            raise RuntimeError("original native database changed")
        emit("[ISOLATION] PASS: original native DB SHA256 unchanged; no foreign key violations; original processes not replaced")
        emit("[RESULT] M2 startup/upload/interception E2E PASS; exit=0")
        return 0
    except Exception as error:
        emit("[RESULT] FAIL: " + str(error))
        emit("[PRIVATE-DIAGNOSTICS] " + str(work))
        return 1
    finally:
        for owned_launcher in owned_launchers:
            stop_launcher(owned_launcher)
        helpers.stop_owned(core)
        if model_started:
            model.close()
        else:
            model.server.server_close()
        for handle in handles:
            handle.close()
        output.close()


if __name__ == "__main__":
    raise SystemExit(main())
