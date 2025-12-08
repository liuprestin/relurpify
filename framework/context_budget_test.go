package framework

import "testing"

func TestContextBudgetUpdateUsage(t *testing.T) {
	ctx := NewContext()
	ctx.AddInteraction("user", "hello world", nil)
	budget := NewContextBudget(8000)
	budget.UpdateUsage(ctx, nil)
	usage := budget.GetCurrentUsage()
	if usage.ContextTokens == 0 {
		t.Fatal("expected context tokens to be tracked")
	}
	if usage.TotalTokens == 0 {
		t.Fatal("expected total tokens to be computed")
	}
}

func TestContextBudgetReservations(t *testing.T) {
	budget := NewContextBudget(4000)
	budget.SetReservations(500, 500, 500)
	if budget.AvailableForContext != 2500 {
		t.Fatalf("expected available context 2500, got %d", budget.AvailableForContext)
	}
}

func TestContextBudgetStates(t *testing.T) {
	budget := NewContextBudget(1000)
	budget.mu.Lock()
	budget.currentUsage.ContextUsagePercent = 0.95
	budget.mu.Unlock()
	if budget.CheckBudget() != BudgetCritical {
		t.Fatal("expected critical budget state")
	}
	budget.mu.Lock()
	budget.currentUsage.ContextUsagePercent = 0.5
	budget.mu.Unlock()
	if budget.CheckBudget() != BudgetOK {
		t.Fatal("expected OK budget state")
	}
}
