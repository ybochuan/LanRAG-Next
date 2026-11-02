import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock


SCRIPT = Path(__file__).with_name("m1_scope_poc.py")


class ScopePOCLifecycleTests(unittest.TestCase):
    def test_backend_guard_denies_external_addresses_before_any_download(self):
        spec = importlib.util.spec_from_file_location("m1_scope_poc", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        # Execute only the stdlib guard in a disposable process, never install
        # a permanent audit hook in this test process. Numeric address: no DNS.
        prefix = module.BACKEND.split("import uvicorn", 1)[0]
        assertion = '''
socket.getaddrinfo("127.0.0.1", 80)
try:
    socket.getaddrinfo("203.0.113.1", 80)
except PermissionError:
    pass
else:
    raise SystemExit("external address not blocked")
'''
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / "network-attempts"
            result = subprocess.run([sys.executable, "-B", "-c", prefix+assertion, "uploads", "ready", str(marker)], capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
            self.assertEqual(marker.read_text(encoding="utf-8"), "blocked\n")

    def test_unconfirmed_exit_never_reports_stopped(self):
        spec = importlib.util.spec_from_file_location("m1_scope_poc", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        messages = []
        with mock.patch.object(module, "stop_owned", side_effect=OSError("simulated OS refusal")):
            stopped = module.stop_all_owned([object()], messages.append)
        self.assertFalse(stopped)
        self.assertFalse(any("Owned processes stopped" in message for message in messages))
    def test_missing_interpreter_fails_and_writes_a_report(self):
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory) / "report.md"
            result = subprocess.run(
                [sys.executable, str(SCRIPT), "--mineru-python", str(Path(directory) / "missing.exe"), "--report", str(report)],
                capture_output=True, text=True, timeout=10,
            )
            self.assertEqual(result.returncode, 1)
            self.assertIn("[RESULT] FAIL", report.read_text(encoding="utf-8"))

    def test_cleanup_stops_an_actual_owned_process(self):
        spec = importlib.util.spec_from_file_location("m1_scope_poc", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        process = subprocess.Popen(
            [sys.executable, "-c", "import time; time.sleep(60)"],
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
        )
        try:
            module.stop_owned(process)
            self.assertIsNotNone(process.poll())
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=5)

    @unittest.skipUnless(sys.platform == "win32", "Windows sharing/deletion semantics")
    def test_cleanup_retries_a_real_temporarily_locked_file(self):
        spec = importlib.util.spec_from_file_location("m1_scope_poc", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        private = tempfile.TemporaryDirectory(prefix="agentrag-m1-scope-")
        root = Path(private.name)
        held = (root / "held.log").open("wb")
        errors = []
        def clean():
            try:
                module.cleanup_private(private)
            except Exception as error:
                errors.append(error)
        worker = threading.Thread(target=clean)
        worker.start()
        time.sleep(0.2)
        held.close()
        worker.join(timeout=5)
        self.assertFalse(worker.is_alive())
        self.assertEqual(errors, [])
        self.assertFalse(root.exists())


if __name__ == "__main__":
    unittest.main()
