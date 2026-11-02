package parser

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"testing"
)

// These are actual PDF files with xref tables, page trees, resources and streams.
// Unused Font/Image resources deliberately occur on every page.
func pagePDF(streams ...string) []byte {
	return pagePDFWithFont("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", streams...)
}

func pagePDFWithFont(fontDict string, streams ...string) []byte {
	n := len(streams)
	font, image := 3+2*n, 4+2*n
	kids := ""
	for i := range streams {
		kids += fmt.Sprintf("%d 0 R ", 3+2*i)
	}
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, n)}
	for _, stream := range streams {
		contentID := len(objects) + 2
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /Font << /F1 %d 0 R >> /XObject << /Im1 %d 0 R >> >> /Contents %d 0 R >>", font, image, contentID), fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	objects = append(objects, fontDict, "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x00\nendstream")
	return pdfObjects(objects)
}

func pdfObjects(objects []string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}

func TestPreflightRealPDFTextLayersNeverRouteToOCR(t *testing.T) {
	for _, stream := range []string{
		"BT /F1 12 Tf (Readable text) Tj ET",
		"BT /F1 12 Tf [(Real) -50 (text)] TJ ET",
		"BT /F1 12 Tf <48656c6c6f> Tj ET",
		"BT /F1 12 Tf 3 Tr (Already OCRed searchable layer) Tj ET",
		"BT /F1 12 Tf (First) Tj (Second) ' 1 2 (Third) \" ET",
	} {
		t.Run(stream, func(t *testing.T) {
			report, err := PreflightPDF(context.Background(), pagePDF(stream))
			if err != nil || report.TotalPages != 1 || report.TextLayerPages != 1 || report.ImageOnlyPages != 0 {
				t.Fatalf("report=%+v err=%v", report, err)
			}
			decision, err := DecidePDF(report)
			if err != nil || decision.Tier != TierFlash || decision.OCRMode != "txt" {
				t.Fatalf("unsafe decision=%+v err=%v", decision, err)
			}
		})
	}
}

func TestPreflightCountsMixedAndImageOnlyPages(t *testing.T) {
	text, image := "BT /F1 12 Tf (Text) Tj ET", "q 100 0 0 100 0 0 cm /Im1 Do Q"
	r, err := PreflightPDF(context.Background(), pagePDF(text, image, text))
	if err != nil || r.TotalPages != 3 || r.TextLayerPages != 2 || r.ImageOnlyPages != 1 {
		t.Fatalf("mixed=%+v err=%v", r, err)
	}
	d, err := DecidePDF(r)
	if err != nil || d.Tier != TierFlash || d.OCRMode != "txt" || !d.ReviewRequired {
		t.Fatalf("mixed policy=%+v err=%v", d, err)
	}
	r, err = PreflightPDF(context.Background(), pagePDF(image, image))
	if err != nil || r.TextLayerPages != 0 || r.ImageOnlyPages != 2 {
		t.Fatalf("scan=%+v err=%v", r, err)
	}
	d, err = DecidePDF(r)
	if err != nil || d.Tier != TierBasic || d.OCRMode != "ocr" {
		t.Fatalf("scan policy=%+v err=%v", d, err)
	}
}

func TestDecidePDFSignalsAlwaysUseTheNoDownloadRule(t *testing.T) {
	for _, pages := range [][]PageSignals{{{DenseGraphics: true, MultiColumn: true}}, {{}, {Scanned: true}}, {{Scanned: true}, {}, {Scanned: true}}} {
		d, err := Decide("pdf", pages)
		if err != nil || d.Tier != TierFlash || d.OCRMode != "txt" {
			t.Fatalf("PDF signal route=%+v err=%v", d, err)
		}
	}
	if _, err := Decide("pdf", []PageSignals{{NoText: true}}); err == nil {
		t.Fatal("no text alone does not prove a scanned image")
	}
}

func TestPreflightDoesNotMistakeUnusedFontsOrEmptyTextForTextLayers(t *testing.T) {
	for _, stream := range []string{"", "BT /F1 12 Tf ET", "BT /F1 12 Tf () Tj [( ) 1 <>] TJ ET", "% BT /F1 12 Tf (fake) Tj ET\n", "0 0 10 10 re f"} {
		r, err := PreflightPDF(context.Background(), pagePDF(stream))
		if err != nil || r.TextLayerPages != 0 || r.ImageOnlyPages != 0 {
			t.Fatalf("false text: %+v, %v", r, err)
		}
		if d, err := DecidePDF(r); err == nil {
			t.Fatalf("ambiguous page must not start OCR: %+v", d)
		}
	}
}

