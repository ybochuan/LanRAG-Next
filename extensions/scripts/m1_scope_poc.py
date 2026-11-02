"""Run the opt-in V1 scope POC against an owned MinerU 4.0.1 Flash server.

No production Host, native database, user documents, models or GPU are used.
The helper is launched from the existing venv with bytecode writes disabled.
"""

import argparse
from datetime import datetime
import os
from pathlib import Path
import secrets
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import urllib.request
from zoneinfo import ZoneInfo


BACKEND = r'''
import ipaddress, json, os, socket, sys
from pathlib import Path

def network_guard(event, args):
    host = None
    if event == "socket.getaddrinfo":
        host = args[0]
    elif event == "socket.connect" and args[0].family in (socket.AF_INET, socket.AF_INET6):
        host = args[1][0]
    else:
        return
    if host is None or host in ("", "localhost"):
        return
    try:
        if ipaddress.ip_address(host).is_loopback:
            return
    except ValueError:
        pass
    # Record only a count marker, never a host, URL or credential.
    with Path(sys.argv[3]).open("a", encoding="utf-8") as marker:
        marker.write("blocked\n")
    raise PermissionError("external network disabled for preflight proof")

sys.addaudithook(network_guard)
import uvicorn
from mineru.parser.api_server import create_app

sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.bind(("127.0.0.1", 0))
sock.listen(128)
app = create_app(upload_dir=sys.argv[1], tier="flash", concurrency=1,
                 image_analysis=False, preload_models=False,
                 api_key=os.environ["AGENTRAG_M1_POC_BACKEND_KEY"])
Path(sys.argv[2]).write_text(json.dumps({"port": sock.getsockname()[1]}), encoding="utf-8")
uvicorn.Server(uvicorn.Config(app, log_level="warning")).run(sockets=[sock])
'''


def stop_owned(process):
    if process is None or process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def cleanup_private(private):
    # Windows scanners may briefly retain a handle after the owned process exits.
    # Retry only this TemporaryDirectory, never broaden to neighboring paths.
    target = Path(private.name).resolve()
    if target.parent != Path(tempfile.gettempdir()).resolve() or not target.name.startswith("agentrag-m1-scope-"):
        raise OSError("refusing unexpected temporary directory")
    for attempt in range(6):
        try:
            private.cleanup()
            return
        except (PermissionError, NotADirectoryError):
            if attempt == 5:
                raise
            time.sleep(0.1 * (2 ** attempt))


