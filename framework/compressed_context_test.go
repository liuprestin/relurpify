package framework

import (
	"testing"
	"time"
)

func TestSimpleCompressionStrategyCompress(t *testing.T) {
	strategy := NewSimpleCompressionStrategy()
	llm := &stubLLM{text: `Summary: Completed refactor.
Key Facts: [{"type":"decision","content":"Refactored module","relevance":0.9}]`}
	interactions := []Interaction{
		{ID: 1, Role: "user", Content: "Please refactor the module", Timestamp: time.Now()},
		{ID: 2, Role: "assistant", Content: "Working on it", Timestamp: time.Now()},
	}
	cc, err := strategy.Compress(interactions, llm)
	if err != nil {
		t.Fatalf("compress returned error: %v", err)
	}
	if cc == nil {
		t.Fatal("expected compressed context")
	}
	if cc.Summary == "" {
		t.Fatal("expected summary to be populated")
	}
	if len(cc.KeyFacts) == 0 {
		t.Fatal("expected key facts to be extracted")
	}
	if cc.OriginalTokens <= cc.CompressedTokens {
		t.Fatalf("expected compression to reduce tokens, got original=%d compressed=%d", cc.OriginalTokens, cc.CompressedTokens)
	}
}

func TestSimpleCompressionStrategyShouldCompress(t *testing.T) {
	strategy := NewSimpleCompressionStrategy()
	ctx := NewContext()
	for i := 0; i < 12; i++ {
		ctx.AddInteraction("user", "message", nil)
	}
	if !strategy.ShouldCompress(ctx, nil) {
		t.Fatal("expected compression recommendation when history exceeds threshold")
	}
	budget := NewContextBudget(1000)
	budget.mu.Lock()
	budget.currentUsage.ContextUsagePercent = 0.5
	budget.mu.Unlock()
	if strategy.ShouldCompress(ctx, budget) {
		t.Fatal("expected compression to stay disabled when usage below threshold")
	}
	budget.mu.Lock()
	budget.currentUsage.ContextUsagePercent = 0.9
	budget.mu.Unlock()
	if !strategy.ShouldCompress(ctx, budget) {
		t.Fatal("expected compression once usage exceeds threshold")
	}
}

func TestContextCompressHistory(t *testing.T) {
	ctx := NewContext()
	for i := 0; i < 15; i++ {
		ctx.AddInteraction("user", "long message content", nil)
	}
	llm := &stubLLM{text: `Summary: summary
Key Facts: [{"type":"decision","content":"fact","relevance":0.8}]`}
	strategy := NewSimpleCompressionStrategy()
	err := ctx.CompressHistory(strategy.KeepRecentCount, llm, strategy)
	if err != nil {
		t.Fatalf("CompressHistory returned error: %v", err)
	}
	stats := ctx.GetCompressionStats()
	if stats.CompressionEvents == 0 {
		t.Fatal("expected compression event to be logged")
	}
	if len(ctx.compressedHistory) == 0 {
		t.Fatal("expected compressed history entries")
	}
	if len(ctx.history) != strategy.KeepRecentCount {
		t.Fatalf("expected recent history length %d, got %d", strategy.KeepRecentCount, len(ctx.history))
	}
}
