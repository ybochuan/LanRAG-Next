#!/usr/bin/env python3
"""P2-B: read latency of an isolated Core during sustained writes.

The Core instance under test listens on 127.0.0.1:18881 and points at a copy
of the production database. The writer is a separate SQLite connection to the
same copy. The production database is never written.
"""
from __future__ import annotations

import argparse
import concurrent.futures
import json
import re
import sqlite3
import statistics
import threading
import time
from pathlib import Path

import requests


def load_private_env(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        match = re.match(r"export\s+([A-Z0-9_]+)\s*=\s*(.*)$", line.strip())
        if match:
            values[match.group(1)] = match.group(2).strip().strip("'\"")
    return values


def percentile(values: list[float], p: float) -> float:
    ordered = sorted(values)
    if not ordered:
        return 0.0
    idx = min(len(ordered) - 1, max(0, int(round((p / 100.0) * len(ordered) + 0.5)) - 1))
    return ordered[idx]


def bench_get(session: requests.Session, url: str, n: int) -> dict:
    durations = []
    statuses = {}
    for _ in range(n):
        started = time.perf_counter()
        try:
            response = session.get(url, timeout=30)
            statuses[str(response.status_code)] = statuses.get(str(response.status_code), 0) + 1
        except Exception as exc:
            statuses["error:" + type(exc).__name__] = statuses.get("error:" + type(exc).__name__, 0) + 1
        durations.append((time.perf_counter() - started) * 1000.0)
    return {
        "n": n,
        "statuses": statuses,
        "p50_ms": round(statistics.median(durations), 2),
        "p95_ms": round(percentile(durations, 95), 2),
        "max_ms": round(max(durations), 2),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--env-file", default="/mnt/d/LanRAG-Next/data/launcher-run-lY5BJT/private.env")
    parser.add_argument("--db", default="/home/ybochuan/.local/share/agentrag/p2b-core/weknora.db")
    parser.add_argument("--base", default="http://127.0.0.1:18881")
    parser.add_argument("--out", default="/mnt/d/LanRAG-Next/data/p2b-core-write-contention.json")
    parser.add_argument("--rows", type=int, default=20)
    parser.add_argument("--blob-bytes", type=int, default=8 * 1024 * 1024)
    args = parser.parse_args()

    env = load_private_env(Path(args.env_file))
    kb = env["AGENTRAG_KB_IDS"].split(",")[0].strip()
    path = f"/api/v1/knowledge-bases/{kb}/knowledge?page=1&page_size=20"
    session = requests.Session()
    session.headers.update({"X-API-Key": env["AGENTRAG_WEKNORA_API_KEY"], "X-Tenant-ID": env.get("AGENTRAG_TENANT_ID", "1")})
    url = args.base.rstrip("/") + path

    # Warm up, then baseline with the same four concurrent clients.
    for _ in range(2):
        session.get(url, timeout=10)
    baseline = []
    for concurrency in (1, 4):
        workers = [requests.Session() for _ in range(concurrency)]
        for worker in workers:
            worker.headers.update(session.headers)
        started = time.perf_counter()
        counts = []
        with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
            futures = [pool.submit(bench_get, worker, url, 10) for worker in workers]
            for future in concurrent.futures.as_completed(futures):
                counts.append(future.result())
        baseline.append({
            "concurrency": concurrency,
            "wall_ms": round((time.perf_counter() - started) * 1000.0, 2),
            "runs": counts,
        })

    # Isolated writer connection on the copy.
    writer = sqlite3.connect(args.db, timeout=10)
    writer.execute("PRAGMA journal_mode=WAL")
    writer.execute("PRAGMA synchronous=FULL")
    writer.execute("PRAGMA busy_timeout=5000")
    writer.execute("PRAGMA wal_autocheckpoint=1000")
    writer.execute("CREATE TABLE IF NOT EXISTS p2b_blob_probe(id INTEGER PRIMARY KEY, payload BLOB NOT NULL)")
    writer.commit()

    stop = threading.Event()
    reader_stats: list[dict] = []
    lock_errors: list[str] = []

    def reader():
        local = requests.Session()
        local.headers.update(session.headers)
        durations = []
        statuses = {}
        while not stop.is_set():
            started = time.perf_counter()
            try:
                response = local.get(url, timeout=30)
                statuses[str(response.status_code)] = statuses.get(str(response.status_code), 0) + 1
            except Exception as exc:
                statuses["error:" + type(exc).__name__] = statuses.get("error:" + type(exc).__name__, 0) + 1
            durations.append((time.perf_counter() - started) * 1000.0)
        reader_stats.append({"n": len(durations), "statuses": statuses, "p50_ms": round(statistics.median(durations), 2) if durations else 0.0,
                             "p95_ms": round(percentile(durations, 95), 2), "max_ms": round(max(durations), 2) if durations else 0.0})

    threads = [threading.Thread(target=reader, daemon=True) for _ in range(4)]
    for thread in threads:
        thread.start()
    time.sleep(0.2)

    writer_started = time.perf_counter()
    for _ in range(args.rows):
        try:
            writer.execute("INSERT INTO p2b_blob_probe(payload) VALUES (randomblob(?))", (args.blob_bytes,))
            writer.commit()
        except Exception as exc:
            lock_errors.append(str(exc))
            break
    writer_wall_ms = (time.perf_counter() - writer_started) * 1000.0
    stop.set()
    for thread in threads:
        thread.join(timeout=10)
    writer.close()

    result = {
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "db": args.db,
        "base": args.base,
        "rows": args.rows,
        "blob_bytes": args.blob_bytes,
        "writer_wall_ms": round(writer_wall_ms, 2),
        "writer_ms_per_row": round(writer_wall_ms / args.rows, 2) if args.rows else 0.0,
        "baseline": baseline,
        "during_write": reader_stats,
        "writer_errors": lock_errors,
    }
    Path(args.out).write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
