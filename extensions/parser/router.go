// Package parser implements independent AgentRAG parsing policy.
package parser

import (
	"errors"
	"strings"
)

// Path selects either the existing native reader or the MinerU tier policy.
type Path string

const (
	// PathNative preserves the existing reader without a MinerU tier decision.
	PathNative Path = "native"
	// PathMinerU requires preflight signals and an explicit quality tier.
	PathMinerU Path = "mineru"
)

// Tier is the MinerU quality requested after preflight, never a parse attempt.
type Tier string

const (
	TierFlash    Tier = "flash"
	TierBasic    Tier = "basic"
	TierStandard Tier = "standard"
	TierAdvanced Tier = "advanced"
)

// PageSignals contains already measured or annotated signals for one page.
// This policy does not inspect files or define the per-KB detection thresholds.
type PageSignals struct {
	Scanned        bool // The page is a scan requiring OCR.
	NoText         bool // The page has no usable text layer.
	Garbled        bool // Extracted text is not usable.
	VectorText     bool // Text is represented by vector outlines.
	MultiColumn    bool // Multiple text columns require layout handling.
	TableLikely    bool // A table is likely present.
	DenseGraphics  bool // Preflight identified high-density graphical layout.
	EmbeddedImages bool // Embedded images require layout handling.
	SparseText     bool // Preflight marked this page as low-value sparse text.
}

// Decision is a routing result, not a parser response or a calibrated score.
type Decision struct {
	Path           Path   // Native routing or MinerU quality selection.
	Tier           Tier   // Empty for native routing.
	OCRMode        string // Explicit txt/ocr for PDF; independent of quality tier.
	ReviewRequired bool   // Sparse pages or mixed text/nontext require review.
}

// DecidePDF enforces the user's M1-03 no-download policy before formal parsing.
// Any real text layer disables OCR, including minority-text mixed PDFs. Only
// exclusively image-painted pages may enter an OCR route. This supersedes the
// Frozen PDF layout escalation; raster-image policy remains separately below.
func DecidePDF(report PDFPreflight) (Decision, error) {
	if report.TotalPages <= 0 || report.TextLayerPages < 0 || report.ImageOnlyPages < 0 || report.TextLayerPages+report.ImageOnlyPages > report.TotalPages {
		return Decision{}, errors.New("invalid PDF preflight measurements")
	}
	if report.TextLayerPages > 0 {
		return Decision{Path: PathMinerU, Tier: TierFlash, OCRMode: "txt", ReviewRequired: report.TextLayerPages != report.TotalPages}, nil
	}
	if report.ImageOnlyPages == report.TotalPages {
		return Decision{Path: PathMinerU, Tier: TierBasic, OCRMode: "ocr"}, nil
	}
	return Decision{}, errors.New("PDF has no confirmed text or exclusively image pages")
}

// Decide applies the Frozen page rules without I/O, parsing or mutable state.
// fileType is a suffix, not a path or MIME type. PDF/image decisions require
// complete preflight signals; callers own scope binding and signal extraction.
func Decide(fileType string, pages []PageSignals) (Decision, error) {
	fileType = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileType)), ".")
	switch fileType {
	case "doc", "docx", "docm", "ppt", "pptx", "pptm", "xls", "xlsx", "xlsm",
		"odt", "ods", "odp", "epub", "ofd", "html", "htm", "mhtml", "mht", "csv", "tsv",
		"txt", "md", "markdown", "rst", "tex":
		return Decision{Path: PathNative}, nil
	case "pdf", "png", "jpg", "jpeg", "bmp", "tif", "tiff":
		if len(pages) == 0 {
			return Decision{}, errors.New("preflight page signals required")
		}
	default:
		return Decision{}, errors.New("unsupported parser file type")
	}
	if fileType == "pdf" {
		report := PDFPreflight{TotalPages: len(pages)}
		sparse := 0
		for _, page := range pages {
			if page.Scanned {
				report.ImageOnlyPages++
			} else if !page.NoText && !page.Garbled && !page.VectorText {
				report.TextLayerPages++
			}
			if page.SparseText {
				sparse++
			}
		}
		decision, err := DecidePDF(report)
		decision.ReviewRequired = decision.ReviewRequired || sparse > len(pages)/2
		return decision, err
	}

	maxRank, sparsePages := 0, 0
	for _, page := range pages {
		needsOCR := page.Scanned || page.NoText || page.Garbled || page.VectorText
		hasLayout := page.MultiColumn || page.TableLikely || page.DenseGraphics || page.EmbeddedImages
		rank := 0
		if needsOCR {
			rank = 1
		}
		if hasLayout {
			rank = 2
		}
		if needsOCR && page.DenseGraphics {
			rank = 3
		}
		if rank > maxRank {
			maxRank = rank
		}
		if page.SparseText {
			sparsePages++
		}
	}
	tiers := [...]Tier{TierFlash, TierBasic, TierStandard, TierAdvanced}
	return Decision{
		Path:           PathMinerU,
		Tier:           tiers[maxRank],
		ReviewRequired: sparsePages > len(pages)/2,
	}, nil
}
