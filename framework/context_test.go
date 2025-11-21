package framework

import "testing"

func TestContextSnapshotRestore(t *testing.T) {
	ctx := NewContext()
	ctx.Set("task.id", "123")
	ctx.SetVariable("cursor", 42)
	ctx.SetKnowledge("analysis", "done")
	ctx.AddInteraction("user", "hello", nil)

	snap := ctx.Snapshot()
	ctx.Set("task.id", "456")
	ctx.SetVariable("cursor", 0)

	if err := ctx.Restore(snap); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	if val, _ := ctx.Get("task.id"); val != "123" {
		t.Fatalf("expected task.id=123, got %v", val)
	}
	if val, _ := ctx.GetVariable("cursor"); val != 42 {
		t.Fatalf("expected cursor=42, got %v", val)
	}
	if len(ctx.History()) != 1 {
		t.Fatalf("expected history size 1, got %d", len(ctx.History()))
	}
}
