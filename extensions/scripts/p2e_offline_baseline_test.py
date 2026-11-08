"""Tests for the P2-E-01 read-only offline baseline script."""

from __future__ import annotations

import importlib.util
import json
import os
import sqlite3
import tempfile
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).with_name("p2e_offline_baseline.py")
SPEC = importlib.util.spec_from_file_location("p2e_offline_baseline", SCRIPT_PATH)
baseline = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(baseline)


def _request_line(
    timestamp: str,
    request_id: str,
    latency: str,
    method: str,
    path: str,
    status: int,
    level: str = "INFO",
) -> str:
    return (
        f"{level} [{timestamp}] [{request_id} client_ip=127.0.0.1 latency={latency} "
        f'method={method} path={path} response_body={{"ok":true}} size=11 status_code={status}]                      |'
    )


class CoreLogTests(unittest.TestCase):
    def test_parses_and_normalizes_requests(self):
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / "core.log"
            log.write_text(
                "\n".join(
                    [
                        _request_line(
                            "2026-11-08 14:00:00.000",
                            "req-1",
                            "1.5ms",
                            "GET",
                            "/api/v1/knowledge-bases/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/knowledge",
                            200,
                        ),
                        _request_line(
                            "2026-11-08 14:00:01.000",
                            "req-2",
                            "250µs",
                            "GET",
                            "/health",
                            200,
                        ),
                        _request_line(
                            "2026-11-08 14:00:02.000",
                            "req-3",
                            "2s",
                            "POST",
                            "/api/v1/knowledge-bases/123/lifecycle",
                            401,
                            level="WARNING",
                        ),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            result = baseline.parse_core_log(log)

        self.assertEqual(result["line_count"], 3)
        self.assertEqual(result["request_count"], 3)
        self.assertEqual(result["parse_errors"], 0)
        self.assertEqual(dict(result["methods"]), {"GET": 2, "POST": 1})
        self.assertEqual(dict(result["status_codes"]), {"200": 2, "401": 1})
        self.assertEqual(dict(result["status_classes"]), {"2xx": 2, "4xx": 1})
        self.assertIn("/api/v1/knowledge-bases/:id/knowledge", result["routes"])
        self.assertIn("/api/v1/knowledge-bases/:id/lifecycle", result["routes"])
        summary = baseline.latency_summary(result["_latency_values_ms"])
        self.assertEqual(summary["count"], 3)
        self.assertAlmostEqual(summary["p50_ms"], 1.5)
        self.assertAlmostEqual(summary["max_ms"], 2000.0)
        self.assertEqual(summary["over_200ms"], 1)
        self.assertEqual(summary["over_1s"], 1)

    def test_counts_unparseable_request_lines(self):
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / "core.log"
            log.write_text(
                "INFO [2026-11-08 14:00:00.000] [req-1 client_ip=127.0.0.1 latency=bad\n",
                encoding="utf-8",
            )
            result = baseline.parse_core_log(log)
        self.assertEqual(result["request_count"], 0)
        self.assertEqual(result["parse_errors"], 1)


class HostLogTests(unittest.TestCase):
    def test_counts_events_and_reconciliation_codes(self):
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / "host.log"
            log.write_text(
                "\n".join(
                    [
                        "2026/11/08 14:04:03 host_workers_started",
                        "2026/11/08 14:04:03 reconciliation_waiting_for_core "
                        "code=upstream_unreachable consecutive=1 next_retry=2026-11-08T06:04:08Z",
                        "2026/11/08 14:04:09 reconciliation_recovered",
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            result = baseline.parse_host_log(log)

        self.assertEqual(result["line_count"], 3)
        self.assertEqual(result["parse_errors"], 0)
        self.assertEqual(
            dict(result["events"]),
            {
                "host_workers_started": 1,
                "reconciliation_recovered": 1,
                "reconciliation_waiting_for_core": 1,
            },
        )
        self.assertEqual(dict(result["codes"]), {"upstream_unreachable": 1})


class OutboxTests(unittest.TestCase):
    def test_reads_status_counts_and_pending_age(self):
        with tempfile.TemporaryDirectory() as directory:
            database = Path(directory) / "ext.db"
            connection = sqlite3.connect(database)
            try:
                connection.execute(
                    "CREATE TABLE ext_projection_outbox "
                    "(status TEXT NOT NULL, retry_count INTEGER NOT NULL, created_at INTEGER NOT NULL)"
                )
                connection.executemany(
                    "INSERT INTO ext_projection_outbox VALUES (?, ?, ?)",
                    [
                        ("pending", 0, 1_000_000),
                        ("processing", 1, 2_000_000),
                        ("processing", 1, 4_000_000),
                        ("dead", 5, 500_000),
                    ],
                )
                connection.commit()
            finally:
                connection.close()

            result = baseline.read_outbox_stats(database, now_ms=2_010_000)

        self.assertTrue(result["available"])
        self.assertEqual(result["total"], 4)
        self.assertEqual(result["by_status"], {"dead": 1, "pending": 1, "processing": 2})
        self.assertEqual(result["oldest_pending_created_at_ms"], 1_000_000)
        self.assertAlmostEqual(result["oldest_pending_age_seconds"], 1010.0)
        self.assertEqual(result["created_at_min_ms"], 500_000)
        self.assertEqual(result["created_at_max_ms"], 4_000_000)
        self.assertEqual(result["max_retry_count"], 5)

    def test_missing_database_is_reported_without_error(self):
        result = baseline.read_outbox_stats(Path("missing-p2e-outbox.db"))
        self.assertFalse(result["available"])
        self.assertEqual(result["reason"], "database_missing")


class RunDiscoveryAndReportTests(unittest.TestCase):
    def test_discovers_newest_run_and_builds_json(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            older = root / "launcher-run-older"
            newer = root / "launcher-run-newer"
            older.mkdir()
            newer.mkdir()
            (older / "core.log").write_text(
                _request_line("2026-11-08 10:00:00.000", "old", "1ms", "GET", "/health", 200)
                + "\n",
                encoding="utf-8",
            )
            (newer / "core.log").write_text(
                _request_line("2026-11-08 12:00:00.000", "new", "2ms", "GET", "/health", 200)
                + "\n",
                encoding="utf-8",
            )
            (newer / "host.log").write_text("2026/11/08 12:00:00 host_workers_started\n", encoding="utf-8")
            os.utime(older / "core.log", (1_700_000_000, 1_700_000_000))
            os.utime(newer / "core.log", (1_700_000_100, 1_700_000_100))

            found = baseline.discover_run_dirs(root, latest=1)
            report = baseline.build_report(found, None, now_ms=1_700_000_000_000)

        self.assertEqual([path.name for path in found], ["launcher-run-newer"])
        self.assertEqual(report["aggregate"]["request_count"], 1)
        self.assertEqual(report["aggregate"]["host_events"], {"host_workers_started": 1})
        self.assertEqual(report["outbox"]["reason"], "database_not_configured")
        json.dumps(report)


if __name__ == "__main__":
    unittest.main()
