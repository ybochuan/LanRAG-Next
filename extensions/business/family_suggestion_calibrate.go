package business

import (
	"context"
	"math"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// FamilyCalibrationInput is an offline, single-library manifest.
type FamilyCalibrationInput struct {
	KnowledgeBaseID string                  `json:"knowledge_base_id"`
	Pairs           []FamilyCalibrationPair `json:"pairs"`
}

// FamilyCalibrationPair labels one pair and isolates related examples by group.
type FamilyCalibrationPair struct {
	Group          string                    `json:"group"`
	Partition      string                    `json:"partition"`
	Expected       string                    `json:"expected"`
	PeerMembership string                    `json:"peer_membership,omitempty"`
	PublishedID    string                    `json:"published_document_id,omitempty"`
	Peer           FamilyCalibrationDocument `json:"peer"`
	Anchor         FamilyCalibrationDocument `json:"anchor"`
}

// FamilyCalibrationDocument holds the same non-body inputs used by discovery.
type FamilyCalibrationDocument struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	ChunkHashes []string `json:"chunk_hashes,omitempty"`
}

// FamilyCalibrationMetrics evaluates exact suggestion types against human labels.
type FamilyCalibrationMetrics struct {
	Samples       int     `json:"samples"`
	TruePositive  int     `json:"true_positive"`
	FalsePositive int     `json:"false_positive"`
	FalseNegative int     `json:"false_negative"`
	Precision     float64 `json:"precision"`
	Recall        float64 `json:"recall"`
}

// FamilyCalibrationReport reports training and holdout scores separately.
type FamilyCalibrationReport struct {
	KnowledgeBaseID string                   `json:"knowledge_base_id"`
	Policy          SuggestionPolicy         `json:"policy"`
	Train           FamilyCalibrationMetrics `json:"train"`
	Holdout         FamilyCalibrationMetrics `json:"holdout"`
	Accepted        bool                     `json:"accepted"`
	SemanticModel   string                   `json:"semantic_model,omitempty"`
}

type calibrationObservation struct {
	evidence  SuggestionEvidence
	predicted string
	expected  string
}

func calibrationKind(kind string) bool {
	switch kind {
	case "none", "membership", "amends", "interprets", "related_to", "supersedes":
		return true
	default:
		return false
	}
}

func calibrationDocuments(p FamilyCalibrationPair) (peer, anchor scanDocument, ok bool) {
	if p.Peer.ID == p.Anchor.ID || !validID(p.Peer.ID) || !validID(p.Anchor.ID) ||
		(p.PublishedID != "" && !validID(p.PublishedID)) ||
		len(p.Peer.ChunkHashes) > 4096 || len(p.Anchor.ChunkHashes) > 4096 {
		return peer, anchor, false
	}
	convert := func(d FamilyCalibrationDocument, family string) scanDocument {
		return scanDocument{doc: weknora.AttachmentDocument{ID: d.ID, Title: d.Title, Description: d.Description}, family: family}
	}
	membership := p.PeerMembership
	switch membership {
	case "", "unassigned":
		membership = ""
	case "same":
		membership = "family"
	case "other":
	default:
		return peer, anchor, false
	}
	return convert(p.Peer, membership), convert(p.Anchor, "family"), true
}

// CalibrateFamilySuggestions selects weights on training data and assesses the
// selected policy on disjoint held-out groups. It does not change any library.
func CalibrateFamilySuggestions(in FamilyCalibrationInput) (FamilyCalibrationReport, error) {
	return CalibrateFamilySuggestionsWithModel(context.Background(), in, nil)
}

