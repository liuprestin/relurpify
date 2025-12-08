package framework

import (
	"testing"
	"time"
)

type fakeContextItem struct {
	tokens    int
	relevance float64
	priority  int
	age       time.Duration
}

func (f *fakeContextItem) TokenCount() int         { return f.tokens }
func (f *fakeContextItem) RelevanceScore() float64 { return f.relevance }
func (f *fakeContextItem) Priority() int           { return f.priority }
func (f *fakeContextItem) Compress() (ContextItem, error) {
	return &fakeContextItem{
		tokens:    f.tokens / 2,
		relevance: f.relevance * 0.8,
		priority:  f.priority + 1,
		age:       f.age,
	}, nil
}
func (f *fakeContextItem) Type() ContextItemType { return ContextTypeMemory }
func (f *fakeContextItem) Age() time.Duration    { return f.age }

func TestContextManagerAddItem(t *testing.T) {
	budget := NewContextBudget(8000)
	budget.SetReservations(500, 500, 500)
	manager := NewContextManager(budget)
	item := &fakeContextItem{tokens: 10, relevance: 1.0, priority: 1, age: time.Hour}
	if err := manager.AddItem(item); err != nil {
		t.Fatalf("AddItem returned error: %v", err)
	}
	if len(manager.GetItems()) != 1 {
		t.Fatal("expected item to be added")
	}
}

func TestContextManagerCompression(t *testing.T) {
	budget := NewContextBudget(1000)
	manager := NewContextManager(budget)
	manager.items = []ContextItem{
		&fakeContextItem{tokens: 100, relevance: 0.05, priority: 5, age: time.Hour},
	}
	budget.mu.Lock()
	budget.currentUsage.ContextUsagePercent = 0.85
	budget.mu.Unlock()
	if err := manager.MakeSpace(20); err != nil {
		t.Fatalf("expected MakeSpace to succeed via compression, got %v", err)
	}
}

func TestContextManagerPrune(t *testing.T) {
	budget := NewContextBudget(1000)
	manager := NewContextManager(budget)
	manager.items = []ContextItem{
		&fakeContextItem{tokens: 100, relevance: 0.0, priority: 5, age: 48 * time.Hour},
		&fakeContextItem{tokens: 80, relevance: 0.0, priority: 6, age: 24 * time.Hour},
	}
	budget.mu.Lock()
	budget.currentUsage.ContextUsagePercent = 0.95
	budget.mu.Unlock()
	if err := manager.MakeSpace(50); err != nil {
		t.Fatalf("expected pruning to succeed, got %v", err)
	}
	stats := manager.GetStats()
	if stats.TotalItems == 0 {
		t.Fatal("expected some items remaining after pruning")
	}
}
