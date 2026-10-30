"""Regression gates: an SSE HTTP 200 or a started Host is never E2E success."""

import importlib.util
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import sys
import tempfile
import time
import unittest

spec = importlib.util.spec_from_file_location("agentrag_e2e", Path(__file__).with_name("e2e_test.py"))
e2e = importlib.util.module_from_spec(spec)
spec.loader.exec_module(e2e)


class E2EAcceptanceTests(unittest.TestCase):
    def test_log_byte_offset_preserves_evidence_after_chinese_prefix(self):
        with tempfile.TemporaryDirectory(prefix="e2e-log-test-") as temporary:
            path = Path(temporary) / "core.log"
            path.write_bytes(("已完成旧请求\n" * 10).encode())
            offset = path.stat().st_size
            expected = "this-request " + e2e.GATE + "\n"
            with path.open("ab") as output:
                output.write(expected.encode())
            self.assertEqual(e2e.phase_log(path, offset), expected)
            self.assertTrue(e2e.gate_logged(e2e.phase_log(path, offset), "this-request"))

    @unittest.skipUnless(sys.platform == "linux", "WSL/Linux process-group lifecycle")
    def test_sigterm_cleans_actual_main_owned_child_and_private_copy(self):
        try:
            e2e.free_port(8080)
            e2e.free_port(8090)
        except RuntimeError:
            self.skipTest("8080/8090 already owned; do not touch their owner")
        with tempfile.TemporaryDirectory(prefix="e2e-signal-test-") as temporary:
            directory = Path(temporary).resolve()
            source = directory / "source.db"
            sqlite3.connect(source).close()
            marker = directory / "owned.json"
            core = directory / "owned-core"
            core.write_text("#!/usr/bin/env python3\n# " + e2e.GATE + "\nimport json,os,pathlib,time\npathlib.Path(os.environ['E2E_OWNED_MARKER']).write_text(json.dumps({'pid':os.getpid(),'work':str(pathlib.Path(os.environ['DB_PATH']).parent)}))\ntime.sleep(60)\n")
            core.chmod(0o700)
            environment = {**os.environ, "TMPDIR": str(directory), "E2E_OWNED_MARKER": str(marker)}
            process = subprocess.Popen([sys.executable, str(Path(e2e.__file__)), "--core", str(core), "--source-db", str(source), "--report", str(directory / "report.log")], env=environment, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
            owned = None
            try:
                deadline = time.monotonic() + 10
                while not marker.exists() and process.poll() is None and time.monotonic() < deadline:
                    time.sleep(0.025)
                if not marker.exists():
                    process.terminate()
                    output, _ = process.communicate(timeout=10)
                    self.fail("main never started its owned test child: " + output)
                owned = json.loads(marker.read_text())
                work = Path(owned["work"])
                self.assertTrue(work.is_relative_to(directory))
                process.send_signal(signal.SIGTERM)
                output, _ = process.communicate(timeout=10)
                self.assertIn("[CLEANUP] Owned processes stopped", output)
                self.assertNotEqual(process.returncode, 0)
                self.assertFalse(work.exists())
                self.assertFalse(Path(f"/proc/{owned['pid']}").exists())
            finally:
                if process.poll() is None:
                    process.kill()
                    process.communicate(timeout=5)
                if owned is not None:
                    # Clean up even the RED run; signal only our identifiable fake
                    # core group, never a process discovered merely by port/PID.
                    command = Path(f"/proc/{owned['pid']}/cmdline")
                    try:
                        if str(core).encode() in command.read_bytes().split(b"\0"):
                            os.killpg(owned["pid"], signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    except FileNotFoundError:
                        pass

    def test_jwt_is_only_sent_to_core_api_not_host_health(self):
        class Response:
            status = 200
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def read(self, *args): return b"{}"

        class Recorder:
            def __init__(self): self.requests = []
            def open(self, request, timeout):
                self.requests.append(request)
                return Response()

        http = e2e.HTTP()
        http.token = "test-only-token"
        http.opener = Recorder()
        http.call("GET", "http://127.0.0.1:8080/api/v1/auth/me")
        http.call("GET", "http://127.0.0.1:8090/healthz")
        self.assertEqual(http.opener.requests[0].get_header("Authorization"), "Bearer test-only-token")
        self.assertIsNone(http.opener.requests[1].get_header("Authorization"))

    def test_generated_password_satisfies_native_eight_to_thirty_two_policy(self):
        password = e2e.new_password()
        self.assertGreaterEqual(len(password), 8)
        self.assertLessEqual(len(password), 32)
        self.assertTrue(any(c.isupper() for c in password))
        self.assertTrue(any(c.islower() for c in password))
        self.assertTrue(any(c.isdigit() for c in password))
        self.assertIn("!", password)

    def test_http_200_sse_error_is_failure(self):
        body = 'event: error\ndata: {"response_type":"error","content":"bridge unavailable"}\n\n'
        self.assertFalse(e2e.rag_succeeded(200, body))

    def test_empty_complete_is_not_a_generated_answer(self):
        self.assertFalse(e2e.rag_succeeded(200, 'data: {"response_type":"complete"}\n\n'))

    def test_success_needs_real_answer_and_terminal_event(self):
        body = 'data: {"response_type":"answer","content":"AGENTRAG_E2E_ANSWER"}\n\n'
        self.assertFalse(e2e.rag_succeeded(200, body))
        body += 'data: {"response_type":"complete"}\n\n'
        self.assertTrue(e2e.rag_succeeded(200, body))
        self.assertFalse(e2e.rag_succeeded(503, body))

    def test_failure_cannot_hide_behind_earlier_answer(self):
        body = 'data: {"response_type":"answer","content":"AGENTRAG_E2E_ANSWER"}\n\ndata: {"response_type":"error"}\n\ndata: {"response_type":"complete"}\n\n'
        self.assertFalse(e2e.rag_succeeded(200, body))

    def test_uncorrelated_log_does_not_prove_offline_gate(self):
        self.assertFalse(e2e.gate_logged('other-request ' + e2e.GATE, 'this-request'))
        self.assertTrue(e2e.gate_logged('this-request ' + e2e.GATE, 'this-request'))


if __name__ == "__main__":
    unittest.main()
