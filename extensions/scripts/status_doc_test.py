from __future__ import annotations

import importlib.util
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("status_doc", Path(__file__).with_name("status_doc.py"))
status_doc = importlib.util.module_from_spec(spec)
import sys
sys.modules[spec.name] = status_doc
spec.loader.exec_module(status_doc)


BACKLOG = """# Tasks

## Workstream A

| ID／状态 | 期望结果 | 依据与验证 | 何时重新审视 |
|---|---|---|---|
| A-1／已完成（2026-11-01） | 第一项能力，见[证据](docs/evidence/a-2026-11-01.md)。 | x | y |
| A-2／进行中 | 第二项能力。 | x | y |

## Workstream B

| 任务／状态 | 期望结果 | 依据 | 验证 |
|---|---|---|---|
| B-1／阻塞 | 第三项能力。 | x | y |
"""


class StatusDocTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        (self.root / "docs" / "evidence").mkdir(parents=True)
        self.old_root = status_doc.ROOT
        status_doc.ROOT = self.root

    def tearDown(self) -> None:
        status_doc.ROOT = self.old_root
        self.tmp.cleanup()

    def test_renders_sections_and_links(self) -> None:
        rendered = status_doc.render(BACKLOG)
        self.assertIn("## Workstream A", rendered)
        self.assertIn("| A-1 | 已完成（2026-11-01） |", rendered)
        self.assertIn("[a-2026-11-01.md](docs/evidence/a-2026-11-01.md)", rendered)
        self.assertIn("## Workstream B", rendered)
        self.assertIn("| B-1 | 阻塞 |", rendered)

    def test_duplicate_id_uses_latest_dated_row(self) -> None:
        text = BACKLOG.replace(
            "| A-2／进行中 | 第二项能力。 | x | y |",
            "| A-1／已完成（2026-11-03） | 更新后的能力。 | x | y |\n| A-2／进行中 | 第二项能力。 | x | y |",
        )
        entries = status_doc.select_current(status_doc.parse_backlog(text))
        a1 = [entry for entry in entries if entry.identifier == "A-1"]
        self.assertEqual(len(a1), 1)
        self.assertIn("2026-11-03", a1[0].status)
        self.assertEqual(a1[0].capability, "更新后的能力。")

    def test_front_matter_supplies_verification_date(self) -> None:
        (self.root / "docs" / "evidence" / "a-2026-11-01.md").write_text(
            "---\nstatus: verified\nverified_at: 2026-11-02\n---\n# Evidence\n", encoding="utf-8"
        )
        rendered = status_doc.render(BACKLOG)
        self.assertIn("| A-1 | 已完成（2026-11-01） | 第一项能力，见docs/evidence/a-2026-11-01.md。 | 2026-11-01 |", rendered)

    def test_check_detects_stale_output(self) -> None:
        output = self.root / "STATUS.md"
        output.write_text("old", encoding="utf-8")
        self.assertEqual(status_doc.check(output, status_doc.render(BACKLOG)), 1)
        output.write_text(status_doc.render(BACKLOG), encoding="utf-8")
        self.assertEqual(status_doc.check(output, status_doc.render(BACKLOG)), 0)


if __name__ == "__main__":
    unittest.main()
