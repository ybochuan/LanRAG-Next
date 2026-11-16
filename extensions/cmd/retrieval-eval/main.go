// Command retrieval-eval measures document-level retrieval quality against a
// live WeKnora Core. It issues native hybrid-search requests and computes
// Recall@k and MRR from the knowledge IDs the native search returns.
//
// It never modifies retrieval state or the knowledge base.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"
)

// Query is one evaluation item. ExpectedKnowledgeIDs are the documents
// that should be retrieved for this query; relevance is document-level.
type Query struct {
	ID                   string   `json:"id"`
	Question             string   `json:"question"`
	ExpectedKnowledgeIDs []string `json:"expected_knowledge_ids"`
	Notes                string   `json:"notes,omitempty"`
}

// Manifest is the input file: one knowledge base and its queries.
type Manifest struct {
	KnowledgeBaseID string  `json:"knowledge_base_id"`
	Queries         []Query `json:"queries"`
}

// Hit is one native search result, reduced to the fields the metric needs.
type Hit struct {
	KnowledgeID string  `json:"knowledge_id"`
	Title       string  `json:"knowledge_title,omitempty"`
	Score       float64 `json:"score,omitempty"`
}

// QueryResult records the ranked document IDs and the metric contributions.
type QueryResult struct {
	ID         string   `json:"id"`
	Question   string   `json:"question"`
	Ranked     []string `json:"ranked_knowledge_ids"`
	Expected   []string `json:"expected_knowledge_ids"`
	HitRanks   []int    `json:"hit_ranks"`
	RecallAt5  bool     `json:"recall_at_5"`
	RecallAt10 bool     `json:"recall_at_10"`
	RecallAt20 bool     `json:"recall_at_20"`
	FirstRank  int      `json:"first_rank"`
	Error      string   `json:"error,omitempty"`
	ElapsedMs  int64    `json:"elapsed_ms"`
}

// Report aggregates the per-query metrics.
type Report struct {
	GeneratedAt   string        `json:"generated_at"`
	CoreURL       string        `json:"core_url"`
	KnowledgeBase string        `json:"knowledge_base_id"`
	QueryCount    int           `json:"query_count"`
	RecallAt5     float64       `json:"recall_at_5"`
	RecallAt10    float64       `json:"recall_at_10"`
	RecallAt20    float64       `json:"recall_at_20"`
	MRR           float64       `json:"mrr"`
	FailedCount   int           `json:"failed_count"`
	Results       []QueryResult `json:"results"`
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("retrieval-eval", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifestPath := flags.String("manifest", "", "evaluation manifest JSON")
	coreURL := flags.String("core", "http://127.0.0.1:18880", "live Core base URL")
	token := flags.String("token", "", "Core JWT")
	topK := flags.Int("top-k", 20, "native result count to request (1..100)")
	timeout := flags.Duration("timeout", 60*time.Second, "per-query timeout")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.PrintDefaults()
			return nil
		}
		return err
	}
	if *manifestPath == "" || *token == "" {
		return errors.New("manifest and token required")
	}
	if *topK < 1 || *topK > 100 {
		return errors.New("top-k must be between 1 and 100")
	}
	raw, err := os.ReadFile(*manifestPath)
	if err != nil {
		return errors.New("cannot read evaluation manifest")
	}
	var manifest Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&manifest) != nil {
		return errors.New("invalid evaluation manifest")
	}
	if len(manifest.Queries) == 0 {
		return errors.New("evaluation manifest has no queries")
	}
	client := &http.Client{Timeout: *timeout}
	report := Report{GeneratedAt: time.Now().UTC().Format(time.RFC3339), CoreURL: *coreURL, KnowledgeBase: manifest.KnowledgeBaseID, QueryCount: len(manifest.Queries)}
	var sumR5, sumR10, sumR20, sumRR float64
	valid := 0
	for _, q := range manifest.Queries {
		result := evaluate(ctx, client, *coreURL, *token, manifest.KnowledgeBaseID, q, *topK, *timeout)
		report.Results = append(report.Results, result)
		if result.Error != "" {
			report.FailedCount++
			continue
		}
		valid++
		if result.RecallAt5 {
			sumR5++
		}
		if result.RecallAt10 {
			sumR10++
		}
		if result.RecallAt20 {
			sumR20++
		}
		if result.FirstRank > 0 {
			sumRR += 1 / float64(result.FirstRank)
		}
	}
	if valid > 0 {
		report.RecallAt5 = sumR5 / float64(valid)
		report.RecallAt10 = sumR10 / float64(valid)
		report.RecallAt20 = sumR20 / float64(valid)
		report.MRR = sumRR / float64(valid)
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func evaluate(ctx context.Context, client *http.Client, coreURL, token, kb string, q Query, topK int, timeout time.Duration) QueryResult {
	result := QueryResult{ID: q.ID, Question: q.Question, Expected: q.ExpectedKnowledgeIDs}
	started := time.Now()
	hits, err := search(ctx, client, coreURL, token, kb, q.Question, topK, timeout)
	result.ElapsedMs = time.Since(started).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	seen := map[string]int{}
	for i, h := range hits {
		if h.KnowledgeID == "" || seen[h.KnowledgeID] != 0 {
			continue
		}
		seen[h.KnowledgeID] = i + 1
		result.Ranked = append(result.Ranked, h.KnowledgeID)
	}
	expected := map[string]bool{}
	for _, id := range q.ExpectedKnowledgeIDs {
		expected[id] = true
	}
	for rank, id := range result.Ranked {
		if expected[id] {
			result.HitRanks = append(result.HitRanks, rank+1)
			if result.FirstRank == 0 {
				result.FirstRank = rank + 1
			}
		}
	}
	sort.Ints(result.HitRanks)
	for _, rank := range result.HitRanks {
		if rank <= 5 {
			result.RecallAt5 = true
		}
		if rank <= 10 {
			result.RecallAt10 = true
		}
		if rank <= 20 {
			result.RecallAt20 = true
		}
	}
	return result
}

func search(ctx context.Context, client *http.Client, coreURL, token, kb, question string, topK int, timeout time.Duration) ([]Hit, error) {
	payload, err := json.Marshal(map[string]any{"query_text": question, "top_k": topK})
	if err != nil {
		return nil, err
	}
	streamCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		streamCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	request, err := http.NewRequestWithContext(streamCtx, http.MethodPost, coreURL+"/api/v1/knowledge-bases/"+kb+"/hybrid-search", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("core returned HTTP %d", response.StatusCode)
	}
	var decoded struct {
		Success bool  `json:"success"`
		Data    []Hit `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&decoded); err != nil {
		return nil, err
	}
	if !decoded.Success {
		return nil, errors.New("native search reported failure")
	}
	return decoded.Data, nil
}
