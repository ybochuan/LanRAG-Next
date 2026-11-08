package attachment

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Mapping states are stored verbatim and drive the chat-card grouping.
const (
	MappingConfirmed = "confirmed"
	MappingCandidate = "candidate"
	MappingUnmatched = "unmatched"
)

// Semantic is the deterministic link between one attachment and the parent
// document text that refers to it. It never replaces the original filename.
type Semantic struct {
	OriginalFilename   string
	DisplayName        string
	AttachmentSequence int
	ReferenceTitle     string
	ReferenceLocation  string
	ReferenceExcerpt   string
	MappingStatus      string
	MappingConfidence  float64
	IndexPolicy        string
	ParentFileHash     string
	ParentTitle        string
}

type reference struct {
	Title    string
	Ordinal  int
	Location string
	Excerpt  string
}

var (
	annexReference = regexp.MustCompile(`(?i)^\s*附件\s*[:：]?\s*([0-9]{1,2}|[一二三四五六七八九十]{1,3})\s*[、.．:：\-—]?\s*(.*)$`)
	annexNumber    = regexp.MustCompile(`(?i)附件\s*[:：]?\s*([0-9]{1,2}|[一二三四五六七八九十]{1,3})`)
	bookReference  = regexp.MustCompile(`《([^》]{2,60})》`)
	headingStyle   = regexp.MustCompile(`(?i)^(heading|标题)\s*([1-9])$`)
	annexFile      = regexp.MustCompile(`(?i)annex[-_ ]?([0-9]{1,3})`)
	documentNumber = regexp.MustCompile(`^[A-Za-z]{2,}\s*〔[0-9]{4}〕`)
	numberedItem   = regexp.MustCompile(`^[0-9]+[.．、]`)
	chineseHeading = regexp.MustCompile(`^[一二三四五六七八九十]+、`)
)

// Analyze maps extracted files to explicit references in the parent DOCX.
// Any parser failure degrades to filenames and unmatched status; extraction
// itself remains independent and is never blocked by semantic analysis.
func Analyze(parentName string, parentData []byte, parentTitle string, files []File) []Semantic {
	result := make([]Semantic, 0, len(files))
	if len(files) == 0 {
		return result
	}
	parentHash := sha256Hex(parentData)
	refs := parentReferences(parentName, parentData)
	matches := make([]int, len(files))
	scores := make([]float64, len(files))
	for i, file := range files {
		title := attachmentTitle(file.Name, file.Data)
		ordinal := attachmentOrdinal(file.Name, i+1)
		best, bestScore := -1, 0.0
		for j, ref := range refs {
			score := referenceScore(title, ordinal, ref)
			if score > bestScore {
				best, bestScore = j, score
			}
		}
		matches[i], scores[i] = best, bestScore
	}
	// The same reference may not be claimed by several attachments as a
	// confirmed fact. Ambiguity always falls back to a candidate.
	claims := map[string][]int{}
	for i, refIndex := range matches {
		if refIndex >= 0 && scores[i] >= 0.94 {
			claims[referenceKey(refs[refIndex])] = append(claims[referenceKey(refs[refIndex])], i)
		}
	}
	for _, indexes := range claims {
		if len(indexes) < 2 {
			continue
		}
		for _, i := range indexes {
			if scores[i] > 0.85 {
				scores[i] = 0.85
			}
		}
	}
	for i, file := range files {
		title := attachmentTitle(file.Name, file.Data)
		display := title
		if display == "" {
			display = file.Name
		}
		semantic := Semantic{
			OriginalFilename:   file.Name,
			DisplayName:        display,
			AttachmentSequence: attachmentOrdinal(file.Name, i+1),
			MappingStatus:      MappingUnmatched,
			IndexPolicy:        "default",
			ParentFileHash:     parentHash,
			ParentTitle:        displayTitle(parentTitle),
		}
		if refIndex := matches[i]; refIndex >= 0 && scores[i] >= 0.70 {
			ref := refs[refIndex]
			semantic.ReferenceTitle = ref.Title
			semantic.ReferenceLocation = ref.Location
			semantic.ReferenceExcerpt = ref.Excerpt
			semantic.MappingConfidence = math.Round(scores[i]*100) / 100
			if scores[i] >= 0.94 {
				semantic.MappingStatus = MappingConfirmed
			} else {
				semantic.MappingStatus = MappingCandidate
			}
		}
		result = append(result, semantic)
	}
	return result
}

