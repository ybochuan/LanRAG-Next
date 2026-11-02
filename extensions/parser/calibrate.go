package parser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CalibrationManifest supplies human labels, never inferred expected answers.
// Training samples document the separate fit corpus; this tool measures only
// held-out validation samples and does not tune rules on the validation set.
type CalibrationManifest struct {
	Version int                 `json:"version"`
	Samples []CalibrationSample `json:"samples"`
}

// CalibrationSample identifies an immutable PDF and its independently assigned tier.
type CalibrationSample struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	DocumentID      string `json:"document_id"`
	Path            string `json:"path"`
	SHA256          string `json:"sha256"`
	Split           string `json:"split"` // train or validation; split by document family.
	ExpectedTier    Tier   `json:"expected_tier"`
}

// KBMetrics contains rates with the held-out sample count as denominator.
type KBMetrics struct {
	ScopeIdentity
	Total               int     `json:"total"`
	Correct             int     `json:"correct"`
	Overestimated       int     `json:"overestimated"`
	Underestimated      int     `json:"underestimated"`
	Rejected            int     `json:"rejected"`
	Accuracy            float64 `json:"accuracy"`
	OverestimationRate  float64 `json:"overestimation_rate"`
	UnderestimationRate float64 `json:"underestimation_rate"`
	Accepted            bool    `json:"accepted"`
}

// CalibrationReport is an offline measurement, not an OCR or extraction quality assessment.
type CalibrationReport struct {
	KnowledgeBases []KBMetrics `json:"knowledge_bases"`
	Accepted       bool        `json:"accepted"`
}

// Calibrate reads bounded, hash-verified local PDFs using the actual preflight
// and effective route. Duplicate bytes or document families across samples are
// refused; semantic near-duplicate detection remains the corpus curator's job.
func Calibrate(ctx context.Context, root string, manifest CalibrationManifest) (CalibrationReport, error) {
	if ctx == nil || manifest.Version != 1 || len(manifest.Samples) == 0 || len(manifest.Samples) > 100000 {
		return CalibrationReport{}, errors.New("version 1 nonempty labeled corpus required")
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return CalibrationReport{}, err
	}
	defer files.Close()
	metrics := map[ScopeIdentity]*KBMetrics{}
	training := map[ScopeIdentity]int{}
	hashes := map[string]bool{}
	families := map[string]bool{}
	rank := map[Tier]int{TierFlash: 0, TierBasic: 1, TierStandard: 2, TierAdvanced: 3}
	for index, s := range manifest.Samples {
		if err := ctx.Err(); err != nil {
			return CalibrationReport{}, err
		}
		_, validTier := rank[s.ExpectedTier]
		rawHash, hashErr := hex.DecodeString(s.SHA256)
		family := fmt.Sprintf("%d/%s/%s", s.TenantID, s.KnowledgeBaseID, s.DocumentID)
		if s.TenantID == 0 || !safeID(s.KnowledgeBaseID) || !safeID(s.DocumentID) || !validTier || (s.Split != "train" && s.Split != "validation") || hashErr != nil || len(rawHash) != 32 || hashes[s.SHA256] || families[family] || !filepath.IsLocal(s.Path) || strings.Contains(s.Path, "\\") || strings.ToLower(filepath.Ext(s.Path)) != ".pdf" {
			return CalibrationReport{}, fmt.Errorf("invalid or duplicate calibration sample %d", index)
		}
		info, err := files.Lstat(s.Path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > proxySourceLimit {
			return CalibrationReport{}, fmt.Errorf("unreadable calibration sample %d", index)
		}
		f, err := files.Open(s.Path)
		if err != nil {
			return CalibrationReport{}, fmt.Errorf("unreadable calibration sample %d", index)
		}
		b, readErr := io.ReadAll(io.LimitReader(f, proxySourceLimit+1))
		closeErr := f.Close()
		if readErr != nil || closeErr != nil || len(b) > proxySourceLimit {
			return CalibrationReport{}, fmt.Errorf("oversized calibration sample %d", index)
		}
		sum := sha256.Sum256(b)
		actualHash := hex.EncodeToString(sum[:])
		if actualHash != strings.ToLower(s.SHA256) || hashes[actualHash] {
			return CalibrationReport{}, fmt.Errorf("hash mismatch or duplicate calibration sample %d", index)
		}
		hashes[actualHash] = true
		families[family] = true
		scope := ScopeIdentity{s.TenantID, s.KnowledgeBaseID}
		if metrics[scope] == nil {
			metrics[scope] = &KBMetrics{ScopeIdentity: scope}
		}
		if s.Split == "train" {
			training[scope]++
			continue
		}
		m := metrics[scope]
		m.Total++
		preflight, err := PreflightPDF(ctx, b)
		if err != nil {
			m.Rejected++
			continue
		}
		decision, err := DecidePDF(preflight)
		if err != nil {
			m.Rejected++
			continue
		}
		predicted, expected := rank[decision.Tier], rank[s.ExpectedTier]
		if predicted == expected {
			m.Correct++
		} else if predicted > expected {
			m.Overestimated++
		} else {
			m.Underestimated++
		}
	}
	report := CalibrationReport{Accepted: true}
	for scope, m := range metrics {
		if m.Total == 0 || training[scope] == 0 {
			return CalibrationReport{}, errors.New("each KB requires separate train and validation samples")
		}
		m.Accuracy = float64(m.Correct) / float64(m.Total)
		m.OverestimationRate = float64(m.Overestimated) / float64(m.Total)
		m.UnderestimationRate = float64(m.Underestimated) / float64(m.Total)
		m.Accepted = m.Accuracy >= 0.85 && m.OverestimationRate <= 0.10 && m.UnderestimationRate <= 0.15 && m.Rejected == 0
		report.Accepted = report.Accepted && m.Accepted
		report.KnowledgeBases = append(report.KnowledgeBases, *m)
	}
	sort.Slice(report.KnowledgeBases, func(i, j int) bool {
		a, b := report.KnowledgeBases[i], report.KnowledgeBases[j]
		if a.TenantID != b.TenantID {
			return a.TenantID < b.TenantID
		}
		return a.KnowledgeBaseID < b.KnowledgeBaseID
	})
	return report, nil
}
