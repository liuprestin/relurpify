package framework

import (
	"context"
	"fmt"
	"sync"
)

// Tool defines capabilities accessible to agents. Each implementation can wrap
// anything from a filesystem helper to an LSP proxy. The metadata doubles as a
// schema that LLMs can reason about when deciding which tool to call.
type Tool interface {
	Name() string
	Description() string
	Category() string
	Parameters() []ToolParameter
	Execute(ctx context.Context, state *Context, args map[string]interface{}) (*ToolResult, error)
	IsAvailable(ctx context.Context, state *Context) bool
}

// ToolParameter describes an argument the tool accepts.
type ToolParameter struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Default     interface{}
}

// ToolResult is returned by every tool execution.
type ToolResult struct {
	Success  bool
	Data     map[string]interface{}
	Error    string
	Metadata map[string]interface{}
}

// ToolRegistry maintains tools and ensures metadata lookups are fast. Agents
// typically keep a shared registry instance so dynamic planners can discover
// the available affordances at runtime.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry builds a registry instance.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(tool Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[tool.Name()]; exists {
		return fmt.Errorf("tool %s already registered", tool.Name())
	}
	r.tools[tool.Name()] = tool
	return nil
}

// Get fetches a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// All returns all registered tools.
func (r *ToolRegistry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		res = append(res, t)
	}
	return res
}
