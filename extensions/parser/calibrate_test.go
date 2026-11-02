package parser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCalibrationMetricsArePerKBAndRejectLeakage(t *testing.T) {
	root := t.TempDir()
	makeSample := func(doc, split string, want Tier) CalibrationSample {
		b := append(testPDF(), []byte("\n%"+doc)...)
		sum := sha256.Sum256(b)
		if err := os.WriteFile(filepath.Join(root, doc+".pdf"), b, 0600); err != nil {
			t.Fatal(err)
		}
		return CalibrationSample{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: doc, Path: doc + ".pdf", SHA256: hex.EncodeToString(sum[:]), Split: split, ExpectedTier: want}
	}
	manifest := CalibrationManifest{Version: 1, Samples: []CalibrationSample{makeSample("train", "train", TierFlash), makeSample("valid1", "validation", TierFlash), makeSample("valid2", "validation", TierBasic)}}
	report, err := Calibrate(context.Background(), root, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.KnowledgeBases) != 1 || report.Accepted {
		t.Fatalf("unexpected acceptance: %+v", report)
	}
	m := report.KnowledgeBases[0]
	if m.Total != 2 || m.Correct != 1 || m.Underestimated != 1 || m.Accuracy != 0.5 || m.UnderestimationRate != 0.5 {
		t.Fatalf("bad held-out metrics: %+v", m)
	}
	manifest.Samples = append(manifest.Samples, manifest.Samples[0])
	manifest.Samples[3].Split = "validation"
	if _, err := Calibrate(context.Background(), root, manifest); err == nil {
		t.Fatal("train/validation leakage accepted")
	}
}

func TestCalibrationRejectsEmptyTamperedAndEscapingInputs(t *testing.T) {
	if _, err := Calibrate(context.Background(), t.TempDir(), CalibrationManifest{Version: 1}); err == nil {
		t.Fatal("empty corpus passed")
	}
	for _, path := range []string{"../outside.pdf", "/outside.pdf", "missing.pdf"} {
		manifest := CalibrationManifest{Version: 1, Samples: []CalibrationSample{{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc", Path: path, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Split: "validation", ExpectedTier: TierFlash}}}
		if _, err := Calibrate(context.Background(), t.TempDir(), manifest); err == nil {
			t.Fatalf("invalid corpus path accepted: %s", path)
		}
	}
}
