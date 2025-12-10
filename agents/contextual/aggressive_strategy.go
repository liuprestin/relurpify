package contextual

import (
	"fmt"
	"sort"

	"github.com/lexcodex/relurpify/framework"
)

// AggressiveStrategy minimizes upfront context, relying on dynamic retrieval.
type AggressiveStrategy struct {
	minimalThreshold  float64
	expansionTriggers int
}

// NewAggressiveStrategy constructs a tuned aggressive strategy.
func NewAggressiveStrategy() *AggressiveStrategy {
	return &AggressiveStrategy{
		minimalThreshold:  0.6,
		expansionTriggers: 2,
	}
}

// SelectContext picks a minimal set of context for the task.
func (as *AggressiveStrategy) SelectContext(task *framework.Task, budget *framework.ContextBudget) (*ContextRequest, error) {
	if task == nil {
		return nil, fmt.Errorf("task required")
	}
	if budget == nil {
		return nil, fmt.Errorf("budget required")
	}
	request := &ContextRequest{
		Files:      make([]FileRequest, 0),
		ASTQueries: make([]ASTQuery, 0, 1),
		MaxTokens:  budget.AvailableForContext / 4,
	}
	request.ASTQueries = append(request.ASTQueries, ASTQuery{
		Type: ASTQueryListSymbols,
		Filter: ASTFilter{
			ExportedOnly: true,
		},
	})
	for _, file := range ExtractFileReferences(task.Instruction) {
		request.Files = append(request.Files, FileRequest{
			Path:        file,
			DetailLevel: DetailSignatureOnly,
			Priority:    1,
		})
	}
	return request, nil
}

// ShouldCompress aggressively trims conversation history.
func (as *AggressiveStrategy) ShouldCompress(ctx *framework.SharedContext) bool {
	if ctx == nil {
		return false
	}
	return len(ctx.History()) > 5
}

// DetermineDetailLevel escalates detail slowly as relevance increases.
func (as *AggressiveStrategy) DetermineDetailLevel(file string, relevance float64) DetailLevel {
	switch {
	case relevance > 0.9:
		return DetailDetailed
	case relevance > 0.7:
		return DetailConcise
	case relevance > 0.5:
		return DetailMinimal
	default:
		return DetailSignatureOnly
	}
}

// ShouldExpandContext reacts only to explicit failures.
func (as *AggressiveStrategy) ShouldExpandContext(ctx *framework.SharedContext, lastResult *framework.Result) bool {
	if lastResult == nil || lastResult.Data == nil {
		return false
	}
	if lastResult.Success {
		return false
	}
	if errorType, ok := lastResult.Data["error_type"].(string); ok {
		return errorType == "insufficient_context" || errorType == "file_not_found"
	}
	return false
}

// PrioritizeContext favors recency over relevance.
func (as *AggressiveStrategy) PrioritizeContext(items []framework.ContextItem) []framework.ContextItem {
	sorted := append([]framework.ContextItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Age() < sorted[j].Age()
	})
	return sorted
}
