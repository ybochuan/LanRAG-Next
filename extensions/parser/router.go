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
	Path           Path // Native routing or MinerU quality selection.
	Tier           Tier // Empty for native routing.
	ReviewRequired bool // More than half of inspected pages were sparse.
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
