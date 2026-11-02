package parser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// PDFPreflight describes structurally confirmed page content, not OCR accuracy
// or Unicode quality. Unclassified pages are neither text nor image-only pages.
type PDFPreflight struct {
	TotalPages     int `json:"total_pages"`
	TextLayerPages int `json:"text_layer_pages"`
	ImageOnlyPages int `json:"image_only_pages"`
}

const preflightContentLimit = 4 << 20

// PreflightPDF reads actual PDF objects/content with pure Go. It never calls
// MinerU, renders images, loads OCR models or uses pdfcpu's filesystem config.
// Unsupported/ambiguous syntax is an error, never permission to try OCR.
func PreflightPDF(parent context.Context, source []byte) (PDFPreflight, error) {
	if parent == nil || len(source) == 0 || len(source) > proxySourceLimit {
		return PDFPreflight{}, errors.New("invalid PDF preflight input")
	}
	c, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	conf := model.NewStatelessConfiguration()
	conf.ValidationMode = model.ValidationStrict
	conf.Limits = model.ResourceLimits{
		MaxInputBytes: proxySourceLimit, MaxObjectBytes: preflightContentLimit,
		MaxStreamBytes: proxySourceLimit, MaxDecodeBytes: preflightContentLimit,
		MaxObjectCount: 100000, MaxXRefEntries: 100000,
		MaxObjectStreamCount: 100000, MaxObjectStreamFirst: preflightContentLimit,
		MaxRecursionDepth: 32,
	}
	ctx, err := api.ReadContext(c, bytes.NewReader(source), conf)
	if err != nil {
		return PDFPreflight{}, fmt.Errorf("read PDF preflight: %w", err)
	}
	if err := api.ValidateContext(c, ctx); err != nil {
		return PDFPreflight{}, fmt.Errorf("validate PDF preflight: %w", err)
	}
	if ctx.PageCount <= 0 || ctx.PageCount > 1000 {
		return PDFPreflight{}, errors.New("PDF preflight page limit")
	}
	probe := pdfProbe{ctx: ctx, remaining: 32 << 20, operations: 200000}
	// A structure tree may hold ActualText outside page streams. Until MCID
	// mapping is supported, it cannot establish that a textless page is a scan.
	hasStructure := ctx.RootDict["StructTreeRoot"] != nil
	result := PDFPreflight{TotalPages: ctx.PageCount}
	for page := 1; page <= ctx.PageCount; page++ {
		if err := c.Err(); err != nil {
			return PDFPreflight{}, err
		}
		dict, _, attrs, err := ctx.PageDict(c, page, false)
		if err != nil {
			return PDFPreflight{}, fmt.Errorf("PDF preflight page: %w", err)
		}
		content, err := ctx.PageContent(dict, page)
		if err != nil && !errors.Is(err, model.ErrNoContent) {
			return PDFPreflight{}, fmt.Errorf("PDF preflight content: %w", err)
		}
		marks, err := probe.scan(c, content, attrs.Resources, false, 0)
		if err != nil {
			return PDFPreflight{}, fmt.Errorf("PDF preflight operators: %w", err)
		}
		if marks.text {
			result.TextLayerPages++
		} else if marks.image && !marks.vector && !marks.textCandidate && !hasStructure {
			result.ImageOnlyPages++
		}
	}
	return result, c.Err()
}

type pdfProbe struct {
	ctx                   *model.Context
	remaining, operations int
}
type pageMarks struct{ text, image, vector, textCandidate bool }

