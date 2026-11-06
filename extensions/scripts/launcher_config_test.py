import importlib.util
import io
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
    def setUp(self):
        # prepare() revalidates the MinerU origin; without this the suite would
        # change behaviour depending on whether a real MinerU is listening.
        reachable = patch.object(launcher, "reachable_origin", return_value=False)
        reachable.start()
        self.addCleanup(reachable.stop)

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

    def test_supervisor_token_is_generated_once_and_stays_out_of_private_env(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            settings = {"AGENTRAG_TENANT_ID": "7", "AGENTRAG_KB_IDS": "kb-a", "AGENTRAG_EXT_DB": "data/ext.db"}
            with patch.object(launcher, "ROOT", root), patch.object(launcher, "STATE", root / "private.json"), patch.dict(os.environ, settings, clear=True):
                launcher.prepare(root / "private.env")
                first = launcher.load()["MODEL_SUPERVISOR_TOKEN"]
                self.assertGreaterEqual(len(first), 32)
                launcher.prepare(root / "private.env")
                self.assertEqual(launcher.load()["MODEL_SUPERVISOR_TOKEN"], first, "token must survive a relaunch")
                self.assertNotIn(first, (root / "private.env").read_text(), "the supervisor token is not a Host credential")
                self.assertNotIn("MODEL_SUPERVISOR_TOKEN", (root / "private.env").read_text())

    def test_plan_describes_services_without_leaking_the_token(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            settings = {"AGENTRAG_TENANT_ID": "7", "AGENTRAG_KB_IDS": "kb-a"}
            with patch.object(launcher, "ROOT", root), patch.object(launcher, "STATE", root / "private.json"), patch.dict(os.environ, settings, clear=True):
                launcher.prepare(root / "private.env")
                token = launcher.load()["MODEL_SUPERVISOR_TOKEN"]
                output = io.StringIO()
                with patch("sys.stdout", output):
                    launcher.plan()
                parsed = json.loads(output.getvalue())
                self.assertEqual([m["id"] for m in parsed["models"]], ["embedding", "rerank"])
                self.assertEqual(parsed["docker_services"], ["docreader"])
                self.assertTrue(parsed["supervisor"]["token_present"])
                self.assertNotIn(token, output.getvalue())

    def test_token_action_is_the_only_place_the_secret_is_printed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            settings = {"AGENTRAG_TENANT_ID": "7", "AGENTRAG_KB_IDS": "kb-a"}
            with patch.object(launcher, "ROOT", root), patch.object(launcher, "STATE", root / "private.json"), patch.dict(os.environ, settings, clear=True):
                launcher.prepare(root / "private.env")
                token = launcher.load()["MODEL_SUPERVISOR_TOKEN"]
                output = io.StringIO()
                with patch("sys.stdout", output):
                    launcher.show_token()
                self.assertEqual(output.getvalue().strip(), token)

    def test_mcp_key_needs_a_native_login_and_stores_nothing_on_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            settings = {"AGENTRAG_TENANT_ID": "7", "AGENTRAG_KB_IDS": "kb-a"}
            with patch.object(launcher, "ROOT", root), patch.object(launcher, "STATE", root / "private.json"), patch.dict(os.environ, settings, clear=True):
                launcher.prepare(root / "private.env")
                with patch("builtins.open", side_effect=OSError("no terminal")):
                    with self.assertRaises(RuntimeError):
                        launcher.issue_mcp_key()
                self.assertNotIn("MCP_ADMIN_KEY", launcher.load())

    def test_mineru_origin_is_revalidated_against_the_current_network(self):
        # A stale gateway origin (the mirrored-mode bug) is replaced by loopback
        # when the local service actually answers.
        with patch.object(launcher, "reachable_origin", side_effect=lambda origin: origin == "http://127.0.0.1:8000"):
            self.assertEqual(launcher.normalize_mineru("http://172.0.0.1:8000"), "http://127.0.0.1:8000")
        # A working remote origin is never overridden.
        with patch.object(launcher, "reachable_origin", side_effect=lambda origin: origin == "http://mineru.lan:8000"):
            self.assertEqual(launcher.normalize_mineru("http://mineru.lan:8000"), "http://mineru.lan:8000")
        # Nothing configured in mirrored mode: loopback, never a gateway guess.
        with patch.object(launcher, "reachable_origin", return_value=False), \
             patch.object(launcher, "networking_mode", return_value="mirrored"), \
             patch.object(launcher, "default_gateway", return_value="192.168.1.1"):
            self.assertEqual(launcher.normalize_mineru(""), "http://127.0.0.1:8000")
        # NAT keeps the gateway route, because loopback cannot work there.
        with patch.object(launcher, "reachable_origin", return_value=False), \
             patch.object(launcher, "networking_mode", return_value="nat"), \
             patch.object(launcher, "default_gateway", return_value="172.17.32.1"):
            self.assertEqual(launcher.normalize_mineru(""), "http://172.17.32.1:8000")

    def test_prepare_repairs_a_stale_mineru_origin(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with patch.object(launcher, "ROOT", root), patch.object(launcher, "STATE", root / "private.json"), \
                 patch.dict(os.environ, {"AGENTRAG_TENANT_ID": "7", "AGENTRAG_KB_IDS": "kb-a"}, clear=True):
                launcher.save({"AGENTRAG_TENANT_ID": "7", "AGENTRAG_KB_IDS": "kb-a", "MINERU_ENDPOINT": "http://172.0.0.1:8000"})
                with patch.object(launcher, "reachable_origin", side_effect=lambda origin: origin == "http://127.0.0.1:8000"):
                    launcher.prepare(root / "private.env")
                restored = launcher.load()
                self.assertEqual(restored["MINERU_ENDPOINT"], "http://127.0.0.1:8000")
                self.assertEqual(restored["AGENTRAG_PARSER_UPSTREAM_URL"], "http://127.0.0.1:8000")

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
