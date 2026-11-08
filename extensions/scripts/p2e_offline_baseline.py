#!/usr/bin/env python3
"""P2-E-01 read-only offline performance baseline.

The script parses existing AgentRAG Core/Host logs and reads the extension
outbox database in SQLite read-only mode. It emits only aggregate data:
request bodies, tenant IDs, knowledge-base IDs, session IDs and credentials
are never included in the output.
"""

from __future__ import annotations

import argparse
import json
import re
import sqlite3
import time
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_LOG_ROOT = REPO_ROOT / "data"
DEFAULT_LAUNCHER_CONFIG = DEFAULT_LOG_ROOT / "agentrag-launcher.json"

# Example:
# INFO [2026-11-08 14:04:07.025] [request-id client_ip=127.0.0.1
# latency=940.186µs method=GET path=/health response_body=...
# size=15 status_code=200]
_REQUEST_RE = re.compile(
    r"^[A-Z]+ *\[(?P<timestamp>[^\]]+)\] \[[^ ]+ client_ip=(?P<client_ip>\S+) "
    r"latency=(?P<latency_n>[0-9]+(?:\.[0-9]+)?)(?P<latency_unit>ns|µs|μs|ms|s) "
    r"method=(?P<method>\S+) path=(?P<path>\S+) .*?"
    r"size=(?P<size>-?\d+) status_code=(?P<status>\d+)\]\s*(?:\|\s*)?$"
)
_HOST_RE = re.compile(
    r"^(?P<timestamp>\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) "
    r"(?P<event>[A-Za-z0-9_]+)(?: (?P<fields>.*))?$"
)
_UUID_RE = re.compile(
    r"/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-"
    r"[0-9a-fA-F]{4}-[0-9a-fA-F]{12}(?=/|$)"
)
_NUMERIC_ID_RE = re.compile(r"/\d{2,}(?=/|$)")
_FIELD_RE = re.compile(r"(?P<key>[A-Za-z_][A-Za-z0-9_.-]*)=(?P<value>\S+)")
_UNIT_TO_MS = {"ns": 1e-6, "µs": 1e-3, "μs": 1e-3, "ms": 1.0, "s": 1000.0}


def normalize_route(path: str) -> str:
    """Drop query strings and normalize opaque path identifiers."""
    route = path.split("?", 1)[0]
    route = _UUID_RE.sub("/:id", route)
    route = _NUMERIC_ID_RE.sub("/:id", route)
    return route or "/"


def percentile(values: list[float], fraction: float) -> float | None:
    """Return a linearly interpolated percentile for an unsorted sample."""
    if not values:
        return None
    ordered = sorted(values)
    position = (len(ordered) - 1) * fraction
    lower = int(position)
    upper = min(lower + 1, len(ordered) - 1)
    weight = position - lower
    return ordered[lower] + (ordered[upper] - ordered[lower]) * weight


def latency_summary(values: list[float]) -> dict[str, Any]:
    """Aggregate latency values in milliseconds without exposing samples."""
    if not values:
        return {
            "count": 0,
            "p50_ms": None,
            "p95_ms": None,
            "p99_ms": None,
            "max_ms": None,
            "mean_ms": None,
            "over_200ms": 0,
            "over_1s": 0,
        }
    return {
        "count": len(values),
        "p50_ms": round(percentile(values, 0.50) or 0.0, 6),
        "p95_ms": round(percentile(values, 0.95) or 0.0, 6),
        "p99_ms": round(percentile(values, 0.99) or 0.0, 6),
        "max_ms": round(max(values), 6),
        "mean_ms": round(sum(values) / len(values), 6),
        "over_200ms": sum(value > 200 for value in values),
        "over_1s": sum(value > 1000 for value in values),
    }


def parse_core_log(path: Path) -> dict[str, Any]:
    """Parse one Core request log into aggregate counters and latencies."""
    result: dict[str, Any] = {
        "file": path.name,
        "line_count": 0,
        "request_count": 0,
        "parse_errors": 0,
        "first_request_at": None,
        "last_request_at": None,
        "methods": Counter(),
        "status_codes": Counter(),
        "status_classes": Counter(),
        "routes": Counter(),
        "_latency_values_ms": [],
    }
    with path.open("r", encoding="utf-8", errors="replace") as handle:
        for raw_line in handle:
            result["line_count"] += 1
            if "latency=" not in raw_line:
                continue
            match = _REQUEST_RE.match(raw_line.rstrip("\r\n"))
            if not match:
                result["parse_errors"] += 1
                continue
            timestamp = match.group("timestamp")
            if result["first_request_at"] is None:
                result["first_request_at"] = timestamp
            result["last_request_at"] = timestamp
            latency_ms = float(match.group("latency_n")) * _UNIT_TO_MS[match.group("latency_unit")]
            result["_latency_values_ms"].append(latency_ms)
            result["request_count"] += 1
            result["methods"][match.group("method")] += 1
            status = int(match.group("status"))
            result["status_codes"][str(status)] += 1
            result["status_classes"][f"{status // 100}xx"] += 1
            result["routes"][normalize_route(match.group("path"))] += 1
    return result


