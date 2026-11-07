#!/usr/bin/env python3
"""P2-B read concurrency benchmark for the live AgentRAG Core.

Read-only: issues GET requests only. Credentials are read from the launcher's
private env and are never printed.
"""
from __future__ import annotations

import argparse
import concurrent.futures
import json
import re
import statistics
import threading
import time
from pathlib import Path

import requests


def load_private_env(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        match = re.match(r"export\s+([A-Z0-9_]+)\s*=\s*(.*)$", line.strip())
        if not match:
            continue
        values[match.group(1)] = match.group(2).strip().strip("'\"")
    return values


def percentile(values: list[float], p: float) -> float:
    ordered = sorted(values)
    if not ordered:
        return 0.0
    idx = min(len(ordered) - 1, max(0, int(round((p / 100.0) * len(ordered) + 0.5)) - 1))
    return ordered[idx]


class Worker:
    def __init__(self, base: str, api_key: str, tenant: str):
        self.base = base
        self.headers = {"X-API-Key": api_key, "X-Tenant-ID": tenant}
        self.local = threading.local()

    def session(self) -> requests.Session:
        session = getattr(self.local, "session", None)
        if session is None:
            session = requests.Session()
            session.headers.update(self.headers)
            self.local.session = session
        return session

    def get(self, path: str) -> tuple[int, int, float]:
        started = time.perf_counter()
        try:
            response = self.session().get(self.base + path, timeout=30)
            elapsed_ms = (time.perf_counter() - started) * 1000.0
            return response.status_code, len(response.content), elapsed_ms
        except Exception as exc:  # measurement failure is recorded, not hidden
            elapsed_ms = (time.perf_counter() - started) * 1000.0
            return -1, 0, elapsed_ms


def bench_level(worker: Worker, path: str, concurrency: int, total: int) -> dict:
    for _ in range(2):
        worker.get(path)

    durations: list[float] = []
    statuses: dict[str, int] = {}
    sizes: list[int] = []
    started = time.perf_counter()

    def run(_: int):
        return worker.get(path)

    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures = [pool.submit(run, i) for i in range(total)]
        for future in concurrent.futures.as_completed(futures):
            status, size, elapsed = future.result()
            statuses[str(status)] = statuses.get(str(status), 0) + 1
            durations.append(elapsed)
            sizes.append(size)

    wall_ms = (time.perf_counter() - started) * 1000.0
    return {
        "concurrency": concurrency,
        "n": total,
        "statuses": statuses,
        "wall_ms": round(wall_ms, 2),
        "p50_ms": round(statistics.median(durations), 2) if durations else 0.0,
        "p95_ms": round(percentile(durations, 95), 2),
        "max_ms": round(max(durations), 2) if durations else 0.0,
        "response_bytes": sorted(set(sizes)),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--env-file", default="/mnt/d/LanRAG-Next/data/launcher-run-lY5BJT/private.env")
    parser.add_argument("--out", default="/mnt/d/LanRAG-Next/data/p2b-read-concurrency.json")
    parser.add_argument("--levels", default="1,2,4,8,16")
    parser.add_argument("--requests-per-level", type=int, default=20)
    args = parser.parse_args()

    env = load_private_env(Path(args.env_file))
    base = env["AGENTRAG_WEKNORA_URL"].rstrip("/")
    kb = env["AGENTRAG_KB_IDS"].split(",")[0].strip()
    worker = Worker(base, env["AGENTRAG_WEKNORA_API_KEY"], env.get("AGENTRAG_TENANT_ID", "1"))
    endpoints = {
        "knowledge_bases": "/api/v1/knowledge-bases",
        "knowledge_list": f"/api/v1/knowledge-bases/{kb}/knowledge?page=1&page_size=20",
    }
    levels = [int(x.strip()) for x in args.levels.split(",") if x.strip()]
    result = {
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "base": base,
        "database": env.get("DB_PATH"),
        "endpoints": {},
    }
    for name, path in endpoints.items():
        rows = []
        for level in levels:
            rows.append({"path": path, **bench_level(worker, path, level, args.requests_per_level)})
        result["endpoints"][name] = rows
    Path(args.out).write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
