package agents

import (
	"testing"

	"github.com/lexcodex/relurpify/framework"
)

func TestAggressiveStrategySelectContext(t *testing.T) {
	strategy := NewAggressiveStrategy()
	task := &framework.Task{
		ID:          "test-1",
		Instruction: "Fix bug in user authentication",
	}
	budget := framework.NewContextBudget(8000)
	request, err := strategy.SelectContext(task, budget)
	if err != nil {
		t.Fatalf("SelectContext failed: %v", err)
	}
	if request == nil {
		t.Fatalf("expected request, got nil")
	}
	if request.MaxTokens > budget.AvailableForContext/2 {
		t.Fatalf("aggressive strategy using too much budget: %d", request.MaxTokens)
	}
	if len(request.ASTQueries) == 0 {
		t.Fatalf("expected ast queries to bootstrap context")
	}
}

func TestConservativeStrategyBudgetUsage(t *testing.T) {
	strategy := NewConservativeStrategy()
	task := &framework.Task{
		ID:          "test-2",
		Instruction: "Refactor authentication module",
	}
	budget := framework.NewContextBudget(8000)
	request, err := strategy.SelectContext(task, budget)
	if err != nil {
		t.Fatalf("SelectContext failed: %v", err)
	}
	if request.MaxTokens < budget.AvailableForContext/2 {
		t.Fatalf("conservative strategy should allocate larger budget, got %d", request.MaxTokens)
	}
	if len(request.Files) == 0 && len(request.SearchQueries) == 0 {
		t.Fatalf("conservative strategy should preload files or run searches")
	}
}

func TestAdaptiveStrategyExpandsOnFailure(t *testing.T) {
	strategy := NewAdaptiveStrategy()
	shared := framework.NewSharedContext(framework.NewContext(), framework.NewContextBudget(2048), &framework.SimpleSummarizer{})
	result := &framework.Result{
		Success: false,
		Data: map[string]any{
			"error_type": "insufficient_context",
		},
	}
	if !strategy.ShouldExpandContext(shared, result) {
		t.Fatalf("adaptive strategy should expand context on failure")
	}
}