func TestPreflightRejectsInvalidCancelledAndUnsupportedPDF(t *testing.T) {
	if _, err := PreflightPDF(context.Background(), []byte("not a PDF")); err == nil {
		t.Fatal("accepted non PDF")
	}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PreflightPDF(c, testPDF()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if _, err := PreflightPDF(context.Background(), pagePDF("BI /W 1 /H 1 /BPC 8 /CS /G ID x EI")); err == nil {
		t.Fatal("ambiguous inline bytes must not be interpreted as PDF text")
	}
}

func TestPreflightEncodedLowGlyphsAndUncertainTextNeverEnableOCR(t *testing.T) {
	font := "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding << /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences [1 /A /B /C] >> >>"
	r, err := PreflightPDF(context.Background(), pagePDFWithFont(font, "/Im1 Do BT /F1 12 Tf <010203> Tj ET"))
	if err != nil || r.TextLayerPages != 1 {
		t.Fatalf("encoded real text missed: %+v %v", r, err)
	}
	d, err := DecidePDF(r)
	if err != nil || d.OCRMode != "txt" {
		t.Fatalf("encoded text enabled OCR: %+v %v", d, err)
	}
	r, err = PreflightPDF(context.Background(), pagePDF("/Im1 Do BT /F1 12 Tf <000000> Tj ET"))
	if err == nil {
		if d, err := DecidePDF(r); err == nil && d.OCRMode != "txt" {
			t.Fatalf("uncertain glyphs treated as scan: %+v", d)
		}
	}
}

func TestPreflightReadsCompressedFormTextAndRejectsDecodeBomb(t *testing.T) {
	for _, bomb := range []bool{false, true} {
		stream := "BT /F1 12 Tf (Text inside compressed Form) Tj ET"
		if bomb {
			stream = string(bytes.Repeat([]byte(" "), preflightContentLimit+1))
		}
		var compressed bytes.Buffer
		z := zlib.NewWriter(&compressed)
		_, _ = z.Write([]byte(stream))
		_ = z.Close()
		objects := []string{
			"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Fm1 5 0 R >> >> /Contents 4 0 R >>",
			"<< /Length 7 >>\nstream\n/Fm1 Do\nendstream",
			fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /Font << /F1 6 0 R >> >> /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", compressed.Len(), compressed.String()),
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		}
		r, err := PreflightPDF(context.Background(), pdfObjects(objects))
		if bomb {
			if err == nil {
				t.Fatal("decoded content exceeded budget")
			}
			continue
		}
		if err != nil || r.TextLayerPages != 1 {
			t.Fatalf("compressed form=%+v err=%v", r, err)
		}
	}
}

func actualTextPDF(named bool) []byte {
	stream := "/Span << /ActualText (Searchable replacement text) >> BDC /Im1 Do EMC"
	properties := ""
	if named {
		stream = "/Span /P1 BDC /Im1 Do EMC"
		properties = "/Properties << /P1 << /ActualText <FEFF004100420043> >> >>"
	}
	return pdfObjects([]string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Im1 5 0 R >> %s >> /Contents 4 0 R >>", properties),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x00\nendstream",
	})
}

func TestPreflightActualTextIsNeverClassifiedAsImageOnly(t *testing.T) {
	for _, named := range []bool{false, true} {
		r, err := PreflightPDF(context.Background(), actualTextPDF(named))
		if err != nil || r.TextLayerPages != 1 || r.ImageOnlyPages != 0 {
			t.Fatalf("ActualText named=%v report=%+v err=%v", named, r, err)
		}
		d, err := DecidePDF(r)
		if err != nil || d.Tier != TierFlash || d.OCRMode != "txt" {
			t.Fatalf("ActualText triggered OCR: %+v %v", d, err)
		}
	}
}

func TestPreflightTaggedStructureCannotSilentlyGrantOCR(t *testing.T) {
	stream := "/Span << /MCID 0 >> BDC /Im1 Do EMC"
	source := pdfObjects([]string{
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /StructParents 0 /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x00\nendstream",
		"<< /Type /StructTreeRoot /K [7 0 R] /ParentTree 8 0 R /ParentTreeNextKey 1 >>",
		"<< /Type /StructElem /S /Span /P 6 0 R /Pg 3 0 R /K 0 /ActualText (Searchable structure text) >>",
		"<< /Nums [0 [7 0 R]] >>",
	})
	r, err := PreflightPDF(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if d, err := DecidePDF(r); err == nil && d.OCRMode != "txt" {
		t.Fatalf("unresolved structure text granted OCR: %+v", d)
	}
}

func TestPreflightUnknownOperatorCannotGrantOCR(t *testing.T) {
	if _, err := PreflightPDF(context.Background(), pagePDF("/Im1 Do UnrecognizedOperation")); err == nil {
		t.Fatal("unknown content cannot prove a pure-image scan")
	}
}
