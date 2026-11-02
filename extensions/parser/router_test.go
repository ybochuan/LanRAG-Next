package parser_test

import (
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/parser"
)

func TestDecideRasterUsesFrozenPageRules(t *testing.T) {
	tests := []struct {
		name string
		page parser.PageSignals
		tier string
	}{
		{"no complex signals", parser.PageSignals{}, "flash"},
		{"scanned", parser.PageSignals{Scanned: true}, "basic"},
		{"no text", parser.PageSignals{NoText: true}, "basic"},
		{"garbled", parser.PageSignals{Garbled: true}, "basic"},
		{"vector text", parser.PageSignals{VectorText: true}, "basic"},
		{"multiple columns", parser.PageSignals{MultiColumn: true}, "standard"},
		{"table", parser.PageSignals{TableLikely: true}, "standard"},
		{"dense graphics without OCR", parser.PageSignals{DenseGraphics: true}, "standard"},
		{"embedded images", parser.PageSignals{EmbeddedImages: true}, "standard"},
		{"OCR and columns", parser.PageSignals{Scanned: true, MultiColumn: true}, "standard"},
		{"OCR and table", parser.PageSignals{Garbled: true, TableLikely: true}, "standard"},
		{"OCR and embedded images", parser.PageSignals{NoText: true, EmbeddedImages: true}, "standard"},
		{"OCR and dense layout", parser.PageSignals{VectorText: true, DenseGraphics: true}, "advanced"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parser.Decide("png", []parser.PageSignals{tt.page})
			want := parser.Decision{Path: "mineru", Tier: parser.Tier(tt.tier)}
			if err != nil || got != want {
				t.Fatalf("Decide = %+v, %v; want %+v", got, err, want)
			}
		})
	}
}

func TestDecideKeepsOCRAndDenseLayoutOnTheSamePage(t *testing.T) {
	tests := []struct {
		name  string
		pages []parser.PageSignals
		tier  string
	}{
		{"OCR then separate dense layout", []parser.PageSignals{{Scanned: true}, {DenseGraphics: true}}, "standard"},
		{"dense layout then separate OCR", []parser.PageSignals{{DenseGraphics: true}, {NoText: true}}, "standard"},
		{"preserve higher earlier requirement", []parser.PageSignals{{TableLikely: true}, {}, {Scanned: true}}, "standard"},
		{"later advanced requirement", []parser.PageSignals{{}, {Garbled: true, DenseGraphics: true}}, "advanced"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parser.Decide("png", tt.pages)
			if err != nil || got.Path != "mineru" || string(got.Tier) != tt.tier {
				t.Fatalf("Decide = %+v, %v; want mineru/%s", got, err, tt.tier)
			}
		})
	}
}

func TestDecideReviewsOnlyWhenSparsePagesExceedHalf(t *testing.T) {
	tests := []struct {
		name   string
		pages  []parser.PageSignals
		review bool
		tier   string
	}{
		{"exactly half", []parser.PageSignals{{SparseText: true}, {}}, false, "flash"},
		{"more than half", []parser.PageSignals{{SparseText: true}, {}, {SparseText: true}}, true, "flash"},
		{"all sparse", []parser.PageSignals{{SparseText: true}}, true, "flash"},
		{"count pages after advanced", []parser.PageSignals{{Scanned: true, DenseGraphics: true}, {SparseText: true}, {SparseText: true}}, true, "advanced"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parser.Decide("png", tt.pages)
			if err != nil || got.ReviewRequired != tt.review || string(got.Tier) != tt.tier {
				t.Fatalf("Decide = %+v, %v; want tier=%s review=%v", got, err, tt.tier, tt.review)
			}
		})
	}
}

func TestDecideRasterImagesUseTheSameTierRules(t *testing.T) {
	for _, fileType := range []string{"png", "jpg", "jpeg", "bmp", "tif", "tiff"} {
		t.Run(fileType, func(t *testing.T) {
			t.Parallel()
			for _, tt := range []struct {
				page parser.PageSignals
				tier string
			}{
				{parser.PageSignals{}, "flash"},
				{parser.PageSignals{Scanned: true}, "basic"},
				{parser.PageSignals{MultiColumn: true}, "standard"},
				{parser.PageSignals{Scanned: true, DenseGraphics: true}, "advanced"},
			} {
				got, err := parser.Decide(fileType, []parser.PageSignals{tt.page})
				if err != nil || got.Path != "mineru" || string(got.Tier) != tt.tier {
					t.Fatalf("image = %+v, %v; want mineru/%s", got, err, tt.tier)
				}
			}
		})
	}
}

func TestDecideNativeFormatsNeverReceiveATier(t *testing.T) {
	for _, fileType := range []string{
		"doc", "docx", "docm", "ppt", "pptx", "pptm", "xls", "xlsx", "xlsm",
		"odt", "ods", "odp", "epub", "ofd", "html", "htm", "mhtml", "mht", "csv", "tsv",
		"txt", "md", "markdown", "rst", "tex",
	} {
		t.Run(fileType, func(t *testing.T) {
			t.Parallel()
			for _, pages := range [][]parser.PageSignals{nil, {{Scanned: true, DenseGraphics: true, SparseText: true}}} {
				got, err := parser.Decide(fileType, pages)
				want := parser.Decision{Path: "native"}
				if err != nil || got != want {
					t.Fatalf("native = %+v, %v; want %+v", got, err, want)
				}
			}
		})
	}
}

func TestDecideRejectsMissingPreflightAndUnknownFormats(t *testing.T) {
	for _, fileType := range []string{"pdf", "png"} {
		for _, pages := range [][]parser.PageSignals{nil, {}} {
			if got, err := parser.Decide(fileType, pages); err == nil || got != (parser.Decision{}) {
				t.Fatalf("missing preflight for %s produced usable decision: %+v, %v", fileType, got, err)
			}
		}
	}
	for _, fileType := range []string{"", "unknown", "application/pdf", "report.pdf", "/tmp/report.pdf", "pdf.exe"} {
		if got, err := parser.Decide(fileType, []parser.PageSignals{{}}); err == nil || got != (parser.Decision{}) {
			t.Fatalf("unknown type produced usable decision: %+v, %v", got, err)
		}
	}
}

func TestDecideNormalizesFileTypeSuffix(t *testing.T) {
	for _, fileType := range []string{" PDF ", ".PDF"} {
		got, err := parser.Decide(fileType, []parser.PageSignals{{}})
		if err != nil || got.Path != "mineru" || got.Tier != "flash" {
			t.Fatalf("normalized suffix = %+v, %v; want mineru/flash", got, err)
		}
	}
}
