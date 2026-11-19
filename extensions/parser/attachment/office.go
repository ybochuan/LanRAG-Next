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
func (e OfficeExtractor) Extract(data []byte) ([]File, error) {
	result, err := e.extract(data)
	return result.files, err
}

func (OfficeExtractor) extract(data []byte) (extraction, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return extraction{}, errors.New("invalid Office container")
	}
	var result extraction
	var expanded uint64
	count := 0
	for _, f := range z.File {
		if f.FileInfo().IsDir() || !(strings.HasPrefix(f.Name, "word/embeddings/") || strings.HasPrefix(f.Name, "xl/embeddings/") || strings.HasPrefix(f.Name, "ppt/embeddings/")) {
			continue
		}
		count++
		if count > MaxFiles {
			return extraction{}, errors.New("attachment batch exceeds limit")
		}
		if !allowedExtension(f.Name) {
			result.skip(f.Name, "unsupported_format")
			continue
		}
		if f.UncompressedSize64 >= MaxFileBytes {
			result.skip(f.Name, "size_limit")
			continue
		}
		expanded += f.UncompressedSize64
		if expanded > MaxBatchBytes {
			return extraction{}, errors.New("attachment batch exceeds limit")
		}
		b, err := readMember(f, MaxFileBytes-1)
		if err != nil {
			result.skip(f.Name, "read_failed")
			continue
		}
		name := baseName(f.Name)
		if !Allowed(name, b) {
			result.skip(name, "invalid_format")
			continue
		}
		result.files = append(result.files, File{name, b})
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
