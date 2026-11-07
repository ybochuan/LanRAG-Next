#!/usr/bin/env python3
"""P2-B SQLite direct-read and isolated WAL write-contention probe.

Reads the live database read-only. The write-contention probe always works on
an isolated byte-for-byte copy and never opens the production database for
writing.
"""
from __future__ import annotations

import argparse
import concurrent.futures
import json
import os
import re
import shutil
import sqlite3
import statistics
import tempfile
import threading
import time
from pathlib import Path


def percentile(values: list[float], p: float) -> float:
    ordered = sorted(values)
    if not ordered:
        return 0.0
    idx = min(len(ordered) - 1, max(0, int(round((p / 100.0) * len(ordered) + 0.5)) - 1))
    return ordered[idx]


def read_query(path: str, kb: str, iterations: int) -> list[float]:
    uri = f"file:{path}?mode=ro"
    con = sqlite3.connect(uri, uri=True, timeout=10)
    try:
        con.execute("PRAGMA query_only=ON")
        durations = []
        for _ in range(iterations):
            started = time.perf_counter()
            rows = con.execute(
                "SELECT id,title,parse_status,created_at FROM knowledges "
                "WHERE knowledge_base_id=? AND deleted_at IS NULL "
                "ORDER BY created_at DESC LIMIT 20",
                (kb,),
            ).fetchall()
            durations.append((time.perf_counter() - started) * 1000.0)
            _ = len(rows)
        return durations
    finally:
        con.close()


def bench_read(path: str, kb: str, concurrency: int, iterations: int) -> dict:
    durations = []
    started = time.perf_counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures = [pool.submit(read_query, path, kb, iterations) for _ in range(concurrency)]
        for future in concurrent.futures.as_completed(futures):
            durations.extend(future.result())
    wall = (time.perf_counter() - started) * 1000.0
    return {
        "concurrency": concurrency,
        "queries": len(durations),
        "wall_ms": round(wall, 2),
        "qps": round(len(durations) / (wall / 1000.0), 1),
        "p50_ms": round(statistics.median(durations), 2),
        "p95_ms": round(percentile(durations, 95), 2),
        "max_ms": round(max(durations), 2),
    }


def make_copy(source: str, destination: str) -> None:
    os.makedirs(os.path.dirname(destination), exist_ok=True)
    src = sqlite3.connect(source)
    dst = sqlite3.connect(destination)
    try:
        src.backup(dst)
    finally:
        dst.close()
        src.close()


def write_contention_probe(copy_path: str, kb: str, writer_rows: int, blob_bytes: int, readers: int) -> dict:
    # Prepare only the isolated copy.
    writer = sqlite3.connect(copy_path, timeout=10)
    writer.execute("PRAGMA journal_mode=WAL")
    writer.execute("PRAGMA synchronous=FULL")
    writer.execute("PRAGMA busy_timeout=5000")
    writer.execute("PRAGMA wal_autocheckpoint=1000")
    writer.execute("CREATE TABLE IF NOT EXISTS p2b_blob_probe(id INTEGER PRIMARY KEY, payload BLOB NOT NULL)")
    writer.commit()

    stop = threading.Event()
    errors: list[str] = []
    reader_latencies: list[float] = []
    reader_counts = [0] * readers

    def reader(idx: int):
        con = sqlite3.connect(f"file:{copy_path}?mode=ro", uri=True, timeout=10)
        con.execute("PRAGMA query_only=ON")
        try:
            while not stop.is_set():
                started = time.perf_counter()
                try:
                    con.execute(
                        "SELECT id,title,parse_status,created_at FROM knowledges "
                        "WHERE knowledge_base_id=? AND deleted_at IS NULL "
                        "ORDER BY created_at DESC LIMIT 20",
                        (kb,),
                    ).fetchall()
                    reader_latencies.append((time.perf_counter() - started) * 1000.0)
                    reader_counts[idx] += 1
                except Exception as exc:
                    errors.append(f"reader: {exc}")
                    break
        finally:
            con.close()

    threads = [threading.Thread(target=reader, args=(i,), daemon=True) for i in range(readers)]
    for thread in threads:
        thread.start()
    time.sleep(0.2)

    writer_started = time.perf_counter()
    for i in range(writer_rows):
        try:
            writer.execute("INSERT INTO p2b_blob_probe(payload) VALUES (randomblob(?))", (blob_bytes,))
            writer.commit()
        except Exception as exc:
            errors.append(f"writer: {exc}")
            break
    writer_wall = (time.perf_counter() - writer_started) * 1000.0
    stop.set()
    for thread in threads:
        thread.join(timeout=5)
    writer.close()

    return {
        "writer_rows": writer_rows,
        "blob_bytes": blob_bytes,
        "writer_wall_ms": round(writer_wall, 2),
        "writer_ms_per_row": round(writer_wall / writer_rows, 2) if writer_rows else 0.0,
        "reader_threads": readers,
        "reader_queries": len(reader_latencies),
        "reader_p50_ms": round(statistics.median(reader_latencies), 2) if reader_latencies else 0.0,
        "reader_p95_ms": round(percentile(reader_latencies, 95), 2),
        "reader_max_ms": round(max(reader_latencies), 2) if reader_latencies else 0.0,
        "errors": errors[:20],
        "wal_bytes_after": os.path.getsize(copy_path + "-wal") if os.path.exists(copy_path + "-wal") else 0,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--db", default="/home/ybochuan/.local/share/agentrag/weknora.db")
    parser.add_argument("--kb", default="164bfd73-d231-4d5c-994e-057ac0cdf48a")
    parser.add_argument("--out", default="/mnt/d/LanRAG-Next/data/p2b-sqlite-read.json")
    parser.add_argument("--write-out", default="/mnt/d/LanRAG-Next/data/p2b-sqlite-write.json")
    parser.add_argument("--run-write", action="store_true")
    args = parser.parse_args()

    baseline = []
    for concurrency in (1, 4, 8, 16):
        baseline.append(bench_read(args.db, args.kb, concurrency, 25))
    result = {
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "db": args.db,
        "mode": "read_only",
        "baseline": baseline,
    }
    Path(args.out).write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, ensure_ascii=False, indent=2))

    if args.run_write:
        tmpdir = tempfile.mkdtemp(prefix="p2b-write-", dir="/home/ybochuan/.local/share/agentrag")
        copy_path = os.path.join(tmpdir, "weknora.db")
        make_copy(args.db, copy_path)
        try:
            write_result = write_contention_probe(copy_path, args.kb, writer_rows=12, blob_bytes=8 * 1024 * 1024, readers=4)
            write_result["copy_path"] = copy_path
            Path(args.write_out).write_text(json.dumps(write_result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
            print(json.dumps(write_result, ensure_ascii=False, indent=2))
        finally:
            shutil.rmtree(tmpdir, ignore_errors=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
