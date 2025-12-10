package framework

import (
	"context"
	"fmt"
	"strings"
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
	Permissions() ToolPermissions
}

// ToolParameter describes an argument the tool accepts.
type ToolParameter struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Default     interface{}
}

// PermissionAware allows tools to receive the permission manager for fine-grained
// runtime checks (e.g. verifying file paths against allowlists).
type PermissionAware interface {
	SetPermissionManager(manager *PermissionManager, agentID string)
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
	mu                sync.RWMutex
	tools             map[string]Tool
	permissionManager *PermissionManager
	registeredAgentID string
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
	// If we already have a manager, inject it immediately
	if r.permissionManager != nil {
		if aware, ok := tool.(PermissionAware); ok {
			aware.SetPermissionManager(r.permissionManager, r.registeredAgentID)
		}
	}
	r.tools[tool.Name()] = r.wrapTool(tool)
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

// UsePermissionManager enables default-deny enforcement for all tools.
func (r *ToolRegistry) UsePermissionManager(agentID string, manager *PermissionManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permissionManager = manager
	r.registeredAgentID = agentID
	for name, tool := range r.tools {
		// Unwrap secureTool to get the inner tool if needed, 
		// but here we just check the interface on the stored tool 
		// (which might be secureTool, so we need to be careful).
		// wrapTool handles wrapping, but we need to inject into the *inner* tool.
		// Since r.tools stores the *wrapped* tool after Register/UsePermissionManager,
		// we need to access the underlying tool if it's already wrapped.
		
		var inner Tool = tool
		if secure, ok := tool.(*secureTool); ok {
			inner = secure.Tool
		}
		
		if aware, ok := inner.(PermissionAware); ok {
			aware.SetPermissionManager(manager, agentID)
		}
		
		// If it wasn't wrapped yet, wrap it. If it was, wrapTool handles re-wrapping (updating fields).
		r.tools[name] = r.wrapTool(inner)
	}
}

// RestrictTo removes tools not present in the allowed set.
func (r *ToolRegistry) RestrictTo(allowed []string) {
	if len(allowed) == 0 {
		return
	}
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name := range r.tools {
		if _, ok := set[name]; !ok {
			delete(r.tools, name)
		}
	}
}

// wrapTool decorates a tool with the secure wrapper when a permission manager
// is active so every execution path consistently enforces authorization.
func (r *ToolRegistry) wrapTool(tool Tool) Tool {
	if tool == nil {
		return nil
	}
	if existing, ok := tool.(*secureTool); ok {
		existing.manager = r.permissionManager
		existing.agentID = r.registeredAgentID
		return existing
	}
	if r.permissionManager == nil {
		return tool
	}
	return &secureTool{
		Tool:    tool,
		manager: r.permissionManager,
		agentID: r.registeredAgentID,
	}
}

type secureTool struct {
	Tool
	manager *PermissionManager
	agentID string
}

// Execute authorizes the wrapped tool before delegating to the original
// implementation to ensure permission checks happen even for direct callers.
func (t *secureTool) Execute(ctx context.Context, state *Context, args map[string]interface{}) (*ToolResult, error) {
	if t.manager != nil {
		if err := t.manager.AuthorizeTool(ctx, t.agentID, t.Tool, args); err != nil {
			return nil, err
		}
	}
	return t.Tool.Execute(ctx, state, args)
}
