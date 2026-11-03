package attachment

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"path"
	"strings"
)

// OfficeExtractor extracts actual OOXML embedding members; OLE .bin is excluded.
type OfficeExtractor struct{}

func (OfficeExtractor) Supports(name string, header []byte) bool {
	ext := strings.ToLower(path.Ext(name))
	return (ext == ".docx" || ext == ".xlsx" || ext == ".pptx") && bytes.HasPrefix(header, []byte("PK\x03\x04"))
}
func (OfficeExtractor) Extract(data []byte) ([]File, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("invalid Office container")
	}
	var result []File
	total := 0
	for _, f := range z.File {
		if f.FileInfo().IsDir() || !(strings.HasPrefix(f.Name, "word/embeddings/") || strings.HasPrefix(f.Name, "xl/embeddings/") || strings.HasPrefix(f.Name, "ppt/embeddings/")) || !allowedExtension(f.Name) || f.UncompressedSize64 >= MaxFileBytes {
			continue
		}
		b, err := readMember(f, MaxFileBytes-1)
		if err != nil {
			return nil, err
		}
		name := baseName(f.Name)
		if !Allowed(name, b) {
			continue
		}
		total += len(b)
		if len(result) >= MaxFiles || total > MaxBatchBytes {
			return nil, errors.New("attachment batch exceeds limit")
		}
		result = append(result, File{name, b})
	}
	return result, nil
}
func readMember(f *zip.File, limit int) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("Office member exceeds limit")
	}
	r, err := f.Open()
	if err != nil {
		return nil, errors.New("Office member open failed")
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil || len(b) > limit {
		return nil, errors.New("Office member read failed")
	}
	return b, nil
}
