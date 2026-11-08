#!/usr/bin/env python3
"""RAG isolation experiment: search-only vs search+list vs search+Host overview."""
from __future__ import annotations

import argparse
import itertools
import json
import re
import statistics
import threading
import time
from pathlib import Path

import requests


def load_env(path: Path) -> dict[str, str]:
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


_query_seq = itertools.count(1)


def request_once(session: requests.Session, spec: dict) -> tuple[int | None, float, str | None]:
    payload = spec.get("json")
    if spec.get("query_prefix"):
        payload = dict(payload or {})
        payload["query"] = f"{spec['query_prefix']}（隔离测试 {next(_query_seq)}）"
    started = time.perf_counter()
    try:
        response = session.request(
            spec["method"], spec["url"], json=payload, timeout=90
        )
        elapsed = (time.perf_counter() - started) * 1000.0
        return response.status_code, elapsed, None
    except Exception as exc:  # noqa: BLE001 - experiment records failures
        elapsed = (time.perf_counter() - started) * 1000.0
        return None, elapsed, type(exc).__name__


def run_group(name: str, specs: dict[str, dict], duration: float) -> dict:
    stop = threading.Event()
    stats: dict[str, list[float]] = {task: [] for task in specs}
    statuses: dict[str, dict[str, int]] = {task: {} for task in specs}
    errors: dict[str, list[str]] = {task: [] for task in specs}
    lock = threading.Lock()

    def worker(task: str, spec: dict) -> None:
        session = requests.Session()
        session.headers.update(spec["headers"])
        while not stop.is_set():
            status, elapsed, error = request_once(session, spec)
            with lock:
                stats[task].append(elapsed)
                if status is not None:
                    statuses[task][str(status)] = statuses[task].get(str(status), 0) + 1
                if error:
                    errors[task].append(error)

    threads = []
    for task, spec in specs.items():
        for _ in range(spec["workers"]):
            threads.append(threading.Thread(target=worker, args=(task, spec), daemon=True))
    for thread in threads:
        thread.start()
    time.sleep(duration)
    stop.set()
    for thread in threads:
        thread.join(timeout=15)

    result = {"duration_s": duration, "tasks": {}}
    for task in specs:
        values = stats[task]
        result["tasks"][task] = {
            "n": len(values),
            "statuses": statuses[task],
            "errors": errors[task][:10],
            "p50_ms": round(statistics.median(values), 2) if values else 0.0,
            "p95_ms": round(percentile(values, 95), 2),
            "max_ms": round(max(values), 2) if values else 0.0,
        }
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-dir", default="")
    parser.add_argument("--duration", type=float, default=12.0)
    parser.add_argument("--out", default="/mnt/d/LanRAG-Next/data/rag-isolation-2026-11-08.json")
    args = parser.parse_args()

    if args.run_dir:
        run_dir = Path(args.run_dir)
    else:
        candidates = sorted(
            Path("/mnt/d/LanRAG-Next/data").glob("launcher-run-*"),
            key=lambda p: p.stat().st_mtime,
            reverse=True,
        )
        run_dir = next((p for p in candidates if (p / "private.env").exists()), candidates[0])

    env = load_env(run_dir / "private.env")
    kb = env["AGENTRAG_KB_IDS"].split(",")[0].strip()
    core = env["AGENTRAG_WEKNORA_URL"].rstrip("/")
    core_headers = {
        "X-API-Key": env["AGENTRAG_WEKNORA_API_KEY"],
        "X-Tenant-ID": env.get("AGENTRAG_TENANT_ID", "1"),
    }
    host_headers = {
        "Authorization": "Bearer " + env["AGENTRAG_MANAGEMENT_KEY"],
        "X-AgentRAG-Bridge-Version": "1",
    }

    search = {
        "method": "POST",
        "url": core + "/api/v1/knowledge-search",
        "headers": core_headers,
        "json": {"query": "上门服务制度的办理要求和流程是什么？", "knowledge_base_ids": [kb]},
        "query_prefix": "上门服务制度的办理要求和流程是什么？",
        "workers": 4,
    }
    knowledge_list = {
        "method": "GET",
        "url": core + f"/api/v1/knowledge-bases/{kb}/knowledge?page=1&page_size=20",
        "headers": core_headers,
        "workers": 4,
    }
    host_overview = {
        "method": "POST",
        "url": "http://127.0.0.1:8090/management/lifecycle",
        "headers": host_headers,
        "json": {"action": "overview", "page": 1, "page_size": 20, "tenant_id": 1, "knowledge_base_id": kb},
        "workers": 4,
    }

    # Warm the embedding/rerank paths before measuring. Keep this separate from
    # measured windows so a cold first request does not become group noise.
    warmup_session = requests.Session()
    warmup_session.headers.update(core_headers)
    for _ in range(2):
        status, elapsed, error = request_once(warmup_session, search)
        print(f"warmup search status={status} elapsed_ms={elapsed:.2f} error={error}", flush=True)

    groups = {
        "search_only": {"knowledge_search": search},
        "search_list": {"knowledge_search": search, "knowledge_list": knowledge_list},
        "search_host_overview": {"knowledge_search": search, "host_overview": host_overview},
    }
    output = {
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "run_dir": str(run_dir),
        "groups": {},
    }
    for name, specs in groups.items():
        print(f"group {name} ...", flush=True)
        output["groups"][name] = run_group(name, specs, args.duration)
    out_path = Path(args.out)
    out_path.write_text(json.dumps(output, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(output, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