func parentReferences(name string, data []byte) []reference {
	if !strings.EqualFold(path.Ext(name), ".docx") {
		return nil
	}
	source, err := docxDocumentXML(data)
	if err != nil {
		return nil
	}
	parts, _, _, err := documentParts(source)
	if err != nil {
		return nil
	}
	var result []reference
	section := ""
	for i, part := range parts {
		if !part.paragraph {
			continue
		}
		text := strings.TrimSpace(part.text)
		if text == "" {
			continue
		}
		if looksLikeHeading(text, part.style) {
			section = text
			continue
		}
		result = append(result, extractReferences(text, section, i+1)...)
	}
	return dedupeReferences(result)
}

func extractReferences(text, section string, paragraph int) []reference {
	location := section
	if location == "" {
		location = fmt.Sprintf("第%d段", paragraph)
	}
	excerpt := truncateRunes(text, 240)
	var result []reference
	ordinal := 0
	if m := annexNumber.FindStringSubmatch(text); m != nil {
		ordinal = parseOrdinal(m[1])
	}
	if m := annexReference.FindStringSubmatch(text); m != nil {
		ordinal = parseOrdinal(m[1])
		title := cleanReferenceTitle(m[2])
		if title != "" {
			result = append(result, reference{Title: title, Ordinal: ordinal, Location: location, Excerpt: excerpt})
		} else if ordinal > 0 {
			result = append(result, reference{Ordinal: ordinal, Location: location, Excerpt: excerpt})
		}
	}
	for _, m := range bookReference.FindAllStringSubmatch(text, -1) {
		title := cleanReferenceTitle(m[1])
		if title == "" {
			continue
		}
		result = append(result, reference{Title: title, Ordinal: ordinal, Location: location, Excerpt: excerpt})
	}
	return result
}

func dedupeReferences(items []reference) []reference {
	seen := map[string]bool{}
	result := make([]reference, 0, len(items))
	for _, item := range items {
		key := referenceKey(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
	}
	return result
}

func referenceKey(ref reference) string {
	return strconv.Itoa(ref.Ordinal) + "|" + normalizeTitle(ref.Title)
}

func referenceScore(title string, ordinal int, ref reference) float64 {
	normalized := normalizeTitle(title)
	refTitle := normalizeTitle(ref.Title)
	if normalized != "" && refTitle != "" && normalized == refTitle {
		return 1.0
	}
	if ordinal > 0 && ref.Ordinal == ordinal {
		if normalized == "" || refTitle == "" {
			return 0.92
		}
		return 0.90
	}
	if normalized != "" && refTitle != "" {
		similarity := titleSimilarity(normalized, refTitle)
		if similarity >= 0.86 {
			return 0.75 + (similarity - 0.86)
		}
	}
	return 0
}

func attachmentTitle(name string, data []byte) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".docx":
		return docxTitle(data)
	case ".xlsx":
		return xlsxTitle(data)
	case ".pptx":
		return pptxTitle(data)
	}
	return ""
}

func displayTitle(title string) string {
	title = strings.TrimSpace(title)
	for _, ext := range []string{".docx", ".doc", ".pdf", ".xlsx", ".xls", ".pptx", ".ppt"} {
		if strings.EqualFold(path.Ext(title), ext) {
			return strings.TrimSuffix(title, path.Ext(title))
		}
	}
	return title
}

func docxTitle(data []byte) string {
	source, err := docxDocumentXML(data)
	if err != nil {
		return ""
	}
	parts, _, _, err := documentParts(source)
	if err != nil {
		return ""
	}
	var candidates []string
	for _, part := range parts {
		if !part.paragraph {
			continue
		}
		text := strings.Join(strings.Fields(strings.TrimSpace(part.text)), " ")
		if text == "" || annexTitle.MatchString(text) {
			continue
		}
		if m := annexReference.FindStringSubmatch(text); m != nil {
			if title := cleanReferenceTitle(m[2]); title != "" {
				return title
			}
			continue
		}
		if looksLikeFormLabel(text) || documentNumber.MatchString(text) || numberedItem.MatchString(text) {
			continue
		}
		if len([]rune(text)) > 120 {
			continue
		}
		candidates = append(candidates, text)
	}
	if len(candidates) == 0 {
		return ""
	}
	title := candidates[0]
	if len([]rune(title)) < 24 || strings.HasSuffix(title, "的") || strings.HasSuffix(title, "法定代表人") {
		for i := 1; i < len(candidates) && i < 4; i++ {
			next := candidates[i]
			if len([]rune(next)) > 60 || strings.ContainsAny(next, "。；：:") || chineseHeading.MatchString(next) || numberedItem.MatchString(next) {
				break
			}
			title += next
			if len([]rune(title)) >= 24 && !strings.HasSuffix(title, "的") && !strings.HasSuffix(title, "法定代表人") {
				break
			}
		}
	}
	return title
}

