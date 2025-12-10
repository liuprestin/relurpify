package framework

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedContextDowngradesOnBudgetWarning(t *testing.T) {
	ctx := NewContext()
	budget := NewContextBudget(256)
	summarizer := &SimpleSummarizer{}
	sc := NewSharedContext(ctx, budget, summarizer)

	path := filepath.Join(t.TempDir(), "file.go")
	content := strings.Repeat("func example() {}\n", 50)
	fc, err := sc.AddFile(path, content, "go", DetailFull)
	if err != nil {
		t.Fatalf("AddFile failed: %v", err)
	}
	if fc.Level != DetailFull {
		t.Fatalf("expected DetailFull, got %v", fc.Level)
	}

	// Simulate budget pressure.
	sc.OnBudgetWarning(0.9)
	if fc.Level != DetailSummary {
		t.Fatalf("expected file downgraded to summary, got %v", fc.Level)
	}
}

func TestSharedContextRefreshConversationSummary(t *testing.T) {
	ctx := NewContext()
	sc := NewSharedContext(ctx, nil, &SimpleSummarizer{})
	sc.AddInteraction("user", "Add new API endpoint", nil)
	sc.AddInteraction("assistant", "Implemented handler", nil)

	sc.RefreshConversationSummary()
	if sc.GetConversationSummary() == "" {
		t.Fatalf("expected conversation summary to be populated")
	}
}