// CalibrateFamilySuggestionsWithModel evaluates the same local title embeddings
// as discovery when the operator explicitly enables them.
func CalibrateFamilySuggestionsWithModel(ctx context.Context, in FamilyCalibrationInput, model *LocalFamilyEmbedder) (FamilyCalibrationReport, error) {
	out := FamilyCalibrationReport{KnowledgeBaseID: in.KnowledgeBaseID}
	if !validID(in.KnowledgeBaseID) || len(in.Pairs) < 20 || len(in.Pairs) > 50 {
		return out, ErrInvalid
	}
	folds := map[string][]calibrationObservation{"train": {}, "holdout": {}}
	groups, documents := map[string]string{}, map[string]string{}
	seen := map[[2]string]bool{}
	for _, pair := range in.Pairs {
		_, _, ok := calibrationDocuments(pair)
		key := [2]string{min(pair.Peer.ID, pair.Anchor.ID), max(pair.Peer.ID, pair.Anchor.ID)}
		if !ok || !validID(pair.Group) || !calibrationKind(pair.Expected) || seen[key] ||
			(pair.Partition != "train" && pair.Partition != "holdout") {
			return out, ErrInvalid
		}
		if !sameCalibrationFold(groups, pair.Group, pair.Partition) ||
			!sameCalibrationFold(documents, pair.Peer.ID, pair.Partition) ||
			!sameCalibrationFold(documents, pair.Anchor.ID, pair.Partition) {
			return out, ErrInvalid
		}
		seen[key] = true
	}
	var vectors map[string][]float64
	if model != nil {
		var err error
		vectors, err = calibrationVectors(ctx, in.Pairs, model)
		if err != nil {
			return out, err
		}
		out.SemanticModel = model.model
	}
	for _, pair := range in.Pairs {
		peer, anchor, _ := calibrationDocuments(pair)
		item, suggested := candidateSignals("family", pair.PublishedID, peer, anchor)
		if item.Kind != "" && model != nil {
			suggested = applyFamilySemanticEvidence(&item.Evidence, vectors[pair.Peer.ID], vectors[pair.Anchor.ID], model.model) || suggested
		}
		sample := calibrationObservation{expected: pair.Expected, predicted: "none"}
		if suggested {
			sample.predicted = item.EdgeType
			if item.Kind == "membership" {
				sample.predicted = "membership"
			}
			sample.evidence = item.Evidence
			sample.evidence.Structure, _ = structuralEvidence(pair.Peer.ChunkHashes, pair.Anchor.ChunkHashes)
		}
		folds[pair.Partition] = append(folds[pair.Partition], sample)
	}
	for _, samples := range folds {
		positive := 0
		for _, row := range samples {
			if row.expected != "none" {
				positive++
			}
		}
		if positive == 0 || positive == len(samples) {
			return out, ErrInvalid
		}
	}
	bestF1, bestDistance := -1., math.Inf(1)
	for a := 0; a <= 20; a++ {
		for b := 0; b <= 20-a; b++ {
			p := SuggestionPolicy{SimilarityWeight: float64(a) / 20, DeclarationWeight: float64(b) / 20, StructureWeight: float64(20-a-b) / 20}
			for threshold := 1; threshold <= 20; threshold++ {
				p.Threshold = float64(threshold) / 20
				metrics := calibrationMetrics(folds["train"], p)
				f1 := 0.
				if metrics.Precision+metrics.Recall > 0 {
					f1 = 2 * metrics.Precision * metrics.Recall / (metrics.Precision + metrics.Recall)
				}
				distance := math.Abs(p.SimilarityWeight-.6) + math.Abs(p.DeclarationWeight-.25) +
					math.Abs(p.StructureWeight-.15) + math.Abs(p.Threshold-.70)
				eligible, bestEligible := calibrationAccepted(metrics), calibrationAccepted(out.Train)
				if eligible && !bestEligible || eligible == bestEligible &&
					(f1 > bestF1 || f1 == bestF1 && distance < bestDistance) {
					out.Policy, out.Train, bestF1, bestDistance = p, metrics, f1, distance
				}
			}
		}
	}
	out.Holdout = calibrationMetrics(folds["holdout"], out.Policy)
	out.Accepted = calibrationAccepted(out.Train) && calibrationAccepted(out.Holdout)
	return out, nil
}

func calibrationVectors(ctx context.Context, pairs []FamilyCalibrationPair, model *LocalFamilyEmbedder) (map[string][]float64, error) {
	var ids, titles []string
	seen := map[string]string{}
	for _, pair := range pairs {
		for _, doc := range [2]FamilyCalibrationDocument{pair.Peer, pair.Anchor} {
			if title, ok := seen[doc.ID]; ok {
				if title != doc.Title {
					return nil, ErrInvalid
				}
				continue
			}
			seen[doc.ID] = doc.Title
			ids, titles = append(ids, doc.ID), append(titles, doc.Title)
		}
	}
	vectors := make(map[string][]float64, len(ids))
	dimension := 0
	for start := 0; start < len(ids); start += familyEmbeddingBatch {
		batch, err := model.embed(ctx, titles[start:min(start+familyEmbeddingBatch, len(ids))])
		if err != nil {
			return nil, err
		}
		if dimension == 0 {
			dimension = len(batch[0])
		} else if len(batch[0]) != dimension {
			return nil, ErrUnavailable
		}
		for i, vector := range batch {
			vectors[ids[start+i]] = vector
		}
	}
	return vectors, nil
}
func sameCalibrationFold(assignments map[string]string, key, fold string) bool {
	if old := assignments[key]; old != "" {
		return old == fold
	}
	assignments[key] = fold
	return true
}

func calibrationAccepted(metrics FamilyCalibrationMetrics) bool {
	return metrics.Precision >= .70 && metrics.Recall >= .60
}

func calibrationMetrics(samples []calibrationObservation, p SuggestionPolicy) FamilyCalibrationMetrics {
	out := FamilyCalibrationMetrics{Samples: len(samples)}
	for _, row := range samples {
		predicted := "none"
		if row.predicted != "none" && p.confidence(row.evidence) >= p.Threshold {
			predicted = row.predicted
		}
		if predicted == row.expected {
			if predicted != "none" {
				out.TruePositive++
			}
			continue
		}
		if predicted != "none" {
			out.FalsePositive++
		}
		if row.expected != "none" {
			out.FalseNegative++
		}
	}
	if total := out.TruePositive + out.FalsePositive; total > 0 {
		out.Precision = float64(out.TruePositive) / float64(total)
	}
	if total := out.TruePositive + out.FalseNegative; total > 0 {
		out.Recall = float64(out.TruePositive) / float64(total)
	}
	return out
}
