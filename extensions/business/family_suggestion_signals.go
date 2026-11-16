package business

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// Strip a four-digit year only when it does not sit inside a longer identifier.
var titleYear = regexp.MustCompile(`(^|[^0-9])((?:19|20)[0-9]{2})([^0-9]|$)`)
var editionSuffix = regexp.MustCompile(`(?i)(?:年?(?:[0-9]{1,2}月)?(?:修订版|修正版|新版|版)|edition|revision|rev)\s*$`)
var titleExtension = regexp.MustCompile(`(?i)\.(?:docx?|pdf|xlsx?|pptx?|txt)\s*$`)

func trimTitleExtension(raw string) string {
	return titleExtension.ReplaceAllString(strings.TrimSpace(raw), "")
}

func titleIdentity(raw string) (stem string, year int) {
	title := trimTitleExtension(raw)
	if match := titleYear.FindStringSubmatchIndex(title); len(match) != 0 {
		year, _ = strconv.Atoi(title[match[4]:match[5]])
		tail := strings.Trim(title[match[5]:], " ）)】]》-_")
		if tail == "" || editionSuffix.MatchString(tail) && editionSuffix.ReplaceAllString(tail, "") == "" {
			title = title[:match[4]]
		} else {
			year = 0
		}
	}
	return normalizedEvidenceText(title), year
}

func normalizedEvidenceText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}

func titleOverlap(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	bigrams := func(value string) map[string]bool {
		runes := []rune(value)
		out := make(map[string]bool, len(runes))
		for i := 1; i < len(runes); i++ {
			out[string(runes[i-1:i+1])] = true
		}
		return out
	}
	x, y := bigrams(a), bigrams(b)
	if len(x)+len(y) == 0 {
		return 0
	}
	count := 0
	for piece := range x {
		if y[piece] {
			count++
		}
	}
	return 2 * float64(count) / float64(len(x)+len(y))
}

func candidateSignals(family, published string, peer, anchor scanDocument) (FamilySuggestion, bool) {
	from, to := peer.doc, anchor.doc
	a, ay := titleIdentity(from.Title)
	b, by := titleIdentity(to.Title)
	out := FamilySuggestion{BusinessFamilyID: family, Kind: "relationship", EdgeType: "related_to",
		DocumentID: from.ID, TargetDocumentID: to.ID}
	out.Evidence.Similarity = titleOverlap(a, b)
	out.Evidence.TitleSimilarity = out.Evidence.Similarity
	if out.Evidence.Similarity > 0 {
		out.Evidence.Reasons = append(out.Evidence.Reasons, "title_overlap")
	}
	sequence := a != "" && a == b && ay != 0 && by != 0 && ay != by
	if sequence {
		out.Evidence.Declaration = 1
		out.Evidence.Reasons = append(out.Evidence.Reasons, "title_stem", "year_sequence")
		switch {
		case peer.family == "":
			out.Kind, out.EdgeType = "membership", ""
		case peer.family == family:
			out.EdgeType = "supersedes"
			if ay < by {
				out.DocumentID, out.TargetDocumentID = out.TargetDocumentID, out.DocumentID
			}
			if published != "" && out.TargetDocumentID != published {
				return FamilySuggestion{}, false
			}
		}
	} else if len([]rune(b)) >= 4 {
		if kind, declared := declaredDocumentRelation(from, to); declared {
			out.EdgeType, out.Evidence.Declaration = kind, 1
			out.Evidence.Reasons = append(out.Evidence.Reasons, "explicit_title_mention")
		} else if kind, declared := declaredDocumentRelation(to, from); declared {
			out.EdgeType, out.Evidence.Declaration = kind, 1
			out.DocumentID, out.TargetDocumentID = out.TargetDocumentID, out.DocumentID
			out.Evidence.Reasons = append(out.Evidence.Reasons, "explicit_title_mention")
		}
	}
	if out.EdgeType == "related_to" && out.DocumentID > out.TargetDocumentID {
		out.DocumentID, out.TargetDocumentID = out.TargetDocumentID, out.DocumentID
	}
	return out, len(out.Evidence.Reasons) > 0
}

func declaredDocumentRelation(from, to weknora.AttachmentDocument) (string, bool) {
	target := normalizedEvidenceText(trimTitleExtension(to.Title))
	if len([]rune(target)) < 4 || !strings.Contains(normalizedEvidenceText(trimTitleExtension(from.Title)+" "+from.Description), target) {
		return "", false
	}
	switch {
	case strings.Contains(from.Title, "修订"), strings.Contains(strings.ToLower(from.Title), "amendment"):
		return "amends", true
	case strings.Contains(from.Title, "解释"), strings.Contains(from.Title, "解读"), strings.Contains(strings.ToLower(from.Title), "interpretation"):
		return "interprets", true
	default:
		return "related_to", true
	}
}

func structuralEvidence(a, b []string) (float64, string) {
	if len(a) == 0 || len(b) == 0 {
		return 0, ""
	}
	set := make(map[string]bool, len(a))
	for _, hash := range a {
		set[hash] = true
	}
	common := 0
	for _, hash := range b {
		if set[hash] {
			common++
			delete(set, hash)
		}
	}
	if common != 0 {
		return 2 * float64(common) / float64(len(a)+len(b)), "shared_chunk_hashes"
	}
	// Equal lengths on their own are weak structural evidence.
	return .4 * float64(min(len(a), len(b))) / float64(max(len(a), len(b))), "chunk_count"
}