func looksLikeFormLabel(text string) bool {
	if strings.HasSuffix(text, "：") || strings.HasSuffix(text, ":") {
		return true
	}
	if strings.ContainsAny(text, "＿_") {
		return true
	}
	for _, prefix := range []string{"归档日期", "档案编号", "业务种类", "档案类别", "业务金额", "提交人", "审核人", "说明：", "注："} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func xlsxTitle(data []byte) string {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	for _, file := range z.File {
		if file.Name != "xl/workbook.xml" {
			continue
		}
		raw, err := readMember(file, MaxSourceBytes)
		if err != nil {
			return ""
		}
		var workbook struct {
			Sheets []struct {
				Name string `xml:"name,attr"`
			} `xml:"sheets>sheet"`
		}
		if xml.Unmarshal(raw, &workbook) != nil {
			return ""
		}
		for _, sheet := range workbook.Sheets {
			if name := strings.TrimSpace(sheet.Name); name != "" {
				return name
			}
		}
	}
	return ""
}

func pptxTitle(data []byte) string {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	for _, file := range z.File {
		if file.Name != "ppt/slides/slide1.xml" {
			continue
		}
		raw, err := readMember(file, MaxSourceBytes)
		if err != nil {
			return ""
		}
		decoder := xml.NewDecoder(bytes.NewReader(raw))
		for {
			token, err := decoder.Token()
			if err != nil {
				return ""
			}
			if start, ok := token.(xml.StartElement); ok && start.Name.Local == "t" {
				var text string
				if decoder.DecodeElement(&text, &start) == nil {
					text = strings.TrimSpace(text)
					if text != "" {
						return text
					}
				}
			}
		}
	}
	return ""
}

func docxDocumentXML(data []byte) ([]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("invalid DOCX container")
	}
	for _, file := range z.File {
		if file.Name == "word/document.xml" {
			return readMember(file, MaxSourceBytes)
		}
	}
	return nil, errors.New("DOCX document missing")
}

func attachmentOrdinal(name string, fallback int) int {
	if m := annexFile.FindStringSubmatch(path.Base(name)); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func parseOrdinal(value string) int {
	value = strings.TrimSpace(value)
	if n, err := strconv.Atoi(value); err == nil {
		return n
	}
	digits := map[rune]int{'一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	runes := []rune(value)
	if len(runes) == 1 && runes[0] == '十' {
		return 10
	}
	if len(runes) == 2 && runes[0] == '十' {
		if n, ok := digits[runes[1]]; ok {
			return 10 + n
		}
	}
	if len(runes) == 2 && runes[1] == '十' {
		if n, ok := digits[runes[0]]; ok {
			return n * 10
		}
	}
	if len(runes) == 3 && runes[1] == '十' {
		tens, ok1 := digits[runes[0]]
		ones, ok2 := digits[runes[2]]
		if ok1 && ok2 {
			return tens*10 + ones
		}
	}
	if len(runes) == 1 {
		return digits[runes[0]]
	}
	return 0
}

func cleanReferenceTitle(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "：:，,。.；;、-— \t\r\n")
	value = strings.TrimPrefix(value, "包括")
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "：:，,。.；;、-— \t\r\n")
	if len([]rune(value)) < 2 || len([]rune(value)) > 80 {
		return ""
	}
	return value
}

func normalizeTitle(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "《》〈〉\"'“”‘’")
	value = strings.ToLower(value)
	value = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("：:，,。.；;、-—()（）[]【】", r) {
			return -1
		}
		return r
	}, value)
	return value
}

func titleSimilarity(a, b string) float64 {
	left, right := []rune(a), []rune(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	distance := levenshtein(left, right)
	longest := len(left)
	if len(right) > longest {
		longest = len(right)
	}
	return 1 - float64(distance)/float64(longest)
}

func levenshtein(a, b []rune) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(current[j-1]+1, min(previous[j]+1, previous[j-1]+cost))
		}
		previous = current
	}
	return previous[len(b)]
}

func looksLikeHeading(text, style string) bool {
	if annexTitle.MatchString(strings.TrimSpace(text)) {
		return false
	}
	style = strings.TrimSpace(style)
	if headingStyle.MatchString(style) || strings.HasPrefix(strings.ToLower(style), "heading") {
		return true
	}
	if len([]rune(text)) > 60 {
		return false
	}
	if strings.HasPrefix(text, "第") && strings.ContainsAny(text, "章节篇部分") {
		return true
	}
	return chineseHeading.MatchString(text)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
