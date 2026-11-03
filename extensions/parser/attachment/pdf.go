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

func (PDFExtractor) Supports(name string, header []byte) bool {
	return strings.EqualFold(path.Ext(name), ".pdf") && bytes.Contains(header, []byte("%PDF-"))
}
func (PDFExtractor) Extract(data []byte) ([]File, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conf := model.NewStatelessConfiguration()
	conf.Limits.MaxInputBytes = MaxSourceBytes
	conf.Limits.MaxObjectBytes = 4 << 20
	conf.Limits.MaxStreamBytes = MaxSourceBytes
	conf.Limits.MaxDecodeBytes = MaxFileBytes
	listed, err := api.Attachments(ctx, bytes.NewReader(data), conf)
	if err != nil {
		return nil, errors.New("PDF attachment listing failed")
	}
	names := []string{}
	for _, a := range listed {
		if allowedExtension(a.FileName) {
			names = append(names, a.ID)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	if len(names) > MaxFiles {
		return nil, errors.New("attachment batch exceeds limit")
	}
	var result []File
	total := 0
	for _, id := range names {
		// pdfcpu decodes before returning its Reader. Bound each attachment
		// separately so one oversize stream cannot discard valid siblings.
		raw, err := api.ExtractAttachmentsRaw(ctx, bytes.NewReader(data), "", []string{id}, conf)
		if errors.Is(err, filter.ErrDecodeLimitExceeded) {
			continue
		}
		if err != nil {
			return nil, errors.New("PDF attachment extraction failed")
		}
		for _, a := range raw {
			b, err := io.ReadAll(io.LimitReader(a.Reader, MaxFileBytes))
			if err != nil {
				return nil, errors.New("PDF attachment read failed")
			}
			name := baseName(a.FileName)
			if !Allowed(name, b) {
				continue
			}
			total += len(b)
			if total > MaxBatchBytes {
				return nil, errors.New("attachment batch exceeds limit")
			}
			result = append(result, File{name, b})
		}
	}
	return result, nil
}
