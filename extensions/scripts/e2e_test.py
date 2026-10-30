"""Real core + real Host E2E, isolated DB, public auth, local model fixture.

Never replaces the Bridge with a 200 stub. A reachable Host returning 503 is a
failed online gate, even when the core has already opened an HTTP 200 SSE stream.
"""

import argparse
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import mmap
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import socket
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
import uuid
from zoneinfo import ZoneInfo

GATE = "[Bridge] before_retrieve failed, blocking search to prevent policy bypass"
RECEIPT = "bridge_before_retrieve_received"
ANSWER = "AGENTRAG_E2E_ANSWER"


def new_password():
    return secrets.token_urlsafe(18) + "Aa1!"


def sse_records(body):
    records = []
    event = ""
    for line in body.splitlines():
        if line.startswith("event:"):
            event = line[6:].strip()
        elif line.startswith("data:"):
            try:
                data = json.loads(line[5:].strip())
            except ValueError:
                continue
            if isinstance(data, dict):
                records.append((event, data))
        elif not line.strip():
            event = ""
    return records


def rag_succeeded(status, body):
    records = sse_records(body)
    kinds = [str(d.get("response_type", d.get("type", event))) for event, d in records]
    failed = any(k in ("error", "failed") or bool(d.get("error")) for k, (_, d) in zip(kinds, records))
    terminal = any(k in ("complete", "done") or d.get("is_completed") is True for k, (_, d) in zip(kinds, records))
    answered = any(ANSWER in json.dumps(d) for k, (_, d) in zip(kinds, records) if k in ("answer", "response", "message", "delta"))
    return 200 <= status < 300 and terminal and answered and not failed


def gate_logged(segment, request_id):
    return any(GATE in line and request_id in line for line in segment.splitlines())


def phase_log(path, offset):
    with path.open("rb") as source:
        source.seek(offset)
        return source.read().decode("utf-8", errors="replace")


class HTTP:
    def __init__(self):
        self.token = ""
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def call(self, method, url, payload=None, request_id=None, timeout=45):
        headers = {"Content-Type": "application/json"}
        if self.token and url.startswith("http://127.0.0.1:8080/api/v1/"):
            headers["Authorization"] = "Bearer " + self.token
        if request_id:
            headers["X-Request-ID"] = request_id
        req = urllib.request.Request(url, data=None if payload is None else json.dumps(payload).encode(), headers=headers, method=method)
        try:
            response = self.opener.open(req, timeout=timeout)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, response.read(4 << 20).decode("utf-8", errors="replace")

    def api(self, method, route, payload=None):
        status, body = self.call(method, "http://127.0.0.1:8080/api/v1" + route, payload)
        try:
            data = json.loads(body)
        except ValueError:
            raise RuntimeError(f"{method} {route}: non-JSON HTTP {status}") from None
        if status not in (200, 201) or data.get("success") is False:
            # Do not include authentication response bodies, passwords or tokens.
            raise RuntimeError(f"{method} {route}: HTTP {status}, request rejected")
        return data


