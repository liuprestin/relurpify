package persistence

import (
	"context"
	"testing"
)

// TestInMemoryVectorStore exercises the happy path for insert/query/delete to
// ensure the educational TF model behaves as expected.
func TestInMemoryVectorStore(t *testing.T) {
	store := NewInMemoryVectorStore()
	ctx := context.Background()

	docs := []Document{
		{ID: "1", Content: "function handles http requests"},
		{ID: "2", Content: "database transaction rollback"},
		{ID: "3", Content: "http server middleware logging"},
	}
	for _, doc := range docs {
		if err := store.Upsert(ctx, doc); err != nil {
			t.Fatalf("upsert doc %s: %v", doc.ID, err)
		}
	}

	results, err := store.Query(ctx, "http logging", 2)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected at least one result")
	}
	if results[0].Document.ID != "3" {
		t.Fatalf("expected doc 3 to rank first, got %+v", results[0])
	}

	if err := store.Delete(ctx, "1"); err != nil {
		t.Fatalf("delete error: %v", err)
	}
}
