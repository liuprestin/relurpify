package contextual

import (
	"fmt"
	"sort"

	"github.com/lexcodex/relurpify/framework"
)

// ConservativeStrategy loads more context upfront to minimize tool calls.
type ConservativeStrategy struct {
	preloadDepth     int
	compressionPoint float64
}

// NewConservativeStrategy builds a tuned conservative strategy.
func NewConservativeStrategy() *ConservativeStrategy {
	return &ConservativeStrategy{
		preloadDepth:     2,
		compressionPoint: 0.85,
	}
}

// SelectContext eagerly loads referenced files and related metadata.
func (cs *ConservativeStrategy) SelectContext(task *framework.Task, budget *framework.ContextBudget) (*ContextRequest, error) {
	if task == nil {
		return nil, fmt.Errorf("task required")
	}
	if budget == nil {
		return nil, fmt.Errorf("budget required")
	}
	request := &ContextRequest{
		Files:         make([]FileRequest, 0),
		ASTQueries:    make([]ASTQuery, 0, 2),
		SearchQueries: make([]SearchQuery, 0),
		MemoryQueries: make([]MemoryQuery, 0),
		MaxTokens:     budget.AvailableForContext * 3 / 4,
	}
	request.ASTQueries = append(request.ASTQueries, ASTQuery{
		Type: ASTQueryListSymbols,
		Filter: ASTFilter{
			ExportedOnly: false,
		},
	})
	files := ExtractFileReferences(task.Instruction)
	if len(files) > 0 {
		for _, file := range files {
			request.Files = append(request.Files, FileRequest{
				Path:        file,
				DetailLevel: DetailDetailed,
				Priority:    0,
				Pinned:      true,
			})
			request.ASTQueries = append(request.ASTQueries, ASTQuery{
				Type:   ASTQueryGetDependencies,
				Symbol: file,
			})
		}
	} else {
		request.SearchQueries = append(request.SearchQueries, SearchQuery{
			Query:      task.Instruction,
			Mode:       framework.SearchHybrid,
			MaxResults: 20,
		})
	}
	request.MemoryQueries = append(request.MemoryQueries, MemoryQuery{
		Scope:      framework.MemoryScopeProject,
		Query:      task.Instruction,
		MaxResults: 10,
	})
	return request, nil
}

// ShouldCompress waits until history grows significantly.
func (cs *ConservativeStrategy) ShouldCompress(ctx *framework.SharedContext) bool {
	if ctx == nil {
		return false
	}
	return len(ctx.History()) > 15
}

// DetermineDetailLevel defaults to detailed/full content.
func (cs *ConservativeStrategy) DetermineDetailLevel(file string, relevance float64) DetailLevel {
	switch {
	case relevance > 0.8:
		return DetailFull
	case relevance > 0.5:
		return DetailDetailed
	default:
		return DetailConcise
	}
}

// ShouldExpandContext proactively expands after retrieval steps.
func (cs *ConservativeStrategy) ShouldExpandContext(ctx *framework.SharedContext, lastResult *framework.Result) bool {
	if lastResult == nil || lastResult.Data == nil {
		return false
	}
	if tool, ok := lastResult.Data["tool_used"].(string); ok {
		return tool == "search" || tool == "query_ast"
	}
	return false
}

// PrioritizeContext sorts by relevance score.
func (cs *ConservativeStrategy) PrioritizeContext(items []framework.ContextItem) []framework.ContextItem {
	sorted := append([]framework.ContextItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].RelevanceScore() > sorted[j].RelevanceScore()
	})
	return sorted
}
