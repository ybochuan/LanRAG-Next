import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("launcher_config", Path(__file__).with_name("launcher_config.py"))
launcher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(launcher)


class LauncherConfigTest(unittest.TestCase):
    def test_paths_and_native_db_separation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            settings = {"AGENTRAG_TENANT_ID": "7", "AGENTRAG_KB_IDS": "kb-a", "MINERU_ENDPOINT": "http://127.0.0.1:8000", "AGENTRAG_EXT_DB": "data/weknora.db"}
            with patch.object(launcher, "ROOT", root), patch.object(launcher, "STATE", root / "private.json"), patch.dict(os.environ, settings, clear=True):
                launcher.prepare(root / "private.env")
                cfg = launcher.load()
                self.assertEqual(cfg["AGENTRAG_EXT_DB"], str(root / "data/agentrag_ext.db"))
                self.assertEqual(cfg["AGENTRAG_PARSER_UPSTREAM_URL"], settings["MINERU_ENDPOINT"])
                self.assertNotEqual(cfg["AGENTRAG_EXT_DB"], cfg["DB_PATH"])

    def test_shell_values_are_quoted_and_unknown_keys_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with patch.object(launcher, "STATE", root / "private.json"):
                launcher.save({"AGENTRAG_WEKNORA_API_KEY": "x'$(touch /tmp/not-executed)"})
                launcher.emit_env(root / "private.env")
                self.assertIn("'\"'\"'", (root / "private.env").read_text())
                launcher.save({"BASH_ENV": "/tmp/untrusted"})
                with self.assertRaises(RuntimeError):
                    launcher.emit_env(root / "private.env")

    def test_invalid_port_does_not_write_configuration(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with patch.object(launcher, "ROOT", root), patch.object(launcher, "STATE", root / "private.json"), patch.dict(os.environ, {"AGENTRAG_TENANT_ID": "7", "AGENTRAG_KB_IDS": "kb-a", "CORE_PORT": "8090"}, clear=True):
                with self.assertRaises(RuntimeError):
                    launcher.prepare(root / "private.env")
                self.assertFalse(launcher.STATE.exists())

    def test_saved_paths_ports_and_parser_origin_survive_restart(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            saved = {"AGENTRAG_TENANT_ID":"7", "AGENTRAG_KB_IDS":"kb-a", "ENV_FILE":str(root / "custom.env"), "DB_PATH":str(root / "custom.db"), "CORE_PORT":"18080", "MINERU_ENDPOINT":"http://127.0.0.1:18000", "AGENTRAG_PARSER_UPSTREAM_URL":"http://127.0.0.1:28000", "AGENTRAG_RECONCILE_INTERVAL":"17s", "SSRF_DISABLE":"false"}
            with patch.object(launcher,"ROOT",root), patch.object(launcher,"STATE",root / "private.json"), patch.dict(os.environ,{},clear=True):
                launcher.save(saved)
                launcher.prepare(root / "private.env")
                restored = launcher.load()
                for key,value in saved.items(): self.assertEqual(restored[key],value,key)

    def test_dotenv_export_spaces_quotes_and_complex_refusal(self):
        with tempfile.TemporaryDirectory() as directory:
            filename = Path(directory) / "config.env"
            for text in ('export DB_PATH=custom/native.db', 'DB_PATH = "custom/native.db" # comment'):
                filename.write_text(text)
                self.assertEqual(launcher.dotenv_value(filename,"DB_PATH","wrong"),"custom/native.db")
            filename.write_text('DB_PATH="${ROOT_DIR}/native.db"')
            with self.assertRaises(RuntimeError): launcher.dotenv_value(filename,"DB_PATH","wrong")

    @unittest.skipIf(os.name == "nt", "POSIX file modes and symlinks")
    def test_private_environment_permissions_and_link_refusal(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            with patch.object(launcher,"STATE",root / "private.json"):
                launcher.save({"AGENTRAG_WEKNORA_API_KEY":"local-test-not-secret"})
                target=root / "private.env"; target.touch(); target.chmod(0o644)
                launcher.emit_env(target)
                self.assertEqual(target.stat().st_mode & 0o777,0o600)
                link=root / "link.env"; link.symlink_to(target)
                with self.assertRaises(RuntimeError): launcher.emit_env(link)


if __name__ == "__main__":
    unittest.main()