// scan uses pdfcpu's object parser for strings, hex strings and arrays; plain
// searches for BT/Tj would count comments, unused fonts and image bytes as text.
func (p *pdfProbe) scan(c context.Context, content []byte, resources types.Dict, font bool, depth int) (pageMarks, error) {
	var marks pageMarks
	p.remaining -= len(content)
	if depth > 16 || len(content) > preflightContentLimit || p.remaining < 0 {
		return marks, errors.New("PDF preflight content budget")
	}
	s := string(content)
	var args []types.Object
	var stack []bool
	inText := false
	markedDepth := 0
	for {
		s = strings.TrimLeft(s, "\x00\t\n\f\r ")
		if s == "" {
			break
		}
		p.operations--
		if p.operations < 0 {
			return marks, errors.New("PDF preflight operation budget")
		}
		if err := c.Err(); err != nil {
			return marks, err
		}
		if s[0] == '%' {
			i := strings.IndexAny(s, "\r\n")
			if i < 0 {
				break
			}
			s = s[i:]
			continue
		}
		if strings.ContainsRune("/[(<+-.0123456789", rune(s[0])) {
			obj, err := model.ParseObject(c, &s, 0, 32)
			if err != nil {
				return marks, err
			}
			args = append(args, obj)
			if len(args) > 64 {
				return marks, errors.New("PDF preflight operand limit")
			}
			continue
		}
		i := 0
		for i < len(s) && !strings.ContainsRune("\x00\t\n\f\r ()<>[]{}/%", rune(s[i])) {
			i++
		}
		if i == 0 {
			return marks, errors.New("unsupported PDF content delimiter")
		}
		op := s[:i]
		s = s[i:]
		switch op {
		case "BMC", "BDC", "DP", "MP":
			want := 1
			if op == "BDC" || op == "DP" {
				want = 2
			}
			if len(args) != want {
				return marks, errors.New("invalid PDF marked content")
			}
			if _, ok := args[0].(types.Name); !ok {
				return marks, errors.New("invalid PDF marked content tag")
			}
			if want == 2 {
				props, err := p.markedProperties(args[1], resources)
				if err != nil {
					return marks, err
				}
				if actual, exists := props["ActualText"]; exists {
					text, err := types.StringOrHexLiteral(actual)
					if err != nil {
						return marks, errors.New("invalid PDF ActualText")
					}
					marks.text = marks.text || strings.TrimSpace(*text) != ""
					marks.textCandidate = marks.textCandidate || len(*text) > 0
				}
			}
			if op == "BMC" || op == "BDC" {
				markedDepth++
				if markedDepth > 32 {
					return marks, errors.New("PDF marked content depth limit")
				}
			}
		case "EMC":
			if markedDepth == 0 {
				return marks, errors.New("unbalanced PDF marked content")
			}
			markedDepth--
		case "BI":
			// Inline-image terminators are ambiguous inside arbitrary image data.
			return marks, errors.New("inline image requires separate inspection")
		case "BT":
			if inText {
				return marks, errors.New("nested PDF text object")
			}
			inText = true
		case "ET":
			if !inText {
				return marks, errors.New("unbalanced PDF text object")
			}
			inText = false
		case "q":
			if len(stack) >= 32 {
				return marks, errors.New("PDF graphics depth limit")
			}
			stack = append(stack, font)
		case "Q":
			if len(stack) == 0 {
				return marks, errors.New("unbalanced PDF graphics state")
			}
			font = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
		case "Tf":
			if len(args) != 2 {
				return marks, errors.New("invalid PDF font selection")
			}
			name, ok := args[0].(types.Name)
			if !ok {
				return marks, errors.New("invalid PDF font name")
			}
			fonts, err := p.ctx.DereferenceDict(resources["Font"])
			if err != nil {
				return marks, err
			}
			fd, err := p.ctx.DereferenceDict(fonts[name.Value()])
			if err != nil || fd == nil {
				return marks, errors.New("unresolved PDF text font")
			}
			font = true
		case "Tj", "TJ", "'", "\"":
			want := 1
			if op == "\"" {
				want = 3
			}
			if !inText || !font || len(args) != want {
				return marks, errors.New("invalid PDF text display")
			}
			text, err := nonemptyPDFText(args[len(args)-1])
			if err != nil {
				return marks, err
			}
			marks.text = marks.text || text
			marks.textCandidate = marks.textCandidate || pdfTextPayload(args[len(args)-1])
		case "Do":
			if len(args) != 1 {
				return marks, errors.New("invalid PDF XObject display")
			}
			name, ok := args[0].(types.Name)
			if !ok {
				return marks, errors.New("invalid PDF XObject name")
			}
			xobjects, err := p.ctx.DereferenceDict(resources["XObject"])
			if err != nil {
				return marks, err
			}
			sd, _, err := p.ctx.DereferenceStreamDict(xobjects[name.Value()])
			if err != nil || sd == nil {
				return marks, errors.New("unresolved PDF XObject")
			}
			subtype := sd.NameEntry("Subtype")
			if subtype == nil {
				return marks, errors.New("missing PDF XObject subtype")
			}
			switch *subtype {
			case "Image":
				marks.image = true // Never decode/render the pixels.
			case "Form":
				if err := p.ctx.DecodeStream(sd); err != nil {
					return marks, err
				}
				res := resources
				if raw, ok := sd.Dict["Resources"]; ok {
					res, err = p.ctx.DereferenceDict(raw)
					if err != nil {
						return marks, err
					}
				}
				child, err := p.scan(c, sd.Content, res, font, depth+1)
				if err != nil {
					return marks, err
				}
				marks.text = marks.text || child.text
				marks.image = marks.image || child.image
				marks.vector = marks.vector || child.vector
				marks.textCandidate = marks.textCandidate || child.textCandidate
			default:
				return marks, errors.New("unsupported PDF XObject")
			}
		case "S", "s", "f", "F", "f*", "B", "B*", "b", "b*", "sh":
			marks.vector = true
		case "cm", "w", "J", "j", "M", "d", "ri", "i", "gs",
			"m", "l", "c", "v", "y", "h", "re", "n", "W", "W*",
			"CS", "cs", "SC", "SCN", "sc", "scn", "G", "g", "RG", "rg", "K", "k",
			"Tc", "Tw", "Tz", "TL", "Tr", "Ts", "Td", "TD", "Tm", "T*", "BX", "EX":
			// Known state/path operators do not establish an extractable layer.
		default:
			return marks, errors.New("unknown PDF content operation requires review")
		}
		args = nil
	}
	if inText || len(stack) != 0 || len(args) != 0 || markedDepth != 0 {
		return marks, errors.New("incomplete PDF content")
	}
	return marks, nil
}