def stop_all_owned(processes, emit):
    stopped = True
    for owned in processes:
        try:
            stop_owned(owned)
        except (OSError, subprocess.TimeoutExpired):
            emit("[CLEANUP] FAIL: owned process exit could not be confirmed")
            stopped = False
    if stopped:
        emit("[CLEANUP] Owned processes stopped")
    return stopped


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--mineru-python", type=Path, required=True)
    root = Path(__file__).resolve().parents[2]
    parser.add_argument("--report", type=Path, default=root / "docs/evidence/m1-preflight-2026-10-31.md")
    args = parser.parse_args(argv)
    lines = []
    backend = tests = private = backend_log = network_attempts = None
    backend_key = secrets.token_urlsafe(32)
    exit_code = 1

    def emit(message):
        message = str(message).replace(backend_key, "[REDACTED]")
        lines.append(message)
        print(message, flush=True)

    def interrupted(_signum, _frame):
        raise KeyboardInterrupt

    previous = {}
    for name in ("SIGINT", "SIGTERM"):
        signum = getattr(signal, name, None)
        if signum is not None:
            previous[signum] = signal.signal(signum, interrupted)
    try:
        emit("[RUN] " + datetime.now(ZoneInfo("Asia/Shanghai")).strftime("%Y-%m-%d %H:%M:%S Asia/Shanghai"))
        emit("[SCOPE] Real MinerU4.0.1; PDF preflight/scoped V1/artifact POC; native upload API and production Host are not exercised")
        if not args.mineru_python.is_file():
            raise RuntimeError("MinerU interpreter unavailable")
        environment = dict(os.environ)
        environment.update({"PYTHONDONTWRITEBYTECODE": "1", "PYTHONNOUSERSITE": "1"})
        version = subprocess.run(
            [str(args.mineru_python), "-B", "-c", "import importlib.metadata as m; print(m.version('mineru'))"],
            env=environment, capture_output=True, text=True, timeout=15,
        )
        if version.returncode != 0 or version.stdout.strip() != "4.0.1":
            raise RuntimeError("MinerU version must be exactly 4.0.1")
        go = shutil.which("go")
        if not go:
            raise RuntimeError("Go toolchain unavailable")
        emit("[VERSION] Existing interpreter verified: MinerU4.0.1; no install or upgrade")
        private = tempfile.TemporaryDirectory(prefix="agentrag-m1-scope-")
        work = Path(private.name)
        if os.name != "nt":
            os.chmod(work, 0o700)
        environment.update({
            "MINERU_HOME": str(work / "mineru-home"),
            "MINERU_MODEL_VLM_SERVER_URL": "",
            "MINERU_MODEL_VLM_API_KEY": "",
            "MINERU_MODEL_SOURCE": "local",
            "HF_HUB_OFFLINE": "1",
            "TRANSFORMERS_OFFLINE": "1",
            "AGENTRAG_M1_POC_BACKEND_KEY": backend_key,
            "GOWORK": "off",
            "HTTP_PROXY": "", "HTTPS_PROXY": "", "ALL_PROXY": "",
            "http_proxy": "", "https_proxy": "", "all_proxy": "",
            "NO_PROXY": "*", "no_proxy": "*",
        })
        ready = work / "ready.json"
        network_attempts = work / "network-attempts"
        backend_log = (work / "backend-private.log").open("wb")
        backend = subprocess.Popen(
            [str(args.mineru_python.resolve()), "-B", "-c", BACKEND, str(work / "uploads"), str(ready), str(network_attempts)],
            cwd=work, env=environment, stdin=subprocess.DEVNULL,
            stdout=backend_log, stderr=subprocess.STDOUT,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
        )
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        deadline = time.monotonic() + 75
        upstream = None
        while time.monotonic() < deadline:
            if backend.poll() is not None:
                raise RuntimeError("owned Flash API exited before readiness; private backend log is not published")
            if ready.exists():
                import json
                port = json.loads(ready.read_text(encoding="utf-8"))["port"]
                upstream = f"http://127.0.0.1:{int(port)}"
                try:
                    with opener.open(upstream + "/v1/health", timeout=1) as response:
                        if response.status == 200:
                            break
                except OSError:
                    pass
            time.sleep(0.1)
        else:
            raise RuntimeError("owned Flash API readiness timed out")
        emit("[BACKEND] Owned random IPv4 loopback port; Flash only, model preloading disabled, private home/uploads")
        emit("[MODE] Caller auto/ocr+advanced; real PDF preflight must force flash/txt; Python DNS/TCP calls are guarded")
        binary = work / ("parser-poc.exe" if os.name == "nt" else "parser-poc")
        built = subprocess.run(
            [go, "test", "-mod=readonly", "-c", "-o", str(binary), "./parser"],
            cwd=root / "extensions", env=environment, capture_output=True, text=True, timeout=90,
        )
        if built.returncode != 0:
            emit(built.stdout + built.stderr)
            raise RuntimeError("POC test binary build failed")
        environment["AGENTRAG_M1_POC_UPSTREAM"] = upstream
        tests = subprocess.Popen(
            [str(binary), "-test.run=^TestScopeProxyRealMinerU401$", "-test.v", "-test.timeout=75s"],
            env=environment, cwd=root / "extensions", stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT, text=True,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
        )
        output, _ = tests.communicate(timeout=85)
        emit(output.rstrip())
        if tests.returncode != 0:
            raise RuntimeError("real scope/protocol assertions failed")
        emit("[PROTOCOL] PASS: cold/hot byte proof, PDF measurements, forced flash/txt, cross-scope refusal, one job per scope and document-bound middle.json")
        exit_code = 0
    except KeyboardInterrupt:
        emit("[RESULT] FAIL: interrupted")
    except subprocess.TimeoutExpired:
        emit("[RESULT] FAIL: owned command timed out")
    except (OSError, RuntimeError, ValueError) as error:
        emit("[RESULT] FAIL: " + str(error))
    finally:
        if not stop_all_owned((tests, backend), emit):
            exit_code = 1
        if backend_log:
            backend_log.close()
        if private:
            if exit_code == 0:
                weights = [p for p in Path(private.name).rglob("*") if p.suffix.lower() == ".onnx"]
                if network_attempts.exists() or weights:
                    emit("[RESOURCES] FAIL: audited Python external connection attempted or private ONNX weights created")
                    exit_code = 1
                else:
                    emit("[RESOURCES] PASS: zero audited Python external DNS/TCP attempts and zero ONNX weights in private work directory")
            try:
                cleanup_private(private)
            except OSError:
                emit("[CLEANUP] FAIL: owned temporary directory remains; no neighboring files removed")
                exit_code = 1
        emit("[STATE] Native DB, venv and user model configuration unchanged")
        emit("[LIMIT] Synthetic doc_id bindings only; no native upload/KB authorization, automatic native doc binding, artifact DB transaction, restart recovery, corpus calibration, actual OCR or full M1 acceptance")
        emit("[RESULT] " + ("PASS" if exit_code == 0 else "FAIL"))
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text("# M1-03 PDF preflight / MinerU4.0.1\n\n```text\n" + "\n".join(lines) + "\n```\n", encoding="utf-8")
        for signum, handler in previous.items():
            signal.signal(signum, handler)
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