def parse_host_log(path: Path) -> dict[str, Any]:
    """Parse one Host event log into aggregate event/code counters."""
    result: dict[str, Any] = {
        "file": path.name,
        "line_count": 0,
        "parse_errors": 0,
        "first_event_at": None,
        "last_event_at": None,
        "events": Counter(),
        "codes": Counter(),
    }
    if not path.exists():
        return result
    with path.open("r", encoding="utf-8", errors="replace") as handle:
        for raw_line in handle:
            result["line_count"] += 1
            match = _HOST_RE.match(raw_line.rstrip("\r\n"))
            if not match:
                result["parse_errors"] += 1
                continue
            timestamp = match.group("timestamp")
            if result["first_event_at"] is None:
                result["first_event_at"] = timestamp
            result["last_event_at"] = timestamp
            result["events"][match.group("event")] += 1
            fields = match.group("fields") or ""
            for field in _FIELD_RE.finditer(fields):
                if field.group("key") == "code":
                    result["codes"][field.group("value")] += 1
    return result


def read_outbox_stats(db_path: Path | None, now_ms: int | None = None) -> dict[str, Any]:
    """Read only the extension outbox table and return aggregate status data."""
    result: dict[str, Any] = {
        "available": False,
        "table": "ext_projection_outbox",
    }
    if db_path is None:
        result["reason"] = "database_not_configured"
        return result
    path = Path(db_path).expanduser()
    if not path.exists():
        result["reason"] = "database_missing"
        return result
    try:
        connection = sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True, timeout=2.0)
        try:
            table = connection.execute(
                "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'ext_projection_outbox'"
            ).fetchone()
            if table is None:
                result["reason"] = "table_missing"
                return result
            rows = connection.execute(
                "SELECT status, COUNT(*), MIN(created_at), MAX(created_at), MAX(retry_count) "
                "FROM ext_projection_outbox GROUP BY status"
            ).fetchall()
            oldest_pending = connection.execute(
                "SELECT MIN(created_at) FROM ext_projection_outbox "
                "WHERE status IN ('pending', 'processing')"
            ).fetchone()[0]
        finally:
            connection.close()
    except (OSError, sqlite3.Error) as exc:
        result["reason"] = type(exc).__name__
        return result

    by_status = {str(row[0]): int(row[1]) for row in rows}
    created_min_values = [int(row[2]) for row in rows if row[2] is not None]
    created_max_values = [int(row[3]) for row in rows if row[3] is not None]
    retry_values = [int(row[4]) for row in rows if row[4] is not None]
    oldest_pending_ms = int(oldest_pending) if oldest_pending is not None else None
    age_seconds = None
    if oldest_pending_ms is not None:
        current = now_ms if now_ms is not None else int(time.time() * 1000)
        age_seconds = max(0.0, (current - oldest_pending_ms) / 1000.0)
    result.update(
        {
            "available": True,
            "total": sum(by_status.values()),
            "by_status": dict(sorted(by_status.items())),
            "oldest_pending_created_at_ms": oldest_pending_ms,
            "oldest_pending_age_seconds": round(age_seconds, 3) if age_seconds is not None else None,
            "created_at_min_ms": min(created_min_values) if created_min_values else None,
            "created_at_max_ms": max(created_max_values) if created_max_values else None,
            "max_retry_count": max(retry_values) if retry_values else None,
        }
    )
    return result


