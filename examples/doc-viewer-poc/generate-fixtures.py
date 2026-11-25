"""Generate reproducible synthetic DOCX files for local renderer comparison."""

import hashlib
import io
import json
import sys
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo

from docx import Document
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.shared import Cm, Pt


def paper(title):
    document = Document()
    document.core_properties.title = title
    document.core_properties.author = "LanRAG Viewer PoC"
    section = document.sections[0]
    section.page_width = Cm(21)
    section.page_height = Cm(29.7)
    section.left_margin = section.right_margin = Cm(2)
    section.top_margin = section.bottom_margin = Cm(2)
    section.header.paragraphs[0].text = "LanRAG 文档阅读器 · 合成样本"
    footer = section.footer.paragraphs[0]
    footer.add_run("合成参考页 ")
    page_field = OxmlElement("w:fldSimple")
    page_field.set(qn("w:instr"), "PAGE")
    footer._p.append(page_field)
    for name in ("Normal", "Title", "Heading 1", "Heading 2"):
        style = document.styles[name]
        style.font.name = "SimSun"
        style.element.get_or_add_rPr().rFonts.set(qn("w:eastAsia"), "SimSun")
    body = document.styles["Normal"]
    body.font.size = Pt(11)
    body.paragraph_format.line_spacing = 1.25
    document.add_heading(title, 0)
    return document


def policy():
    document = paper("合成资料归档制度")
    for chapter, subject in enumerate(("总则", "分类与归档", "复核流程", "附则"), 1):
        document.add_heading(f"第 {chapter} 章  {subject}", 1)
        for item in range(1, 7):
            document.add_heading(f"第 {chapter}.{item:02d} 条  归档检查", 2)
            document.add_paragraph(
                f"测试记录 {chapter:02d}-{item:02d}。资料整理须依照清单完成分类、复查和记录。"
                "同一批材料应保留页序、来源和复核时间。复查人员发现编号不一致时应标明原因，"
                "报告应包括检查范围、异常说明与后续处理。此处为合成文本，仅用于排版核对。"
            )
    document.add_paragraph("制度末尾核验")
    return document


def form():
    document = paper("合成跨页检查表")
    document.add_paragraph("核对列宽、跨页表头、合并单元格、行序和表尾。")
    table = document.add_table(rows=1, cols=4)
    table.style = "Table Grid"
    table.autofit = False
    widths = (2.4, 2.1, 9.6, 2.9)
    for column, width in zip(table.columns, widths):
        column.width = Cm(width)
    header = table.rows[0]
    header._tr.get_or_add_trPr().append(OxmlElement("w:tblHeader"))
    for cell, label, width in zip(header.cells, ("批次", "序号", "检查事项", "结果"), widths):
        cell.width = Cm(width)
        cell.text = label
    for index in range(1, 43):
        row = table.add_row()
        row._tr.get_or_add_trPr().append(OxmlElement("w:cantSplit"))
        for cell, width in zip(row.cells, widths):
            cell.width = Cm(width)
        row.cells[0].text = "复核组"
        row.cells[1].text = f"{index:02d}"
        row.cells[2].text = f"检查点 {index:02d}。核对材料分类、页面顺序和复查记录，保留标记与备注。"
        row.cells[3].text = "待复核"
    table.cell(1, 0).merge(table.cell(3, 0)).text = "前三项合并"
    table.rows[-1].cells[2].text = "检查点 42。表格末行核验"
    document.add_paragraph("表单末尾核验")
    return document


def write_docx(document, target):
    buffer = io.BytesIO()
    document.save(buffer)
    with ZipFile(buffer) as source, ZipFile(target, "w", compression=ZIP_DEFLATED) as output:
        for name in source.namelist():
            output.writestr(ZipInfo(name, (2026, 11, 25, 0, 0, 0)), source.read(name), ZIP_DEFLATED)
    content = target.read_bytes()
    return {"file": target.name, "bytes": len(content), "sha256": hashlib.sha256(content).hexdigest()}


if __name__ == "__main__":
    output_dir = Path(sys.argv[1])
    output_dir.mkdir(parents=True, exist_ok=True)
    fixtures = [
        write_docx(policy(), output_dir / "policy.docx"),
        write_docx(form(), output_dir / "form.docx"),
    ]
    (output_dir / "manifest.json").write_text(
        json.dumps(fixtures, indent=2) + "\n", encoding="utf-8"
    )
