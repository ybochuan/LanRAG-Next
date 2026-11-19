#!/usr/bin/env python3
"""Check an initialized AgentRAG instance without changing data or displaying IDs."""

from contextlib import closing
import json
import os
from pathlib import Path
import re
import sqlite3
import sys


ROOT = Path(__file__).resolve().parents[1]
NATIVE_DATA = {
    "knowledge_bases", "knowledges", "chunks", "lite_embeddings", "sessions", "messages",
    "knowledge_tags", "knowledge_tag_relations", "notifications", "audit_logs", "resources",
    "resource_bindings", "resource_access_grants", "memory_items", "memory_item_embeddings",
    "memory_doc_affinity", "task_pending_ops", "task_dead_letters", "agent_run_suspensions",
}
EXT_HISTORY = {"ext_schema_migrations", "ext_cleanup_runs", "ext_evidence_retention_runs"}


def local_path(value):
    portable = re.fullmatch(r"/mnt/([a-z])/(.*)", str(value)) if os.name == "nt" else None
    return Path(f"{portable[1]}:/{portable[2]}" if portable else value)


def native_business_table(name):
    # SQLite's vec0 module is registered by Core, so inspect the row-ID backing table.
    return name in NATIVE_DATA or re.fullmatch(r"vec_embeddings_\d+_rowids", name) is not None


def inspect_database(filename, select_tables, required):
    path = local_path(filename)
    if not path.is_file():
        return {"status": "unreadable"}
    try:
        with closing(sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True)) as db:
            db.execute("PRAGMA query_only=ON")
            db.execute("BEGIN")
            tables = {row[0] for row in db.execute("SELECT name FROM sqlite_schema WHERE type='table'")}
            if not required <= tables:
                return {"status": "schema_missing", "missing": sorted(required - tables)}
            selected = sorted(filter(select_tables, tables))
            counts = {}
            for name in selected:
                quoted = '"' + name.replace('"', '""') + '"'
                count = db.execute(f"SELECT count(*) FROM {quoted}").fetchone()[0]
                if count:
                    counts[name] = count
            result = {
                "status": "read",
                "quick_check_ok": db.execute("PRAGMA quick_check").fetchall() == [("ok",)],
                "foreign_key_violations": sum(1 for _ in db.execute("PRAGMA foreign_key_check")),
                "tables_checked": len(selected),
                "nonempty": counts,
            }
            if "ext_schema_migrations" in tables:
                result["extension_schema_version"] = db.execute("SELECT max(version) FROM ext_schema_migrations").fetchone()[0]
            return result
    except (OSError, sqlite3.Error):
        return {"status": "check_failed"}


def inspect_files(root):
    root = local_path(root)
    if not root.exists():
        return {"status": "absent", "files": 0}
    if not root.is_dir():
        return {"status": "check_failed"}

    def fail(error):
        raise error

    try:
        count = sum(len(files) for _, _, files in os.walk(root, onerror=fail))
    except OSError:
        return {"status": "check_failed"}
    return {"status": "read", "files": count}


def main():
    state = Path(os.environ.get("AGENTRAG_LAUNCHER_CONFIG", ROOT / "data/agentrag-launcher.json"))
    try:
        config = json.loads(state.read_text(encoding="utf-8-sig"))
    except (OSError, ValueError):
        print(json.dumps({"launcher_state": "unreadable"}))
        return 2
    if not isinstance(config, dict):
        print(json.dumps({"launcher_state": "invalid"}))
        return 2
    result = {
        "saved_kb_scopes": sum(bool(item.strip()) for item in config.get("AGENTRAG_KB_IDS", "").split(",")),
        "mcp_key_configured": bool(config.get("MCP_ADMIN_KEY")),
        "launcher_private_env_copies": sum(1 for file in (ROOT / "data").glob("launcher-run-*/private.env") if file.is_file()),
        "launcher_pid_markers": sum(1 for file in (ROOT / "data").glob("launcher-run-*/pids") if file.is_file()),
        "native": inspect_database(config.get("DB_PATH", ""), native_business_table, {"knowledges", "chunks", "knowledge_bases"}),
        "extension": inspect_database(config.get("AGENTRAG_EXT_DB", ""), lambda name: name.startswith("ext_") and name not in EXT_HISTORY, {"ext_schema_migrations", "ext_lifecycle_current"}),
        "local_files": inspect_files(os.environ.get("LOCAL_STORAGE_BASE_DIR", ROOT / "data/files")),
        "parser_files": inspect_files(config.get("AGENTRAG_PARSER_ARTIFACT_ROOT") or ROOT / "data/parser_artifacts"),
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    databases = [result["native"], result["extension"]]
    storage = [result["local_files"], result["parser_files"]]
    if any(db["status"] != "read" for db in databases) or any(folder["status"] == "check_failed" for folder in storage):
        return 2
    if any(not db["quick_check_ok"] or db["foreign_key_violations"] or db["nonempty"] for db in databases):
        return 1
    return int(any(folder["files"] for folder in storage))


if __name__ == "__main__":
    sys.exit(main())
