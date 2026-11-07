#!/usr/bin/env python3
"""P2-B mixed read workload: RAG search + lifecycle overview + KB list."""
from __future__ import annotations

import argparse
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
        if match:
            values[match.group(1)] = match.group(2).strip().strip("'\"")
    return values


def percentile(values: list[float], p: float) -> float:
    ordered = sorted(values)
    if not ordered:
        return 0.0
    idx = min(len(ordered) - 1, max(0, int(round((p / 100.0) * len(ordered) + 0.5)) - 1))
    return ordered[idx]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--env-file", default="/mnt/d/LanRAG-Next/data/launcher-run-lY5BJT/private.env")
    parser.add_argument("--duration", type=float, default=15.0)
    parser.add_argument("--out", default="/mnt/d/LanRAG-Next/data/p2b-mixed.json")
    args = parser.parse_args()
    env = load_private_env(Path(args.env_file))
    kb = env["AGENTRAG_KB_IDS"].split(",")[0].strip()
    core = env["AGENTRAG_WEKNORA_URL"].rstrip("/")
    core_headers = {"X-API-Key": env["AGENTRAG_WEKNORA_API_KEY"], "X-Tenant-ID": env.get("AGENTRAG_TENANT_ID", "1")}
    host_headers = {"Authorization": "Bearer " + env["AGENTRAG_MANAGEMENT_KEY"], "X-AgentRAG-Bridge-Version": "1"}

    tasks = {
        "knowledge_search": {
            "method": "POST",
            "url": core + "/api/v1/knowledge-search",
            "headers": core_headers,
            "json": {"query": "上门服务制度的办理要求和流程是什么？", "knowledge_base_ids": [kb]},
            "workers": 4,
        },
        "host_overview": {
            "method": "POST",
            "url": "http://127.0.0.1:8090/management/lifecycle",
            "headers": host_headers,
            "json": {"action": "overview", "page": 1, "page_size": 20, "tenant_id": 1, "knowledge_base_id": kb},
            "workers": 4,
        },
        "knowledge_list": {
            "method": "GET",
            "url": core + f"/api/v1/knowledge-bases/{kb}/knowledge?page=1&page_size=20",
            "headers": core_headers,
            "workers": 4,
        },
    }
    stop = threading.Event()
    stats: dict[str, list[float]] = {name: [] for name in tasks}
    statuses: dict[str, dict[str, int]] = {name: {} for name in tasks}
    errors: dict[str, list[str]] = {name: [] for name in tasks}
    lock = threading.Lock()

    def worker(name: str, spec: dict):
        session = requests.Session()
        session.headers.update(spec["headers"])
        while not stop.is_set():
            started = time.perf_counter()
            try:
                if spec["method"] == "POST":
                    response = session.post(spec["url"], json=spec["json"], timeout=60)
                else:
                    response = session.get(spec["url"], timeout=60)
                elapsed = (time.perf_counter() - started) * 1000.0
                with lock:
                    stats[name].append(elapsed)
                    statuses[name][str(response.status_code)] = statuses[name].get(str(response.status_code), 0) + 1
            except Exception as exc:
                with lock:
                    errors[name].append(type(exc).__name__)

    threads = []
    for name, spec in tasks.items():
        for _ in range(spec["workers"]):
            threads.append(threading.Thread(target=worker, args=(name, spec), daemon=True))
    for thread in threads:
        thread.start()
    time.sleep(args.duration)
    stop.set()
    for thread in threads:
        thread.join(timeout=15)

    result = {"generated_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "duration_s": args.duration, "tasks": {}}
    for name in tasks:
        values = stats[name]
        result["tasks"][name] = {
            "n": len(values),
            "statuses": statuses[name],
            "errors": errors[name][:10],
            "p50_ms": round(statistics.median(values), 2) if values else 0.0,
            "p95_ms": round(percentile(values, 95), 2),
            "max_ms": round(max(values), 2) if values else 0.0,
        }
    Path(args.out).write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
