"""Opt-in cancellation test using the actual isolated startup E2E runner."""
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unittest


@unittest.skipUnless(os.name == "posix" and os.environ.get("AGENTRAG_RUN_STARTUP_FAULT_TEST") == "1", "opt-in Linux real Core/Host cancellation test")
class StartupCancellationTest(unittest.TestCase):
    def test_sigterm_cleans_launcher_core_and_host(self):
        root = Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory(prefix="startup-cancel-") as directory:
            work = Path(directory)
            env = dict(os.environ, AGENTRAG_E2E_REPORT=str(work / "report.log"))
            with (work / "stdout.log").open("w") as output:
                process = subprocess.Popen([sys.executable, str(root / "extensions/scripts/startup_e2e.py")], cwd=root, env=env, stdout=output, stderr=subprocess.STDOUT)
                try:
                    deadline = time.monotonic() + 100
                    while "[ORDER] PASS" not in (work / "stdout.log").read_text():
                        self.assertIsNone(process.poll(), "E2E runner exited before test cancellation: " + (work / "stdout.log").read_text()[:1600])
                        if time.monotonic() > deadline:
                            self.fail("runner did not reach the owned running-services phase")
                        time.sleep(.1)
                    process.send_signal(signal.SIGTERM)
                    self.assertEqual(process.wait(timeout=50), 1)
                    result = (work / "stdout.log").read_text()
                    self.assertIn("E2E interrupted; cleaning up owned services", result)
                    private = next(line.split("] ", 1)[1] for line in result.splitlines() if line.startswith("[PRIVATE-DIAGNOSTICS]"))
                    cfg = json.loads((Path(private) / "private-launcher.json").read_text())
                    for port in (8090, int(cfg["CORE_PORT"])):
                        with socket.socket() as listener:
                            listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                            listener.bind(("127.0.0.1", port))
                finally:
                    if process.poll() is None:
                        process.send_signal(signal.SIGTERM)
                        process.wait(timeout=50)


if __name__ == "__main__":
    unittest.main()
