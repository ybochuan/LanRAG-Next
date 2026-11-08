package attachment

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func docxWithParagraphs(t *testing.T, paragraphs string) []byte {
	t.Helper()
	body := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + paragraphs + `</w:body></w:document>`
	return office(t, "word/document.xml", map[string][]byte{"word/document.xml": []byte(body)})
}

func semanticParagraph(text string, heading bool) string {
	style := ""
	if heading {
		style = `<w:pPr><w:pStyle w:val="Heading1"/></w:pPr>`
	}
	return `<w:p>` + style + `<w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

func TestAnalyzeMapsDocxTitlesToParentReferences(t *testing.T) {
	parent := docxWithParagraphs(t, strings.Join([]string{
		semanticParagraph("第三章 结算账户管理", true),
		semanticParagraph("开户后5日内，应提供《结算账户管理协议》和《代扣代缴税款协议》。", false),
		semanticParagraph("附件2：合法性审查书面声明", false),
		semanticParagraph("附件1：账户管理协议", false),
	}, ""))
	files := []File{
		{Name: "annex-02.docx", Data: docxWithParagraphs(t, semanticParagraph("合法性审查书面声明", true))},
		{Name: "annex-04.docx", Data: docxWithParagraphs(t, semanticParagraph("代扣代缴税款协议", true))},
		{Name: "annex-03.docx", Data: docxWithParagraphs(t, semanticParagraph("账户管理协议", true))},
		{Name: "scan-unknown.docx", Data: docxWithParagraphs(t, semanticParagraph("未识别模板", false))},
	}
	got := Analyze("parent.docx", parent, "中国邮政储蓄银行公司业务上门服务管理办法", files)
	if len(got) != 4 {
		t.Fatalf("got %d semantics", len(got))
	}
	want := []struct {
		display  string
		status   string
		sequence int
	}{
		{"合法性审查书面声明", MappingConfirmed, 2},
		{"代扣代缴税款协议", MappingConfirmed, 4},
		{"账户管理协议", MappingConfirmed, 3},
		{"未识别模板", MappingUnmatched, 4},
	}
	for i, item := range got {
		if item.DisplayName != want[i].display || item.MappingStatus != want[i].status || item.AttachmentSequence != want[i].sequence {
			t.Fatalf("item %d = %+v, want %+v", i, item, want[i])
		}
		if item.OriginalFilename != files[i].Name {
			t.Fatalf("item %d original filename = %q, want %q", i, item.OriginalFilename, files[i].Name)
		}
		if item.ParentTitle != "中国邮政储蓄银行公司业务上门服务管理办法" {
			t.Fatalf("item %d parent title = %q", i, item.ParentTitle)
		}
	}
	for _, item := range got[:3] {
		if item.ReferenceLocation != "第三章 结算账户管理" {
			t.Fatalf("reference location = %q", item.ReferenceLocation)
		}
	}
	if got[3].ReferenceTitle != "" || got[3].ReferenceLocation != "" {
		t.Fatalf("unmatched item kept references: %+v", got[3])
	}
	sum := sha256.Sum256(parent)
	if got[0].ParentFileHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("parent hash mismatch")
	}
}

func TestAnalyzeMarksAmbiguousTitleAsCandidate(t *testing.T) {
	parent := docxWithParagraphs(t, semanticParagraph("附件包括：《附件格式》", false))
	files := []File{
		{Name: "annex-01.docx", Data: docxWithParagraphs(t, semanticParagraph("附件格式", true))},
		{Name: "annex-02.docx", Data: docxWithParagraphs(t, semanticParagraph("附件格式", true))},
	}
	got := Analyze("parent.docx", parent, "", files)
	for i, item := range got {
		if item.MappingStatus != MappingCandidate {
			t.Fatalf("item %d status = %q, want candidate", i, item.MappingStatus)
		}
		if item.MappingConfidence != 0.85 {
			t.Fatalf("item %d confidence = %v, want 0.85", i, item.MappingConfidence)
		}
	}
}

func TestAnalyzeFallsBackForNonDocxParent(t *testing.T) {
	files := []File{{Name: "annex-01.docx", Data: docxWithParagraphs(t, semanticParagraph("合法性审查书面声明", true))}}
	got := Analyze("parent.pdf", []byte("not a docx"), "制度", files)
	if len(got) != 1 || got[0].MappingStatus != MappingUnmatched || got[0].DisplayName != "合法性审查书面声明" {
		t.Fatalf("unexpected fallback: %+v", got)
	}
}
