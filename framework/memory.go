package framework

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MemoryScope determines where data is persisted.
type MemoryScope string

const (
	MemoryScopeSession MemoryScope = "session"
	MemoryScopeProject MemoryScope = "project"
	MemoryScopeGlobal  MemoryScope = "global"
)

// MemoryRecord represents a stored memory item. Value is intentionally
// unstructured JSON so agents can stash anything from LLM responses to plan
// summaries without evolving the schema.
type MemoryRecord struct {
	Key       string                 `json:"key"`
	Value     map[string]interface{} `json:"value"`
	Scope     MemoryScope            `json:"scope"`
	Timestamp time.Time              `json:"timestamp"`
	Tags      []string               `json:"tags,omitempty"`
}

// MemoryStore describes the memory system operations.
type MemoryStore interface {
	Remember(ctx context.Context, key string, value map[string]interface{}, scope MemoryScope) error
	Recall(ctx context.Context, key string, scope MemoryScope) (*MemoryRecord, bool, error)
	Search(ctx context.Context, query string, scope MemoryScope) ([]MemoryRecord, error)
	Forget(ctx context.Context, key string, scope MemoryScope) error
	Summarize(ctx context.Context, scope MemoryScope) (string, error)
}

// HybridMemory combines in-memory caching with JSON persistence on disk. The
// design keeps session data transient (great for experiments) while persisting
// project/global scopes across runs for longer-term recall.
type HybridMemory struct {
	mu       sync.RWMutex
	cache    map[MemoryScope]map[string]MemoryRecord
	basePath string
}

// NewHybridMemory creates a new memory store.
func NewHybridMemory(basePath string) (*HybridMemory, error) {
	if basePath == "" {
		basePath = ".memory"
	}
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return nil, err
	}
	store := &HybridMemory{
		cache: map[MemoryScope]map[string]MemoryRecord{
			MemoryScopeSession: {},
			MemoryScopeProject: {},
			MemoryScopeGlobal:  {},
		},
		basePath: basePath,
	}
	if err := store.loadFromDisk(); err != nil {
		return nil, err
	}
	return store, nil
}

func (m *HybridMemory) loadFromDisk() error {
	for scope := range m.cache {
		path := m.scopePath(scope)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		var records []MemoryRecord
		if err := json.Unmarshal(data, &records); err != nil {
			return err
		}
		for _, r := range records {
			m.cache[scope][r.Key] = r
		}
	}
	return nil
}

func (m *HybridMemory) persist(scope MemoryScope) error {
	records := make([]MemoryRecord, 0, len(m.cache[scope]))
	for _, r := range m.cache[scope] {
		records = append(records, r)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.scopePath(scope), data, 0o644)
}

func (m *HybridMemory) scopePath(scope MemoryScope) string {
	filename := string(scope) + ".json"
	return filepath.Join(m.basePath, filename)
}

// Remember stores data for a given scope. Session-scoped memories stay in RAM
// to avoid excessive disk churn during fast agent loops, while project/global
// scopes are flushed to JSON for durability.
func (m *HybridMemory) Remember(ctx context.Context, key string, value map[string]interface{}, scope MemoryScope) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record := MemoryRecord{
		Key:       key,
		Value:     value,
		Scope:     scope,
		Timestamp: time.Now().UTC(),
	}
	m.cache[scope][key] = record
	if scope == MemoryScopeSession {
		return nil
	}
	return m.persist(scope)
}

// Recall retrieves a memory record.
func (m *HybridMemory) Recall(ctx context.Context, key string, scope MemoryScope) (*MemoryRecord, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.cache[scope][key]
	if !ok {
		return nil, false, nil
	}
	return &record, true, nil
}

// Search executes a naive semantic search by substring match. It is purposely
// simple so that the memory subsystem feels deterministic and debuggable; you
// can later replace it with a vector store without touching agent code.
func (m *HybridMemory) Search(ctx context.Context, query string, scope MemoryScope) ([]MemoryRecord, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	lower := strings.ToLower(query)
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []MemoryRecord
	for _, record := range m.cache[scope] {
		data, _ := json.Marshal(record.Value)
		if strings.Contains(strings.ToLower(string(data)), lower) {
			results = append(results, record)
		}
	}
	return results, nil
}

// Forget removes a stored memory entry.
func (m *HybridMemory) Forget(ctx context.Context, key string, scope MemoryScope) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cache[scope], key)
	if scope == MemoryScopeSession {
		return nil
	}
	return m.persist(scope)
}

// Summarize compresses older records into a textual summary. Teams often call
// this before persisting workflows so they can log “what just happened” without
// storing entire transcripts.
func (m *HybridMemory) Summarize(ctx context.Context, scope MemoryScope) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var builder strings.Builder
	builder.WriteString("Summary for scope ")
	builder.WriteString(string(scope))
	builder.WriteString(":\n")
	for _, record := range m.cache[scope] {
		builder.WriteString("- ")
		builder.WriteString(record.Key)
		builder.WriteString(": ")
		data, _ := json.Marshal(record.Value)
		builder.Write(data)
		builder.WriteRune('\n')
	}
	return builder.String(), nil
}
