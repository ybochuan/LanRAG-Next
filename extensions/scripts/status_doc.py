#!/usr/bin/env python3
"""Generate the single dynamic status index from TASK_BACKLOG.md.

The backlog is the source of truth for tasks. This script reads its status-table
rows (ID/status in the first cell) and optional YAML front matter from linked
evidence files, then writes a deterministic STATUS.md. It never modifies the
backlog or any evidence file.
"""

from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BACKLOG = ROOT / "TASK_BACKLOG.md"
STATUS = ROOT / "STATUS.md"
EVIDENCE = ROOT / "docs" / "evidence"
DATE_RE = re.compile(r"(20\d{2}-\d{2}-\d{2})")
LINK_RE = re.compile(r"\[[^\]]*\]\((docs/evidence/[^)#]+)(?:#[^)]*)?\)")
HEADER_CELLS = {"ID／状态", "任务／状态", "任务 / 状态", "ID", "状态"}
MAX_CAPABILITY = 160


@dataclass(frozen=True)
class Entry:
    section: str
    identifier: str
    status: str
    capability: str
    evidence: tuple[str, ...]
    verified: str
    line: int


def _cells(line: str) -> list[str]:
    body = line.strip()
    if body.startswith("|"):
        body = body[1:]
    if body.endswith("|"):
        body = body[:-1]
    return [cell.strip() for cell in body.split("|")]


def _clean_markdown(value: str) -> str:
    value = LINK_RE.sub(lambda match: match.group(1), value)
    value = value.replace("**", "").replace(chr(96), "")
    value = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", value)
    return re.sub(r"\s+", " ", value).strip()


def _front_matter(path: Path) -> dict[str, str]:
    try:
        text = path.read_text(encoding="utf-8")
    except OSError:
        return {}
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        return {}
    fields: dict[str, str] = {}
    for line in lines[1:]:
        if line.strip() == "---":
            return fields
        if ":" not in line:
            continue
        key, value = line.split(":", 1)
        fields[key.strip().lower()] = value.strip().strip('"').strip("'")
    return {}


def _evidence_metadata(evidence: tuple[str, ...]) -> tuple[str, str]:
    verified = ""
    notes: list[str] = []
    for raw in evidence:
        path = ROOT / raw
        metadata = _front_matter(path)
        if not verified:
            match = DATE_RE.search(metadata.get("verified_at", ""))
            if match:
                verified = match.group(1)
        status = metadata.get("status", "")
        if status:
            notes.append(status)
    return verified, "; ".join(notes)


def _parse_status_cell(cell: str) -> tuple[str, str] | None:
    if "／" in cell:
        identifier, status = cell.split("／", 1)
    elif " / " in cell:
        identifier, status = cell.split(" / ", 1)
    else:
        return None
    identifier = identifier.strip()
    status = status.strip()
    if not identifier or not status or identifier in HEADER_CELLS or status in {"状态"}:
        return None
    if not re.match(r"^[A-Za-z][A-Za-z0-9_.-]*(?:[/／][A-Za-z0-9_.-]+)?$", identifier):
        return None
    return identifier.replace("／", "/"), status


def parse_backlog(text: str) -> list[Entry]:
    entries: list[Entry] = []
    section = ""
    in_table = False
    status_table = False
    for number, line in enumerate(text.splitlines(), 1):
        if line.startswith("## "):
            section = line[3:].strip()
            in_table = False
            continue
        if not line.lstrip().startswith("|"):
            in_table = False
            continue
        cells = _cells(line)
        if len(cells) < 2:
            continue
        if not in_table:
            in_table = True
            status_table = cells[0].strip() in HEADER_CELLS
            continue
        if not status_table:
            continue
        parsed = _parse_status_cell(cells[0])
        if parsed is None:
            continue
        identifier, status = parsed
        evidence = tuple(sorted(set(LINK_RE.findall(line))))
        verified = ""
        match = DATE_RE.search(status)
        if match:
            verified = match.group(1)
        front_verified, _ = _evidence_metadata(evidence)
        if not verified:
            verified = front_verified
        if not verified:
            for raw in evidence:
                match = DATE_RE.search(raw)
                if match:
                    verified = match.group(1)
                    break
        capability = _clean_markdown(cells[1])
        if len(capability) > MAX_CAPABILITY:
            capability = capability[: MAX_CAPABILITY - 1].rstrip() + "…"
        entries.append(Entry(section, identifier, status, capability, evidence, verified, number))
    return entries


def select_current(entries: list[Entry]) -> list[Entry]:
    """Keep one row per ID: latest dated row wins; on ties the first row wins."""
    chosen: dict[str, Entry] = {}
    order: list[str] = []
    for entry in entries:
        if entry.identifier not in chosen:
            chosen[entry.identifier] = entry
            order.append(entry.identifier)
            continue
        current = chosen[entry.identifier]
        if (entry.verified, -entry.line) > (current.verified, -current.line):
            chosen[entry.identifier] = entry
    return [chosen[identifier] for identifier in order]


def render(backlog_text: str, source_label: str = "TASK_BACKLOG.md") -> str:
    entries = select_current(parse_backlog(backlog_text))
    lines = [
        "# AgentRAG 当前状态",
        "",
        "本文件由 extensions/scripts/status_doc.py 生成，请勿手工编辑。",
        "任务与验收事实仍以 TASK_BACKLOG.md 和 docs/evidence/ 为准；本文件只汇总当前条目状态。",
        "生成来源：" + source_label + "（共 " + str(len(entries)) + " 个去重条目）。校验命令：python extensions/scripts/status_doc.py --check。",
        "",
    ]
    last_section = None
    for entry in entries:
        if entry.section != last_section:
            if last_section is not None:
                lines.append("")
            lines.extend([f"## {entry.section}", "", "| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |", "|---|---|---|---|---|"])
            last_section = entry.section
        evidence = "<br>".join(f"[{Path(path).name}]({path})" for path in entry.evidence) or ""
        status = entry.status.replace("|", "\\|")
        capability = entry.capability.replace("|", "\\|")
        lines.append(f"| {entry.identifier} | {status} | {capability} | {entry.verified} | {evidence} |")
    return "\n".join(lines).rstrip() + "\n"


def check(path: Path, expected: str) -> int:
    try:
        actual = path.read_text(encoding="utf-8")
    except OSError:
        print("status doc missing: " + str(path), file=sys.stderr)
        return 1
    if actual != expected:
        print("status doc is stale: run python extensions/scripts/status_doc.py", file=sys.stderr)
        return 1
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="fail when STATUS.md is not up to date")
    parser.add_argument("--backlog", type=Path, default=BACKLOG)
    parser.add_argument("--output", type=Path, default=STATUS)
    args = parser.parse_args(argv)
    text = args.backlog.read_text(encoding="utf-8")
    rendered = render(text, source_label=args.backlog.name)
    if args.check:
        return check(args.output, rendered)
    args.output.write_text(rendered, encoding="utf-8")
    print("wrote " + str(args.output) + " (" + str(len(select_current(parse_backlog(text)))) + " entries)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
