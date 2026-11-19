package attachment

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/filter"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// PDFExtractor uses the pinned pure-Go pdfcpu API without filesystem configuration.
type PDFExtractor struct{}

func (e PDFExtractor) Extract(data []byte) ([]File, error) {
	result, err := e.extract(data)
	return result.files, err
}

func (PDFExtractor) Supports(name string, header []byte) bool {
	return strings.EqualFold(path.Ext(name), ".pdf") && bytes.Contains(header, []byte("%PDF-"))
}
func (PDFExtractor) extract(data []byte) (extraction, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conf := model.NewStatelessConfiguration()
	conf.Limits.MaxInputBytes = MaxSourceBytes
	conf.Limits.MaxObjectBytes = 4 << 20
	conf.Limits.MaxStreamBytes = MaxSourceBytes
	conf.Limits.MaxDecodeBytes = MaxFileBytes
	listed, err := api.Attachments(ctx, bytes.NewReader(data), conf)
	if err != nil {
		return extraction{}, errors.New("PDF attachment listing failed")
	}
	if len(listed) > MaxFiles {
		return extraction{}, errors.New("attachment batch exceeds limit")
	}
	var result extraction
	count, total := 0, 0
	for _, a := range listed {
		if !allowedExtension(a.FileName) {
			result.skip(a.FileName, "unsupported_format")
			continue
		}
		count++
		if count > MaxFiles {
			return extraction{}, errors.New("attachment batch exceeds limit")
		}
		// pdfcpu decodes before returning its Reader. Bound each attachment
		// separately so one oversize stream cannot discard valid siblings.
		raw, err := api.ExtractAttachmentsRaw(ctx, bytes.NewReader(data), "", []string{a.ID}, conf)
		if errors.Is(err, filter.ErrDecodeLimitExceeded) {
			result.skip(a.FileName, "size_limit")
			continue
		}
		if err != nil || len(raw) != 1 {
			result.skip(a.FileName, "read_failed")
			continue
		}
		b, err := io.ReadAll(io.LimitReader(raw[0].Reader, MaxFileBytes))
		if err != nil {
			result.skip(a.FileName, "read_failed")
			continue
		}
		if len(b) >= MaxFileBytes {
			result.skip(a.FileName, "size_limit")
			continue
		}
		total += len(b)
		if total > MaxBatchBytes {
			return extraction{}, errors.New("attachment batch exceeds limit")
		}
		result.files = append(result.files, File{baseName(a.FileName), b})
	}
	return result, nil
}