def resolve_ext_db_path(explicit: Path | None, launcher_config: Path) -> Path | None:
    """Resolve the extension DB path without printing launcher configuration."""
    if explicit is not None:
        return Path(explicit)
    try:
        config = json.loads(launcher_config.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    value = config.get("AGENTRAG_EXT_DB")
    if not value:
        return None
    path = Path(value)
    return path if path.is_absolute() else launcher_config.parent / path


def discover_run_dirs(log_root: Path, latest: int, all_runs: bool = False) -> list[Path]:
    """Find launcher run directories with a core.log, newest first."""
    root = Path(log_root)
    if root.is_dir() and (root / "core.log").exists():
        candidates = [root]
    else:
        candidates = [
            path
            for path in root.glob("launcher-run-*")
            if path.is_dir() and (path / "core.log").exists()
        ]
    candidates.sort(key=lambda path: (path / "core.log").stat().st_mtime, reverse=True)
    return candidates if all_runs else candidates[: max(0, latest)]


def _counter_dict(counter: Counter[str]) -> dict[str, int]:
    return {key: int(value) for key, value in sorted(counter.items())}


def _summarize_core(result: dict[str, Any], top_routes: int = 20) -> dict[str, Any]:
    summary = {key: value for key, value in result.items() if key != "_latency_values_ms"}
    summary["methods"] = _counter_dict(result["methods"])
    summary["status_codes"] = _counter_dict(result["status_codes"])
    summary["status_classes"] = _counter_dict(result["status_classes"])
    summary["routes"] = {
        key: int(value) for key, value in result["routes"].most_common(top_routes)
    }
    summary["latency_ms"] = latency_summary(result["_latency_values_ms"])
    return summary


def _summarize_host(result: dict[str, Any]) -> dict[str, Any]:
    summary = dict(result)
    summary["events"] = _counter_dict(result["events"])
    summary["codes"] = _counter_dict(result["codes"])
    return summary


def build_report(
    run_dirs: list[Path],
    ext_db_path: Path | None,
    now_ms: int | None = None,
) -> dict[str, Any]:
    """Build the final JSON-serializable baseline report."""
    runs: list[dict[str, Any]] = []
    all_latencies: list[float] = []
    all_methods: Counter[str] = Counter()
    all_status_codes: Counter[str] = Counter()
    all_status_classes: Counter[str] = Counter()
    all_routes: Counter[str] = Counter()
    all_host_events: Counter[str] = Counter()
    all_host_codes: Counter[str] = Counter()
    total_requests = 0
    total_parse_errors = 0
    first_request_at: str | None = None
    last_request_at: str | None = None

    for run_dir in run_dirs:
        core_path = run_dir / "core.log"
        host_path = run_dir / "host.log"
        core = parse_core_log(core_path) if core_path.exists() else {
            "file": core_path.name,
            "line_count": 0,
            "request_count": 0,
            "parse_errors": 0,
            "first_request_at": None,
            "last_request_at": None,
            "methods": Counter(),
            "status_codes": Counter(),
            "status_classes": Counter(),
            "routes": Counter(),
            "_latency_values_ms": [],
        }
        host = parse_host_log(host_path)
        all_latencies.extend(core["_latency_values_ms"])
        all_methods.update(core["methods"])
        all_status_codes.update(core["status_codes"])
        all_status_classes.update(core["status_classes"])
        all_routes.update(core["routes"])
        all_host_events.update(host["events"])
        all_host_codes.update(host["codes"])
        total_requests += core["request_count"]
        total_parse_errors += core["parse_errors"] + host["parse_errors"]
        if core["first_request_at"] and (
            first_request_at is None or core["first_request_at"] < first_request_at
        ):
            first_request_at = core["first_request_at"]
        if core["last_request_at"] and (
            last_request_at is None or core["last_request_at"] > last_request_at
        ):
            last_request_at = core["last_request_at"]
        runs.append(
            {
                "name": run_dir.name,
                "core": _summarize_core(core),
                "host": _summarize_host(host),
            }
        )

    return {
        "schema": "p2e-offline-baseline/v1",
        "generated_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "runs": runs,
        "aggregate": {
            "request_count": total_requests,
            "parse_errors": total_parse_errors,
            "first_request_at": first_request_at,
            "last_request_at": last_request_at,
            "methods": _counter_dict(all_methods),
            "status_codes": _counter_dict(all_status_codes),
            "status_classes": _counter_dict(all_status_classes),
            "routes": dict(all_routes.most_common(20)),
            "latency_ms": latency_summary(all_latencies),
            "host_events": _counter_dict(all_host_events),
            "host_codes": _counter_dict(all_host_codes),
        },
        "outbox": read_outbox_stats(ext_db_path, now_ms=now_ms),
    }


def _parse_args(argv: list[str] | None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="P2-E-01 read-only Core/Host/Outbox offline baseline"
    )
    parser.add_argument(
        "--run-dir",
        action="append",
        type=Path,
        default=[],
        help="specific launcher-run directory; repeat for multiple runs",
    )
    parser.add_argument(
        "--log-root",
        type=Path,
        default=DEFAULT_LOG_ROOT,
        help="directory containing launcher-run-* folders",
    )
    parser.add_argument("--latest", type=int, default=1, help="number of newest runs to analyze")
    parser.add_argument("--all-runs", action="store_true", help="analyze every launcher-run folder")
    parser.add_argument("--ext-db", type=Path, default=None, help="extension SQLite DB path (read-only)")
    parser.add_argument(
        "--launcher-config",
        type=Path,
        default=DEFAULT_LAUNCHER_CONFIG,
        help="launcher JSON used only to resolve AGENTRAG_EXT_DB",
    )
    parser.add_argument("--out", type=Path, default=None, help="optional JSON output path")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = _parse_args(argv)
    run_dirs = args.run_dir or discover_run_dirs(args.log_root, args.latest, args.all_runs)
    if not run_dirs:
        raise SystemExit(f"no launcher-run directories with core.log found under {args.log_root}")
    ext_db_path = resolve_ext_db_path(args.ext_db, args.launcher_config)
    report = build_report(run_dirs, ext_db_path)
    payload = json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True)
    if args.out is not None:
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_text(payload + "\n", encoding="utf-8")
    print(payload)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
