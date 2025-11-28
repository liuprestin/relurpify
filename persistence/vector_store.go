package persistence

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
)

// Document represents semantic information stored for later retrieval.
type Document struct {
	ID         string
	WorkflowID string
	Content    string
	Metadata   map[string]interface{}
}

// SearchResult contains similarity info.
type SearchResult struct {
	Document Document
	Score    float64
}

// VectorStore provides semantic recall by text similarity.
type VectorStore interface {
	Upsert(ctx context.Context, doc Document) error
	Query(ctx context.Context, query string, limit int) ([]SearchResult, error)
	Delete(ctx context.Context, id string) error
}

// InMemoryVectorStore implements a naive TF-based similarity store.
type InMemoryVectorStore struct {
	mu   sync.RWMutex
	data map[string]Document
	vecs map[string]map[string]float64
}

// NewInMemoryVectorStore returns a ready-to-use store.
func NewInMemoryVectorStore() *InMemoryVectorStore {
	return &InMemoryVectorStore{
		data: make(map[string]Document),
		vecs: make(map[string]map[string]float64),
	}
}

// Upsert encodes and stores a document.
func (s *InMemoryVectorStore) Upsert(ctx context.Context, doc Document) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if doc.ID == "" {
		return errors.New("document id required")
	}
	vector := embed(doc.Content)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[doc.ID] = doc
	s.vecs[doc.ID] = vector
	return nil
}

// Query searches the store using cosine similarity.
func (s *InMemoryVectorStore) Query(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if limit <= 0 {
		limit = 5
	}
	qVec := embed(query)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []SearchResult
	for id, vec := range s.vecs {
		score := cosineSimilarity(qVec, vec)
		if score == 0 {
			continue
		}
		results = append(results, SearchResult{
			Document: s.data[id],
			Score:    score,
		})
	}
	if len(results) == 0 {
		return nil, nil
	}
	sortResults(results)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// Delete removes a document by id.
func (s *InMemoryVectorStore) Delete(ctx context.Context, id string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	delete(s.vecs, id)
	return nil
}

// embed tokenizes text and builds a simple term-frequency vector. The store
// intentionally keeps the math approachable so teams can swap in a real model.
func embed(text string) map[string]float64 {
	vector := make(map[string]float64)
	for _, token := range strings.Fields(strings.ToLower(text)) {
		vector[token]++
	}
	return vector
}

// cosineSimilarity measures the angle between vectors, returning higher scores
// for documents that share more vocabulary with the query.
func cosineSimilarity(a, b map[string]float64) float64 {
	var dot, normA, normB float64
	for term, weight := range a {
		dot += weight * b[term]
		normA += weight * weight
	}
	for _, weight := range b {
		normB += weight * weight
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// sortResults orders matches by descending similarity using a simple swap sort
// (datasets are intentionally tiny for the in-memory implementation).
func sortResults(results []SearchResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}
