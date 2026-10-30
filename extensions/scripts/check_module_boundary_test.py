"""Exercise the boundary command against disposable, real Go modules."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("check_module_boundary.py")
MODULE = "github.com/ybochuan/LanRAG-Next/extensions"


class ModuleBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="agentrag-module-test-")
        self.module_dir = Path(self.temporary.name).resolve()
        self.assertTrue(self.module_dir.is_relative_to(Path(tempfile.gettempdir()).resolve()))
        self.addCleanup(self.temporary.cleanup)
        self.write_module(f"module {MODULE}\n\ngo 1.26.0\n")
        (self.module_dir / "fixture.go").write_text(
            'package fixture\n\nimport "fmt"\n\nfunc Message() string { return fmt.Sprint("ok") }\n',
            encoding="utf-8",
        )

    def write_module(self, text):
        (self.module_dir / "go.mod").write_text(text, encoding="utf-8")

    def run_guard(self, **environment):
        return subprocess.run(
            [sys.executable, str(SCRIPT), "--module-dir", str(self.module_dir)],
            capture_output=True,
            text=True,
            env={**os.environ, "GOWORK": "off", **environment},
            timeout=45,
            check=False,
        )

    def assert_rejected(self, result, reason):
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn(reason, result.stderr)

    def test_standalone_module_passes_without_changing_manifest(self):
        manifest = (self.module_dir / "go.mod").read_bytes()
        result = self.run_guard()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("module boundary passed", result.stdout)
        self.assertEqual((self.module_dir / "go.mod").read_bytes(), manifest)
        self.assertFalse((self.module_dir / "go.sum").exists())

    def test_wrong_module_identity_is_rejected(self):
        self.write_module("module example.com/not-extensions\n\ngo 1.26.0\n")
        self.assert_rejected(self.run_guard(), "unexpected module identity")

    def test_core_requirement_is_rejected_before_network_resolution(self):
        self.write_module(
            f"module {MODULE}\n\ngo 1.26.0\n\nrequire github.com/Tencent/WeKnora v0.0.0\n"
        )
        self.assert_rejected(self.run_guard(GOPROXY="off"), "forbidden module")

    def test_unused_core_replacement_is_rejected(self):
        self.write_module(
            f"module {MODULE}\n\ngo 1.26.0\n\n"
            "replace github.com/Tencent/WeKnora => ./unused-core\n"
        )
        self.assert_rejected(self.run_guard(), "forbidden module")

    def test_replacement_target_cannot_disguise_core_dependency(self):
        self.write_module(
            f"module {MODULE}\n\ngo 1.26.0\n\n"
            "replace example.com/alias => github.com/Tencent/WeKnora v0.0.0\n"
        )
        self.assert_rejected(self.run_guard(GOPROXY="off"), "forbidden module")

    def test_local_replacement_cannot_disguise_core_module(self):
        core = self.module_dir / "unused-core"
        core.mkdir()
        (core / "go.mod").write_text(
            "module github.com/Tencent/WeKnora\n\ngo 1.26.0\n", encoding="utf-8"
        )
        self.write_module(
            f"module {MODULE}\n\ngo 1.26.0\n\n"
            "replace example.com/alias => ./unused-core\n"
        )
        self.assert_rejected(self.run_guard(), "forbidden module")

    def test_workspace_is_rejected_even_when_current_module_is_valid(self):
        workspace = self.module_dir / "fixture.work"
        workspace.write_text("go 1.26.0\n\nuse .\n", encoding="utf-8")
        self.assert_rejected(self.run_guard(GOWORK=str(workspace)), "active Go workspace")


if __name__ == "__main__":
    unittest.main()
