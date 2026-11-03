package attachment

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const wordNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

var annexTitle = regexp.MustCompile(`^附件\s*([0-9]{1,2}(?:[-－—.][0-9]{1,2})?|[一二三四五六七八九十]{1,3})\s*[:：、．.]?$`)

// DocxAnnexExtractor splits only standalone, explicit attachment-number paragraphs.
// Original package resources/styles are retained; each document body is replaced.
type DocxAnnexExtractor struct{}

func (DocxAnnexExtractor) Supports(name string, header []byte) bool {
	return strings.HasSuffix(strings.ToLower(name), ".docx") && bytes.HasPrefix(header, []byte("PK\x03\x04"))
}

type bodyPart struct {
	start, end         int
	text               string
	paragraph, section bool
}

func (DocxAnnexExtractor) Extract(data []byte) ([]File, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("invalid DOCX container")
	}
	var source []byte
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			source, err = readMember(f, MaxSourceBytes)
			break
		}
	}
	if err != nil {
		return nil, err
	}
	if len(source) == 0 {
		return nil, nil
	}
	parts, bodyStart, bodyEnd, err := documentParts(source)
	if err != nil {
		return nil, err
	}
	var starts []int
	for i, p := range parts {
		if p.paragraph && annexTitle.MatchString(strings.TrimSpace(p.text)) {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return nil, nil
	}
	if len(starts) > MaxFiles {
		return nil, errors.New("annex count exceeds limit")
	}
	var section []byte
	for _, p := range parts {
		if p.section {
			section = source[p.start:p.end]
		}
	}
	result := make([]File, 0, len(starts))
	total := 0
	for n, start := range starts {
		end := len(parts)
		if n+1 < len(starts) {
			end = starts[n+1]
		}
		var body bytes.Buffer
		for _, p := range parts[start:end] {
			if !p.section {
				body.Write(source[p.start:p.end])
			}
		}
		body.Write(section)
		replacement := append(append(append([]byte(nil), source[:bodyStart]...), body.Bytes()...), source[bodyEnd:]...)
		var output bytes.Buffer
		writer := zip.NewWriter(&output)
		expanded := 0
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				continue
			}
			var b []byte
			if f.Name == "word/document.xml" {
				b = replacement
			} else {
				b, err = readMember(f, MaxBatchBytes)
				if err != nil {
					return nil, err
				}
			}
			expanded += len(b)
			if expanded > MaxBatchBytes {
				return nil, errors.New("annex package exceeds limit")
			}
			header := f.FileHeader
			header.Method = zip.Deflate
			w, e := writer.CreateHeader(&header)
			if e != nil {
				return nil, errors.New("annex package creation failed")
			}
			if _, e = w.Write(b); e != nil {
				return nil, e
			}
		}
		if err = writer.Close(); err != nil {
			return nil, err
		}
		if output.Len() >= MaxFileBytes {
			continue
		}
		total += output.Len()
		if total > MaxBatchBytes {
			return nil, errors.New("annex batch exceeds limit")
		}
		result = append(result, File{fmt.Sprintf("annex-%02d.docx", n+1), output.Bytes()})
	}
	return result, nil
}
func documentParts(source []byte) ([]bodyPart, int, int, error) {
	d := xml.NewDecoder(bytes.NewReader(source))
	depth, bodyDepth := 0, 0
	start, end := 0, 0
	var parts []bodyPart
	var current *bodyPart
	inText := false
	for {
		before := int(d.InputOffset())
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, 0, errors.New("invalid DOCX document XML")
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Space == wordNS && t.Name.Local == "body" {
				bodyDepth = depth
				start = int(d.InputOffset())
			} else if bodyDepth > 0 && depth == bodyDepth+1 {
				parts = append(parts, bodyPart{start: before, paragraph: t.Name.Space == wordNS && t.Name.Local == "p", section: t.Name.Space == wordNS && t.Name.Local == "sectPr"})
				current = &parts[len(parts)-1]
			}
			if t.Name.Space == wordNS && t.Name.Local == "t" {
				inText = true
			}
		case xml.CharData:
			if inText && current != nil {
				current.text += string(t)
			}
		case xml.EndElement:
			if t.Name.Space == wordNS && t.Name.Local == "t" {
				inText = false
			}
			if bodyDepth > 0 && depth == bodyDepth+1 && current != nil {
				current.end = int(d.InputOffset())
				current = nil
			}
			if depth == bodyDepth && bodyDepth > 0 {
				end = before
				bodyDepth = 0
			}
			depth--
		}
	}
	if start == 0 || end <= start {
		return nil, 0, 0, errors.New("DOCX body missing")
	}
	return parts, start, end, nil
}
