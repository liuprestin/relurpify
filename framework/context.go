// Package framework hosts the foundational data structures that every agent,
// tool, and orchestration primitive depends on. The comments in this file are
// intentionally verbose so that new contributors can treat it as a guided tour
// when they first inspect the runtime context mechanics.
package framework

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// Interaction captures a single turn of conversation or observation. Storing a
// timestamp and arbitrary metadata lets agents replay past reasoning, render
// transcripts, or build features like “explain how we got here” without needing
// to re-run the original tools/LLM calls.
type Interaction struct {
	Role      string                 `json:"role"`
	Content   string                 `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Context acts as the in-memory “blackboard” shared by nodes inside a graph.
// It separates information into three buckets:
//   - state: durable facts that should be visible to all downstream nodes
//   - variables: transient scratch data used by a single node/branch
//   - knowledge: derived/global insights cached for reuse
// The structure embeds a RWMutex because multiple goroutines (parallel graph
// branches) can touch it concurrently.
type Context struct {
	mu          sync.RWMutex
	state       map[string]interface{}
	variables   map[string]interface{}
	knowledge   map[string]interface{}
	history     []Interaction
	phase       string
	maxHistory  int
	maxSnapshot int
}

// NewContext builds an empty execution context with sensible history limits so
// runaway tool chatter does not balloon memory usage.
func NewContext() *Context {
	return &Context{
		state:       make(map[string]interface{}),
		variables:   make(map[string]interface{}),
		knowledge:   make(map[string]interface{}),
		history:     make([]Interaction, 0),
		phase:       "planning",
		maxHistory:  200,
		maxSnapshot: 32,
	}
}

// SetExecutionPhase stores the current execution phase.
func (c *Context) SetExecutionPhase(phase string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.phase = phase
}

// ExecutionPhase returns the current phase.
func (c *Context) ExecutionPhase() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.phase
}

// Get retrieves a value from the shared state.
func (c *Context) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.state[key]
	return v, ok
}

// Set stores a value in the shared state.
func (c *Context) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state[key] = value
}

// GetVariable returns a temporary variable.
func (c *Context) GetVariable(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.variables[key]
	return v, ok
}

// SetVariable stores a variable for scratch usage.
func (c *Context) SetVariable(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.variables[key] = value
}

// Merge copies another context into the current one. It is primarily used when
// parallel graph branches finish executing: each goroutine works on a clone and
// the winning data is merged back in the parent context to keep side effects
// deterministic.
func (c *Context) Merge(other *Context) {
	if other == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	for k, v := range other.state {
		c.state[k] = v
	}
	for k, v := range other.variables {
		c.variables[k] = v
	}
	for k, v := range other.knowledge {
		c.knowledge[k] = v
	}
	c.history = append(c.history, other.history...)
	c.smartTruncateHistoryLocked()
}

// Clone returns a deep copy of the context, enabling speculative work in
// separate goroutines. Gob encoding keeps the implementation compact while
// handling nested maps/slices without bespoke copy logic.
func (c *Context) Clone() *Context {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(c.state); err != nil {
		return NewContext()
	}
	if err := enc.Encode(c.variables); err != nil {
		return NewContext()
	}
	if err := enc.Encode(c.knowledge); err != nil {
		return NewContext()
	}
	if err := enc.Encode(c.history); err != nil {
		return NewContext()
	}

	dec := gob.NewDecoder(bytes.NewBuffer(buf.Bytes()))
	clone := NewContext()
	if err := dec.Decode(&clone.state); err != nil {
		return NewContext()
	}
	if err := dec.Decode(&clone.variables); err != nil {
		return NewContext()
	}
	if err := dec.Decode(&clone.knowledge); err != nil {
		return NewContext()
	}
	if err := dec.Decode(&clone.history); err != nil {
		return NewContext()
	}
	clone.phase = c.phase
	return clone
}

// ContextSnapshot is a serializable snapshot of Context.
type ContextSnapshot struct {
	State     map[string]interface{} `json:"state"`
	Variables map[string]interface{} `json:"variables"`
	Knowledge map[string]interface{} `json:"knowledge"`
	History   []Interaction          `json:"history"`
	Phase     string                 `json:"phase"`
}

// Snapshot captures the context for rollback.
func (c *Context) Snapshot() *ContextSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cp := func(src map[string]interface{}) map[string]interface{} {
		dst := make(map[string]interface{}, len(src))
		for k, v := range src {
			dst[k] = v
		}
		return dst
	}

	snapshot := &ContextSnapshot{
		State:     cp(c.state),
		Variables: cp(c.variables),
		Knowledge: cp(c.knowledge),
		History:   append([]Interaction(nil), c.history...),
		Phase:     c.phase,
	}
	return snapshot
}

// Restore puts the context back to a snapshot. The method intentionally
// overwrites every section instead of mutating in place to avoid sharing map
// references with stale snapshots.
func (c *Context) Restore(snapshot *ContextSnapshot) error {
	if snapshot == nil {
		return errors.New("nil snapshot")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = snapshot.State
	c.variables = snapshot.Variables
	c.knowledge = snapshot.Knowledge
	c.history = snapshot.History
	c.phase = snapshot.Phase
	c.smartTruncateHistoryLocked()
	return nil
}

// MarshalJSON ensures the context is serializable.
func (c *Context) MarshalJSON() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return json.Marshal(&ContextSnapshot{
		State:     c.state,
		Variables: c.variables,
		Knowledge: c.knowledge,
		History:   c.history,
		Phase:     c.phase,
	})
}

// UnmarshalJSON supports loading context from disk.
func (c *Context) UnmarshalJSON(data []byte) error {
	var snapshot ContextSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	return c.Restore(&snapshot)
}

// AddInteraction appends to the conversation history.
func (c *Context) AddInteraction(role, content string, metadata map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.history = append(c.history, Interaction{
		Role:      role,
		Content:   content,
		Timestamp: time.Now().UTC(),
		Metadata:  metadata,
	})
	c.smartTruncateHistoryLocked()
}

// History returns the accumulated conversation history.
func (c *Context) History() []Interaction {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]Interaction(nil), c.history...)
}

// smartTruncateHistoryLocked keeps the conversation history bounded while still
// preserving the very first message (usually the task instruction). The oldest
// middle portion is dropped so that downstream reasoning retains enough
// context without exhausting memory.
func (c *Context) smartTruncateHistoryLocked() {
	if len(c.history) <= c.maxHistory {
		return
	}
	start := len(c.history) - c.maxHistory
	c.history = append(c.history[:1], c.history[start:]...)
}

// SetKnowledge stores derived information available to all nodes.
func (c *Context) SetKnowledge(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.knowledge[key] = value
}

// GetKnowledge retrieves derived info.
func (c *Context) GetKnowledge(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.knowledge[key]
	return val, ok
}
