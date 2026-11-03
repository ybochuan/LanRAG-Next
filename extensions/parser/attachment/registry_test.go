package attachment

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func office(t *testing.T, part string, entries map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	entries2 := map[string][]byte{"[Content_Types].xml": []byte(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`), part: []byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Sample</w:t></w:r></w:p></w:body></w:document>`)}
	for k, v := range entries {
		entries2[k] = v
	}
	for k, v := range entries2 {
		w, e := z.Create(k)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(v); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}

func pdf() []byte {
	objects := []string{`<< /Type /Catalog /Pages 2 0 R >>`, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`, `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>`, "<< /Length 38 >>\nstream\nBT /F1 12 Tf 10 100 Td (Sample) Tj ET\nendstream", `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, s := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, s)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, n := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", n)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), start)
	return b.Bytes()
}

func TestOfficeRegistryFiltersActualTypesAndStrictSize(t *testing.T) {
	for _, tc := range []struct{ name, part, dir string }{{"parent.docx", "word/document.xml", "word/embeddings/"}, {"parent.xlsx", "xl/workbook.xml", "xl/embeddings/"}, {"parent.pptx", "ppt/presentation.xml", "ppt/embeddings/"}} {
		t.Run(tc.name, func(t *testing.T) {
			entries := map[string][]byte{tc.dir + "accepted.PDF": pdf(), tc.dir + "nested.docx": office(t, "word/document.xml", nil), tc.dir + "oleObject.bin": []byte("OLE"), tc.dir + "bad.pdf": []byte("not a PDF"), tc.dir + "exactly-limit.pdf": append([]byte("%PDF-"), make([]byte, MaxFileBytes-5)...), "unrelated/hidden.pdf": pdf()}
			files, e := DefaultRegistry().Extract(tc.name, office(t, tc.part, entries))
			if e != nil {
				t.Fatal(e)
			}
			if len(files) != 2 {
				t.Fatalf("got %d allowed attachments", len(files))
			}
			for _, f := range files {
				if len(f.Data) >= MaxFileBytes || !Allowed(f.Name, f.Data) {
					t.Fatal("invalid accepted attachment")
				}
			}
		})
	}
}

func TestPDFExtractsRawEmbeddedOffice(t *testing.T) {
	child := office(t, "word/document.xml", nil)
	file := filepath.Join(t.TempDir(), "child.docx")
	if e := os.WriteFile(file, child, 0600); e != nil {
		t.Fatal(e)
	}
	var b bytes.Buffer
	if e := api.AddAttachments(context.Background(), bytes.NewReader(pdf()), &b, []string{file}, false, model.NewStatelessConfiguration()); e != nil {
		t.Fatal(e)
	}
	if _, e := api.Attachments(context.Background(), bytes.NewReader(b.Bytes()), model.NewStatelessConfiguration()); e != nil {
		t.Fatalf("synthetic PDF attachments: %v", e)
	}
	files, e := DefaultRegistry().Extract("parent.pdf", b.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	if len(files) != 1 || files[0].Name != "child.docx" || !bytes.Equal(files[0].Data, child) {
		t.Fatal("raw attachment bytes changed")
	}
}

func TestPDFSkipsOversizedAttachmentsWithoutLosingSmallFiles(t *testing.T) {
	root := t.TempDir()
	files := []string{}
	small := pdf()
	for _, item := range []struct {
		name string
		data []byte
	}{
		{"small.pdf", small},
		{"exact-limit.pdf", append([]byte("%PDF-"), make([]byte, MaxFileBytes-5)...)},
		{"over-limit.pdf", append([]byte("%PDF-"), make([]byte, MaxFileBytes-4)...)},
	} {
		name := filepath.Join(root, item.name)
		if err := os.WriteFile(name, item.data, 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, name)
	}
	var source bytes.Buffer
	if err := api.AddAttachments(context.Background(), bytes.NewReader(pdf()), &source, files, false, model.NewStatelessConfiguration()); err != nil {
		t.Fatal(err)
	}
	result, err := DefaultRegistry().Extract("parent.pdf", source.Bytes())
	if err != nil {
		t.Fatalf("oversized attachment blocked parent/small attachment: %v", err)
	}
	if len(result) != 1 || result[0].Name != "small.pdf" || !bytes.Equal(result[0].Data, small) {
		t.Fatal("strict limit must skip only the oversized attachments")
	}
}

func TestDocxAnnexPreservesParagraphsAndTablesWithoutSplittingReferences(t *testing.T) {
	xml := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>MAIN TEXT, see 附件1</w:t></w:r></w:p><w:p><w:r><w:t>附件1</w:t></w:r></w:p><w:p><w:r><w:t>FIRST ONLY</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>TABLE ONLY</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p><w:r><w:t>附件2-1</w:t></w:r></w:p><w:p><w:r><w:t>SECOND ONLY</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`
	files, e := DefaultRegistry().Extract("parent.docx", office(t, "word/document.xml", map[string][]byte{"word/document.xml": []byte(xml)}))
	if e != nil {
		t.Fatal(e)
	}
	if len(files) != 2 {
		t.Fatalf("annex count %d", len(files))
	}
	for i, f := range files {
		body := documentXML(t, f.Data)
		if strings.Contains(body, "MAIN TEXT") {
			t.Fatal("main content copied into annex")
		}
		if i == 0 && (!strings.Contains(body, "TABLE ONLY") || strings.Contains(body, "SECOND ONLY")) {
			t.Fatal("table/annex boundary lost")
		}
	}
}

func documentXML(t *testing.T, data []byte) string {
	t.Helper()
	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			b, e := io.ReadAll(r)
			r.Close()
			if e != nil {
				t.Fatal(e)
			}
			return string(b)
		}
	}
	t.Fatal("no document.xml")
	return ""
}

func TestRealSuppliedDocxAnnexes(t *testing.T) {
	name := os.Getenv("AGENTRAG_ATTACHMENT_SAMPLE")
	if name == "" {
		t.Skip("opt-in user sample, never copied to repository")
	}
	data, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	embedded, e := (OfficeExtractor{}).Extract(data)
	if e != nil {
		t.Fatal(e)
	}
	if len(embedded) != 0 {
		t.Fatal("expected no binary embeddings in supplied sample")
	}
	files, e := DefaultRegistry().Extract("sample.docx", data)
	if e != nil {
		t.Fatal(e)
	}
	if len(files) != 6 {
		t.Fatalf("expected six explicit annex sections, got %d", len(files))
	}
	tables := 0
	for _, f := range files {
		tables += strings.Count(documentXML(t, f.Data), "<w:tbl>")
	}
	if tables != 4 {
		t.Fatalf("expected all four annex tables, got %d", tables)
	}
	t.Logf("sample: %d independent annexes, %d preserved tables; source never modified", len(files), tables)
}
