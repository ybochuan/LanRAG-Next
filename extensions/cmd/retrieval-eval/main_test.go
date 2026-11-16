package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluateComputesDocumentRecallAndMRR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []map[string]any{
				{"knowledge_id": "other", "score": 0.9},
				{"knowledge_id": "want2", "score": 0.8},
				{"knowledge_id": "want1", "score": 0.7},
			},
		})
	}))
	defer server.Close()
	q := Query{ID: "Q1", Question: "q", ExpectedKnowledgeIDs: []string{"want1", "want2"}}
	r := evaluate(context.Background(), server.Client(), server.URL, "token", "kb", q, 20, 0)
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if !r.RecallAt5 || !r.RecallAt10 || !r.RecallAt20 {
		t.Fatalf("expected recall at every cutoff, got %+v", r)
	}
	if r.FirstRank != 2 {
		t.Fatalf("first rank = %d, want 2", r.FirstRank)
	}
	if len(r.HitRanks) != 2 || r.HitRanks[0] != 2 || r.HitRanks[1] != 3 {
		t.Fatalf("hit ranks = %v, want [2 3]", r.HitRanks)
	}
}

func TestEvaluateReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	q := Query{ID: "Q1", Question: "q", ExpectedKnowledgeIDs: []string{"want"}}
	r := evaluate(context.Background(), server.Client(), server.URL, "token", "kb", q, 20, 0)
	if r.Error == "" {
		t.Fatal("expected an HTTP failure")
	}
}

func TestRunAggregatesMetricsAcrossQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []map[string]any{{"knowledge_id": "want"}}})
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	body := `{"knowledge_base_id":"kb","queries":[{"id":"Q1","question":"q1","expected_knowledge_ids":["want"]},{"id":"Q2","question":"q2","expected_knowledge_ids":["other"]}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var out os.File
	_ = out
	reportFile, err := os.CreateTemp(dir, "report-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer reportFile.Close()
	if err := run(context.Background(), []string{"--manifest", path, "--token", "t", "--core", server.URL}, reportFile); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(reportFile.Name())
	var report Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.QueryCount != 2 || report.FailedCount != 0 {
		t.Fatalf("bad report: %+v", report)
	}
	if report.RecallAt5 != 0.5 || report.MRR != 0.5 {
		t.Fatalf("recall@5=%v mrr=%v, want 0.5/0.5", report.RecallAt5, report.MRR)
	}
}

func TestRunRejectsBadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte(`{"knowledge_base_id":"kb","queries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"--manifest", path, "--token", "t"}, os.Stdout); err == nil {
		t.Fatal("expected an empty-queries failure")
	}
}