func (p *pdfProbe) markedProperties(obj types.Object, resources types.Dict) (types.Dict, error) {
	if name, ok := obj.(types.Name); ok {
		properties, err := p.ctx.DereferenceDict(resources["Properties"])
		if err != nil {
			return nil, err
		}
		dict, err := p.ctx.DereferenceDict(properties[name.Value()])
		if err != nil || dict == nil {
			return nil, errors.New("unresolved marked-content property")
		}
		return dict, nil
	}
	if dict, ok := obj.(types.Dict); ok {
		return dict, nil
	}
	return nil, errors.New("unsupported marked-content property")
}

func nonemptyPDFText(obj types.Object) (bool, error) {
	var b []byte
	var err error
	switch value := obj.(type) {
	case types.StringLiteral:
		b, err = types.Unescape(string(value))
	case types.HexLiteral:
		b, err = value.Bytes()
	case types.Array:
		found := false
		for _, part := range value {
			switch part.(type) {
			case types.Integer, types.Float:
				continue
			}
			yes, e := nonemptyPDFText(part)
			if e != nil {
				return false, e
			}
			found = found || yes
		}
		return found, nil
	default:
		return false, errors.New("invalid PDF text operand")
	}
	if err != nil {
		return false, err
	}
	// Character codes are not ASCII: custom encodings/CIDs can use codes 1-31.
	// This detects a structural layer, without claiming linguistic decoding.
	for _, code := range b {
		if !strings.ContainsRune("\x00\t\n\f\r ", rune(code)) {
			return true, nil
		}
	}
	return false, nil
}

// Nonempty but undecidable text must never be mistaken for an image-only scan.
func pdfTextPayload(obj types.Object) bool {
	switch value := obj.(type) {
	case types.StringLiteral:
		b, err := types.Unescape(string(value))
		return err == nil && len(b) > 0
	case types.HexLiteral:
		b, err := value.Bytes()
		return err == nil && len(b) > 0
	case types.Array:
		for _, part := range value {
			if pdfTextPayload(part) {
				return true
			}
		}
	}
	return false
}
