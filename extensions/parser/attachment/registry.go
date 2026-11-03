// Package attachment extracts independent files and explicitly titled DOCX annexes.
package attachment

import (
	"archive/zip"
	"bytes"
	"errors"
	"path"
	"strings"
	"unicode"
)

const (
	MaxFileBytes   = 20 << 20 // Attachments must be strictly smaller than 20 MiB.
	MaxSourceBytes = 50 << 20
	MaxBatchBytes  = 64 << 20
	MaxFiles       = 32
)

// File contains an independent filename and its unchanged file bytes.
type File struct {
	Name string
	Data []byte
}

// Extractor identifies a supported container and returns its independent files.
type Extractor interface {
	Supports(fileName string, header []byte) bool
	Extract(data []byte) ([]File, error)
}

// Registry runs matching extractors in registration order.
type Registry struct{ extractors []Extractor }

// NewRegistry creates an independent, ordered extractor collection.
func NewRegistry(extractors ...Extractor) *Registry {
	return &Registry{extractors: append([]Extractor(nil), extractors...)}
}

// DefaultRegistry enables PDF, OOXML embeddings and explicit DOCX annexes.
func DefaultRegistry() *Registry {
	return NewRegistry(PDFExtractor{}, OfficeExtractor{}, DocxAnnexExtractor{})
}

// Extract filters supported results before any independent ingestion.
func (r *Registry) Extract(name string, data []byte) ([]File, error) {
	if len(data) == 0 || len(data) > MaxSourceBytes {
		return nil, errors.New("invalid attachment source size")
	}
	var result []File
	total := 0
	for _, e := range r.extractors {
		if e == nil || !e.Supports(name, data[:min(len(data), 1024)]) {
			continue
		}
		files, err := e.Extract(data)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			f.Name = baseName(f.Name)
			if !Allowed(f.Name, f.Data) {
				continue
			}
			total += len(f.Data)
			if len(result) >= MaxFiles || total > MaxBatchBytes {
				return nil, errors.New("attachment batch exceeds limit")
			}
			result = append(result, f)
		}
	}
	return result, nil
}
func baseName(name string) string { return path.Base(strings.ReplaceAll(name, `\`, "/")) }
func allowedExtension(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".pdf", ".docx", ".xlsx", ".pptx":
		return true
	}
	return false
}

// Allowed checks the extension, actual container type, and strict size boundary.
func Allowed(name string, data []byte) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsFunc(name, unicode.IsControl) || len(data) == 0 || len(data) >= MaxFileBytes {
		return false
	}
	ext := strings.ToLower(path.Ext(name))
	if ext == ".pdf" {
		return bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-"))
	}
	part := map[string]string{".docx": "word/document.xml", ".xlsx": "xl/workbook.xml", ".pptx": "ppt/presentation.xml"}[ext]
	if part == "" {
		return false
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	hasTypes, hasPart := false, false
	for _, f := range z.File {
		hasTypes = hasTypes || f.Name == "[Content_Types].xml"
		hasPart = hasPart || f.Name == part
	}
	return hasTypes && hasPart
}
