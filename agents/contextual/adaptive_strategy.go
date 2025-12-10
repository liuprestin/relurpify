package contextual

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// StrategyMode controls adaptive strategy personalities.
type StrategyMode string

const (
	ModeAggressive   StrategyMode = "aggressive"
	ModeBalanced     StrategyMode = "balanced"
	ModeConservative StrategyMode = "conservative"
)

// AdaptiveStrategy adjusts retrieval behaviour using task complexity and past success.
type AdaptiveStrategy struct {
	contextLoadHistory []ContextLoadEvent
	successRate        map[string]float64
	currentMode        StrategyMode
	lowSuccessThreshold,
	highSuccessThreshold float64
}

// NewAdaptiveStrategy returns a ready adaptive strategy.
func NewAdaptiveStrategy() *AdaptiveStrategy {
	return &AdaptiveStrategy{
		contextLoadHistory:   make([]ContextLoadEvent, 0),
		successRate:          make(map[string]float64),
		currentMode:          ModeBalanced,
		lowSuccessThreshold:  0.6,
		highSuccessThreshold: 0.85,
	}
}

// SelectContext delegates to the current mode.
func (as *AdaptiveStrategy) SelectContext(task *framework.Task, budget *framework.ContextBudget) (*ContextRequest, error) {
	if task == nil {
		return nil, fmt.Errorf("task required")
	}
	if budget == nil {
		return nil, fmt.Errorf("budget required")
	}
	complexity := as.analyzeTaskComplexity(task)
	as.adjustMode(complexity)
	switch as.currentMode {
	case ModeAggressive:
		return NewAggressiveStrategy().SelectContext(task, budget)
	case ModeConservative:
		return NewConservativeStrategy().SelectContext(task, budget)
	default:
		return as.selectBalancedContext(task, budget)
	}
}

// ShouldCompress adapts threshold based on mode.
func (as *AdaptiveStrategy) ShouldCompress(ctx *framework.SharedContext) bool {
	if ctx == nil {
		return false
	}
	history := len(ctx.History())
	switch as.currentMode {
	case ModeAggressive:
		return history > 5
	case ModeConservative:
		return history > 15
	default:
		return history > 10
	}
}

// DetermineDetailLevel returns mode-specific detail.
func (as *AdaptiveStrategy) DetermineDetailLevel(file string, relevance float64) DetailLevel {
	switch as.currentMode {
	case ModeAggressive:
		if relevance > 0.9 {
			return DetailDetailed
		}
		return DetailConcise
	case ModeConservative:
		if relevance > 0.8 {
			return DetailFull
		}
		if relevance > 0.5 {
			return DetailDetailed
		}
		return DetailConcise
	default:
		if relevance > 0.85 {
			return DetailFull
		}
		if relevance > 0.6 {
			return DetailDetailed
		}
		return DetailConcise
	}
}

// ShouldExpandContext reacts to failures or uncertainty.
func (as *AdaptiveStrategy) ShouldExpandContext(ctx *framework.SharedContext, lastResult *framework.Result) bool {
	if lastResult == nil {
		return false
	}
	event := ContextLoadEvent{
		Timestamp: time.Now(),
		Success:   lastResult.Success,
	}
	if !lastResult.Success {
		event.Trigger = "failure"
		as.contextLoadHistory = append(as.contextLoadHistory, event)
		return true
	}
	if output, ok := lastResult.Data["llm_output"].(string); ok {
		markers := []string{
			"not sure", "unclear", "need more information",
			"cannot determine", "insufficient",
		}
		for _, marker := range markers {
			if ContainsInsensitive(output, marker) {
				event.Trigger = "uncertainty"
				as.contextLoadHistory = append(as.contextLoadHistory, event)
				return true
			}
		}
	}
	as.contextLoadHistory = append(as.contextLoadHistory, event)
	return false
}

// PrioritizeContext combines relevance and recency.
func (as *AdaptiveStrategy) PrioritizeContext(items []framework.ContextItem) []framework.ContextItem {
	sorted := append([]framework.ContextItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		scoreI := sorted[i].RelevanceScore()*0.6 + (1.0/(1.0+sorted[i].Age().Hours()))*0.4
		scoreJ := sorted[j].RelevanceScore()*0.6 + (1.0/(1.0+sorted[j].Age().Hours()))*0.4
		return scoreI > scoreJ
	})
	return sorted
}

func (as *AdaptiveStrategy) analyzeTaskComplexity(task *framework.Task) int {
	if task == nil {
		return 0
	}
	complexity := 0
	inst := task.Instruction
	if len(inst) > 500 {
		complexity += 2
	}
	if countKeywords(inst, []string{"refactor", "redesign", "architecture"}) > 0 {
		complexity += 3
	}
	if countKeywords(inst, []string{"fix", "bug", "error", "debug"}) > 0 {
		complexity += 1
	}
	if countKeywords(inst, []string{"add", "implement", "create"}) > 0 {
		complexity += 2
	}
	if task.Metadata != nil {
		if taskType, ok := task.Metadata["type"]; ok {
			switch strings.ToLower(taskType) {
			case "exploration":
				complexity++
			case "modification":
				complexity += 2
			case "creation":
				complexity += 3
			}
		}
	}
	return complexity
}

func (as *AdaptiveStrategy) adjustMode(complexity int) {
	recent := as.contextLoadHistory
	if len(recent) > 10 {
		recent = recent[len(recent)-10:]
	}
	successCount := 0
	for _, event := range recent {
		if event.Success {
			successCount++
		}
	}
	successRate := 0.0
	if len(recent) > 0 {
		successRate = float64(successCount) / float64(len(recent))
	}
	switch {
	case successRate < as.lowSuccessThreshold:
		as.currentMode = ModeConservative
	case successRate > as.highSuccessThreshold && complexity < 3:
		as.currentMode = ModeAggressive
	default:
		as.currentMode = ModeBalanced
	}
}

func (as *AdaptiveStrategy) selectBalancedContext(task *framework.Task, budget *framework.ContextBudget) (*ContextRequest, error) {
	if budget == nil {
		return nil, fmt.Errorf("budget required")
	}
	request := &ContextRequest{
		Files:         make([]FileRequest, 0),
		ASTQueries:    make([]ASTQuery, 0, 1),
		SearchQueries: make([]SearchQuery, 0, 1),
		MaxTokens:     budget.AvailableForContext / 2,
	}
	request.ASTQueries = append(request.ASTQueries, ASTQuery{
		Type: ASTQueryListSymbols,
		Filter: ASTFilter{
			ExportedOnly: true,
		},
	})
	files := ExtractFileReferences(task.Instruction)
	for i, file := range files {
		priority := 0
		if i > 2 {
			priority = 1
		}
		request.Files = append(request.Files, FileRequest{
			Path:        file,
			DetailLevel: DetailConcise,
			Priority:    priority,
			Pinned:      i < 2,
		})
	}
	request.SearchQueries = append(request.SearchQueries, SearchQuery{
		Query:      ExtractKeywords(task.Instruction),
		Mode:       framework.SearchHybrid,
		MaxResults: 10,
	})
	return request, nil
}
