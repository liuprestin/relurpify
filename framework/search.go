package framework

import (
	"context"
	"fmt"
	"strings"
)

// SearchMode determines which retrieval strategy the engine should prefer.
type SearchMode int

const (
	SearchSemantic SearchMode = iota
	SearchKeyword
	SearchHybrid
	SearchSymbolOnly
)

// SearchQuery configures a context retrieval request.
type SearchQuery struct {
	Text           string
	Mode           SearchMode
	FilePatterns   []string
	MaxResults     int
	IncludeSummary bool
}

// SearchResult unifies snippets returned by semantic, keyword, or symbol searches.
type SearchResult struct {
	File          string
	ChunkID       string
	Snippet       string
	Summary       string
	Score         float64
	RelevanceType string
	StartLine     int
	EndLine       int
}

// VectorMatch captures a semantic match returned by a vector store.
type VectorMatch struct {
	ID       string
	Content  string
	Metadata map[string]any
	Score    float64
}

// SemanticStore is the minimal interface required from a vector store.
type SemanticStore interface {
	Query(ctx context.Context, query string, limit int) ([]VectorMatch, error)
}

// SearchEngine orchestrates multiple search backends (vector store + code index).
type SearchEngine struct {
	vectorStore SemanticStore
	codeIndex   CodeIndex
}

// NewSearchEngine returns a ready-to-use hybrid search instance.
func NewSearchEngine(vs SemanticStore, idx CodeIndex) *SearchEngine {
	return &SearchEngine{
		vectorStore: vs,
		codeIndex:   idx,
	}
}

// Search executes the configured query using semantic and/or keyword retrieval.
func (se *SearchEngine) Search(q SearchQuery) ([]SearchResult, error) {
	switch q.Mode {
	case SearchSemantic:
		return se.semanticSearch(q)
	case SearchKeyword:
		return se.keywordSearch(q)
	case SearchSymbolOnly:
		return se.symbolSearch(q)
	default:
		semantic, err := se.semanticSearch(q)
		if err != nil {
			return nil, err
		}
		keyword, err := se.keywordSearch(q)
		if err != nil {
			return nil, err
		}
		return mergeResults(semantic, keyword, q.MaxResults), nil
	}
}

// SearchWithBudget executes the query but stops once the aggregated snippet
// tokens exceed the budget.
func (se *SearchEngine) SearchWithBudget(q SearchQuery, tokenBudget int) ([]SearchResult, error) {
	results, err := se.Search(q)
	if err != nil {
		return nil, err
	}
	if tokenBudget <= 0 {
		return results, nil
	}
	pruned := make([]SearchResult, 0, len(results))
	remaining := tokenBudget
	for _, result := range results {
		snippetCost := estimateTokens(result.Snippet)
		summaryCost := estimateTokens(result.Summary)
		totalCost := snippetCost + summaryCost
		switch {
		case totalCost <= remaining:
			pruned = append(pruned, result)
			remaining -= totalCost
		case summaryCost > 0 && summaryCost <= remaining:
			trimmed := result
			trimmed.Snippet = ""
			pruned = append(pruned, trimmed)
			remaining -= summaryCost
		default:
			continue
		}
	}
	return pruned, nil
}

func (se *SearchEngine) semanticSearch(q SearchQuery) ([]SearchResult, error) {
	if se.vectorStore == nil {
		return nil, nil
	}
	maxResults := q.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	matches, err := se.vectorStore.Query(context.Background(), q.Text, maxResults)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(matches))
	for _, match := range matches {
		file := ""
		if match.Metadata != nil {
			if path, ok := match.Metadata["path"].(string); ok {
				file = path
			}
		}
		results = append(results, SearchResult{
			File:          file,
			ChunkID:       fmt.Sprintf("semantic-%d", len(results)+1),
			Snippet:       match.Content,
			Summary:       "",
			Score:         match.Score,
			RelevanceType: "semantic",
		})
	}
	return results, nil
}

func (se *SearchEngine) keywordSearch(q SearchQuery) ([]SearchResult, error) {
	if se.codeIndex == nil || q.Text == "" {
		return nil, nil
	}
	maxResults := q.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	lowered := strings.ToLower(q.Text)
	chunks := se.codeIndex.SearchChunks(lowered, maxResults)
	results := make([]SearchResult, 0, len(chunks))
	for _, chunk := range chunks {
		results = append(results, SearchResult{
			File:          chunk.File,
			ChunkID:       chunk.ID,
			Snippet:       chunk.Preview,
			Summary:       chunk.Summary,
			Score:         float64(chunk.TokenCount),
			RelevanceType: "keyword",
			StartLine:     chunk.StartLine,
			EndLine:       chunk.EndLine,
		})
	}
	return results, nil
}

func (se *SearchEngine) symbolSearch(q SearchQuery) ([]SearchResult, error) {
	if se.codeIndex == nil || q.Text == "" {
		return nil, nil
	}
	symbols, err := se.codeIndex.GetSymbolsByName(q.Text)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(symbols))
	for _, loc := range symbols {
		results = append(results, SearchResult{
			File:          loc.File,
			ChunkID:       loc.Symbol.Name,
			Snippet:       loc.Symbol.Signature,
			Summary:       loc.Symbol.DocString,
			Score:         1.0,
			RelevanceType: "symbol",
			StartLine:     loc.Line,
			EndLine:       loc.Line,
		})
	}
	return results, nil
}

func mergeResults(a, b []SearchResult, limit int) []SearchResult {
	out := append([]SearchResult{}, a...)
	out = append(out, b...)
	if limit <= 0 || len(out) <= limit {
		return out
	}
	return out[:limit]
}