class ModelFixture:
    def __init__(self):
        self.generation_calls = 0
        fixture = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                request = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))))
                if self.path.endswith("/embeddings"):
                    inputs = request.get("input", [])
                    if not isinstance(inputs, list):
                        inputs = [inputs]
                    data = {"object": "list", "data": [{"object": "embedding", "index": i, "embedding": [0.1] * 8} for i in range(len(inputs))], "model": "e2e-embedding", "usage": {"prompt_tokens": 1, "total_tokens": 1}}
                else:
                    prompts = json.dumps(request.get("messages", []))
                    understanding = "rewrite_query" in prompts or "rewritten_query" in prompts
                    content = json.dumps({"rewrite_query": "AGENTRAG_E2E_MARKER", "intent": "kb_search", "entities": []}) if understanding else ANSWER
                    if not understanding:
                        fixture.generation_calls += 1
                    if request.get("stream"):
                        self.send_response(200)
                        self.send_header("Content-Type", "text/event-stream")
                        self.end_headers()
                        for delta, finish in (({"role": "assistant", "content": content}, None), ({}, "stop")):
                            chunk = {"id": "e2e-chat", "object": "chat.completion.chunk", "created": 1, "model": "e2e-chat", "choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}
                            self.wfile.write(("data: " + json.dumps(chunk) + "\n\n").encode())
                        self.wfile.write(b"data: [DONE]\n\n")
                        return
                    data = {"id": "e2e-chat", "object": "chat.completion", "created": 1, "model": "e2e-chat", "choices": [{"index": 0, "message": {"role": "assistant", "content": content}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}}
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps(data).encode())

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def start(self):
        self.thread.start()
        return f"http://127.0.0.1:{self.server.server_port}/v1"

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()


def free_port(port):
    with socket.socket() as sock:
        try:
            sock.bind(("127.0.0.1", port))
        except OSError:
            raise RuntimeError(f"port {port} is already owned; refusing to stop or replace its process") from None


def stop_owned(process):
    if process is None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        return
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        # Only a process group created by this script, never a port's unknown owner.
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait(timeout=5)


def wait_health(http, process, url, limit):
    deadline = time.monotonic() + limit
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f"owned service exited before health check, code={process.returncode}")
        try:
            status, _ = http.call("GET", url, timeout=1)
            if status == 200:
                return
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(0.25)
    raise RuntimeError("service health deadline exceeded")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    root = Path(__file__).resolve().parents[2]
    parser.add_argument("--core", type=Path, default=root / "WeKnora-lite")
    parser.add_argument("--source-db", type=Path, default=root / "data/weknora.db")
    parser.add_argument("--report", type=Path)
    args = parser.parse_args(argv)
    stamp = datetime.now(ZoneInfo("Asia/Shanghai")).strftime("%Y%m%d-%H%M%S")
    report = args.report or root / "docs/evidence" / f"m0-e2e-{stamp}.log"
    report.parent.mkdir(parents=True, exist_ok=True)
    offline_ok = online_ok = False
    core = host = None
    model = None
    private = None
    handles = []
    log_file = report.open("w", encoding="utf-8")
    prior_sigterm = None

    def terminated(_signal, _frame):
        raise InterruptedError("E2E termination requested")

    def emit(message):
        print(message, flush=True)
        log_file.write(message + "\n")
        log_file.flush()

    try:
        if sys.platform != "linux":
            raise RuntimeError("run this script inside WSL/Linux, alongside the Linux core")
        prior_sigterm = signal.signal(signal.SIGTERM, terminated)
        emit(f"[RUN] M0 E2E {stamp} Asia/Shanghai")
        emit("[SCOPE] Real core + real Extension Host; local model fixture; isolated DB backup; no Bridge 200 stub")
        free_port(8080)
        free_port(8090)
        if not args.core.is_file() or not os.access(args.core, os.X_OK):
            raise RuntimeError("compiled core binary is unavailable")
        with args.core.open("rb") as source, mmap.mmap(source.fileno(), 0, access=mmap.ACCESS_READ) as binary:
            if binary.find(GATE.encode()) < 0:
                raise RuntimeError("core binary does not contain the expected Bridge gate")
        go = shutil.which("go") or "/usr/local/go/bin/go"
        if not Path(go).is_file():
            raise RuntimeError("Linux Go toolchain unavailable")
        emit("[PREFLIGHT] Gate text present in compiled core; 8080 and 8090 unoccupied")
        private = tempfile.TemporaryDirectory(prefix="agentrag-e2e-")
        work = Path(private.name)
        os.chmod(work, 0o700)
        source = sqlite3.connect(args.source_db.resolve().as_uri() + "?mode=ro", uri=True)
        destination = sqlite3.connect(work / "core.db")
        with source, destination:
            source.backup(destination)
        source.close()
        destination.close()
        emit("[DB] Consistent SQLite backup created; original database will not be written")
        model = ModelFixture()
        model_url = model.start()
        core_log = work / "core.log"
        host_log = work / "host.log"
        core_output = core_log.open("w", encoding="utf-8")
        handles.append(core_output)
        environment = dict(os.environ)
        environment.update({"ENV_FILE": str(root / ".env"), "DB_DRIVER": "sqlite", "DB_PATH": str(work / "core.db"), "RETRIEVE_DRIVER": "sqlite", "STREAM_MANAGER_TYPE": "memory", "STORAGE_TYPE": "local", "LOCAL_STORAGE_BASE_DIR": str(work / "files"), "WEKNORA_WEB_DIR": str(root / "web"), "SSRF_WHITELIST_EXTRA": "127.0.0.1", "LOG_PATH": "", "WEKNORA_LOG_FORMAT": "%d %level %traceId %msg", "WEKNORA_TENANT_ENABLE_RBAC": "false"})
        core = subprocess.Popen([str(args.core.resolve())], cwd=root, env=environment, stdout=core_output, stderr=subprocess.STDOUT, start_new_session=True)
        http = HTTP()
        wait_health(http, core, "http://127.0.0.1:8080/health", 35)
        status, _ = http.call("GET", "http://127.0.0.1:8080/")
        emit(f"[UI] GET / HTTP {status} with explicit WEKNORA_WEB_DIR; API authentication remains enabled")
        status, _ = http.call("GET", "http://127.0.0.1:8080/api/v1/auth/me")
        emit(f"[AUTH] Anonymous /auth/me HTTP {status} (expected 401; RBAC=false does not disable authentication)")
        status, _ = http.call("POST", "http://127.0.0.1:8080/api/v1/auth/auto-setup", {})
        emit(f"[AUTH] Anonymous auto-setup HTTP {status} (standard edition rejects it; Lite requires a desktop capability)")
        nonce = uuid.uuid4().hex[:12]
        credentials = {"username": "agentrag_e2e_" + nonce, "email": f"agentrag-e2e-{nonce}@example.invalid", "password": new_password()}
        http.api("POST", "/auth/register", credentials)
        login = http.api("POST", "/auth/login", {k: credentials[k] for k in ("email", "password")})
        http.token = login.get("token", "")
        if not http.token:
            raise RuntimeError("login returned no access token")
        http.api("GET", "/auth/me")
        emit("[AUTH] Public registration 201; login 200; authenticated /auth/me 200; credential=[REDACTED]")
        model_ids = []
        for name, kind in (("e2e-chat", "KnowledgeQA"), ("e2e-embedding", "Embedding")):
            payload = {"name": name, "type": kind, "source": "remote", "parameters": {"base_url": model_url, "api_key": "e2e-fixture-not-a-secret", "interface_type": "openai", "provider": "generic", "embedding_parameters": {"dimension": 8}}}
            model_ids.append(http.api("POST", "/models", payload)["data"]["id"])
        kb = http.api("POST", "/knowledge-bases", {"name": "AgentRAG E2E isolated fixture", "type": "document", "embedding_model_id": model_ids[1], "summary_model_id": model_ids[0], "indexing_strategy": {"vector_enabled": True, "keyword_enabled": True, "graph_enabled": False, "wiki_enabled": False}, "auto_tag_config": {"enabled": False}, "profile_config": {"enabled": False}})["data"]
        document = http.api("POST", f"/knowledge-bases/{kb['id']}/knowledge/manual", {"title": "AgentRAG E2E marker", "content": "AGENTRAG_E2E_MARKER. This isolated test document requires two reviewers before publication.", "status": "publish", "tag_ids": []})["data"]
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            document = http.api("GET", "/knowledge/" + document["id"])["data"]
            if document.get("parse_status") == "completed":
                break
            if document.get("parse_status") == "failed":
                raise RuntimeError("native fixture ingestion failed")
            time.sleep(0.25)
        else:
            raise RuntimeError("native fixture ingestion deadline exceeded")
        session = http.api("POST", "/sessions", {"title": "AgentRAG E2E offline and online"})["data"]
        payload = {"query": "Retrieve AGENTRAG_E2E_MARKER: how many reviewers are required before publication?", "knowledge_base_ids": [kb["id"]], "knowledge_ids": [document["id"]], "agent_enabled": False, "web_search_enabled": False, "summary_model_id": model_ids[0], "disable_title": True}
        emit("[FIXTURE] Native APIs created chat/embedding models, KB, completed document and session in isolated DB")
        emit("[MODEL] Deterministic local OpenAI-compatible fixture; no external model credentials or charges")

        def ask(phase):
            request_id = "e2e-" + nonce + "-" + phase
            offset = core_log.stat().st_size
            generations = model.generation_calls
            started = time.monotonic()
            status, body = http.call("POST", "http://127.0.0.1:8080/api/v1/knowledge-chat/" + session["id"], payload, request_id=request_id)
            time.sleep(0.15)
            segment = phase_log(core_log, offset)
            records = sse_records(body)
            kinds = sorted({str(d.get("response_type", d.get("type", event))) for event, d in records})
            emit(f"[{phase.upper()}] RAG HTTP {status}; SSE kinds={json.dumps(kinds)}; duration={time.monotonic()-started:.3f}s; generation_calls={model.generation_calls-generations}")
            for line in segment.splitlines():
                if GATE in line or (request_id in line and ("before_retrieve" in line or "stage_failed" in line)):
                    emit("[CORE] " + re.sub(r"\x1b\[[0-9;]*m", "", line))
            return status, body, segment, request_id, model.generation_calls - generations

        free_port(8090)
        status, body, segment, request_id, generations = ask("offline")
        offline_ok = gate_logged(segment, request_id) and not rag_succeeded(status, body) and generations == 0
        emit("[OFFLINE] " + ("PASS: correlated Bridge refusal; no answer generation" if offline_ok else "FAIL: missing correlated gate evidence or generation escaped"))

        host_output = host_log.open("w", encoding="utf-8")
        handles.append(host_output)
        host_environment = {k: v for k, v in os.environ.items() if not k.startswith("AGENTRAG_")}
        host_environment.update({"AGENTRAG_MODE": "probe", "GOWORK": "off"})
        host = subprocess.Popen([go, "run", "-mod=readonly", "./cmd/extension-host"], cwd=root / "extensions", env=host_environment, stdout=host_output, stderr=subprocess.STDOUT, start_new_session=True)
        wait_health(http, host, "http://127.0.0.1:8090/healthz", 180)
        emit("[HOST] Started actual go run ./cmd/extension-host; healthz 200; no 200 Bridge stub")
        receipt_offset = host_log.stat().st_size
        status, body, segment, request_id, generations = ask("online")
        received = RECEIPT in phase_log(host_log, receipt_offset)
        for line in host_log.read_text(errors="replace").splitlines():
            if RECEIPT in line or "host_probe_started" in line:
                emit("[HOST] " + line)
        emit(f"[ONLINE] Host receipt verified={received}; same JWT/session/query/KB/document payload reused")
        online_ok = received and rag_succeeded(status, body) and generations > 0 and not gate_logged(segment, request_id)
        emit("[ONLINE] " + ("PASS: terminal generated answer returned" if online_ok else "FAIL: Host reachable but normal RAG did not complete; a 503 business Hook is not ready"))
    except Exception as error:
        emit(f"[ERROR] {type(error).__name__}: {error}")
    finally:
        stop_owned(host)
        stop_owned(core)
        if model is not None:
            model.close()
        for handle in handles:
            handle.close()
        if private is not None:
            private.cleanup()
        emit("[CLEANUP] Owned processes stopped; isolated DB/credentials removed; original DB preserved")
        emit(f"[RESULT] AUTH/RAG offline={offline_ok}; online={online_ok}; M0={'PASS' if offline_ok and online_ok else 'NOT_ACCEPTED'}")
        emit("[SCOPE] User-approved M0 acceptance: native HTTP gate + default pass-through; governance and accepted race follow-ups remain in backlog")
        emit(f"[REPORT] {report}")
        log_file.close()
        if prior_sigterm is not None:
            signal.signal(signal.SIGTERM, prior_sigterm)
    return 0 if offline_ok and online_ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
