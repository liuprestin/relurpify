package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// Position follows the LSP specification.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Location describes a file and range.
type Location struct {
	URI   string   `json:"uri"`
	Range [2]int64 `json:"range"`
}

// Diagnostic models an error returned by the LSP.
type Diagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
	Line     int    `json:"line"`
}

// SymbolInformation gives structure of a document.
type SymbolInformation struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Location string `json:"location"`
}

// LSPClient defines required operations for the language server proxy.
type LSPClient interface {
	GetDefinition(ctx context.Context, req DefinitionRequest) (DefinitionResult, error)
	GetReferences(ctx context.Context, req ReferencesRequest) ([]Location, error)
	GetHover(ctx context.Context, req HoverRequest) (HoverResult, error)
	GetDiagnostics(ctx context.Context, file string) ([]Diagnostic, error)
	SearchSymbols(ctx context.Context, query string) ([]SymbolInformation, error)
	GetDocumentSymbols(ctx context.Context, file string) ([]SymbolInformation, error)
	Format(ctx context.Context, req FormatRequest) (string, error)
}

// DefinitionRequest describes getDefinition arguments.
type DefinitionRequest struct {
	File     string
	Symbol   string
	Position Position
}

// DefinitionResult holds the response.
type DefinitionResult struct {
	Location  Location `json:"location"`
	Snippet   string   `json:"snippet"`
	Signature string   `json:"signature"`
}

// ReferencesRequest describes references query.
type ReferencesRequest struct {
	File     string
	Symbol   string
	Position Position
}

// HoverRequest describes hover query.
type HoverRequest struct {
	File     string
	Position Position
}

// HoverResult holds docs/type info.
type HoverResult struct {
	TypeInfo string `json:"typeInfo"`
	Docs     string `json:"docs"`
}

// FormatRequest describes format tool.
type FormatRequest struct {
	File string
	Code string
}

// Proxy manages multiple LSP clients.
type Proxy struct {
	mu      sync.RWMutex
	clients map[string]LSPClient
	cache   map[string]cacheEntry
	ttl     time.Duration
}

type cacheEntry struct {
	value      interface{}
	expiration time.Time
}

// NewProxy creates a proxy instance.
func NewProxy(ttl time.Duration) *Proxy {
	if ttl == 0 {
		ttl = time.Minute
	}
	return &Proxy{
		clients: make(map[string]LSPClient),
		cache:   make(map[string]cacheEntry),
		ttl:     ttl,
	}
}

// Register registers a client for a language key.
func (p *Proxy) Register(language string, client LSPClient) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients[language] = client
}

func (p *Proxy) clientForFile(file string) (LSPClient, error) {
	ext := strings.TrimPrefix(filepath.Ext(file), ".")
	p.mu.RLock()
	defer p.mu.RUnlock()
	client, ok := p.clients[ext]
	if !ok {
		return nil, fmt.Errorf("no LSP client for extension %s", ext)
	}
	return client, nil
}

func (p *Proxy) cached(key string, fetch func() (interface{}, error)) (interface{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.cache[key]; ok && time.Now().Before(entry.expiration) {
		return entry.value, nil
	}
	val, err := fetch()
	if err != nil {
		return nil, err
	}
	p.cache[key] = cacheEntry{value: val, expiration: time.Now().Add(p.ttl)}
	return val, nil
}

// DefinitionTool implements the GetDefinition tool.
type DefinitionTool struct {
	Proxy *Proxy
	manager *framework.PermissionManager
	agentID string
}

func (t *DefinitionTool) SetPermissionManager(manager *framework.PermissionManager, agentID string) {
	t.manager = manager
	t.agentID = agentID
}

// Name implements Tool.
func (t *DefinitionTool) Name() string { return "lsp_get_definition" }

// Description implements Tool.
func (t *DefinitionTool) Description() string {
	return "Finds the definition for a symbol."
}

// Category implements Tool.
func (t *DefinitionTool) Category() string { return "lsp" }

// Parameters implements Tool.
func (t *DefinitionTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "file", Type: "string", Description: "File path", Required: true},
		{Name: "symbol", Type: "string", Description: "Symbol name", Required: true},
		{Name: "line", Type: "int", Description: "Line number", Required: true},
		{Name: "character", Type: "int", Description: "Character offset", Required: true},
	}
}

// Execute implements Tool.
func (t *DefinitionTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	file := fmt.Sprint(args["file"])
	if t.manager != nil {
		if err := t.manager.CheckFileAccess(ctx, t.agentID, framework.FileSystemRead, file); err != nil {
			return nil, err
		}
	}
	client, err := t.Proxy.clientForFile(file)
	if err != nil {
		return nil, err
	}
	req := DefinitionRequest{
		File:   file,
		Symbol: fmt.Sprint(args["symbol"]),
		Position: Position{
			Line:      toInt(args["line"]),
			Character: toInt(args["character"]),
		},
	}
	cacheKey := fmt.Sprintf("def:%s:%s:%d:%d", req.File, req.Symbol, req.Position.Line, req.Position.Character)
	resAny, err := t.Proxy.cached(cacheKey, func() (interface{}, error) {
		return client.GetDefinition(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	res := resAny.(DefinitionResult)
	return &framework.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"location":  res.Location,
			"snippet":   res.Snippet,
			"signature": res.Signature,
		},
	}, nil
}

// IsAvailable implements Tool.
func (t *DefinitionTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return t.Proxy != nil
}

func (t *DefinitionTool) Permissions() framework.ToolPermissions {
	return framework.ToolPermissions{Permissions: framework.NewFileSystemPermissionSet("", framework.FileSystemRead, framework.FileSystemList)}
}

// ReferencesTool implements GetReferences tool.
type ReferencesTool struct {
	Proxy *Proxy
	manager *framework.PermissionManager
	agentID string
}

func (t *ReferencesTool) SetPermissionManager(manager *framework.PermissionManager, agentID string) {
	t.manager = manager
	t.agentID = agentID
}

func (t *ReferencesTool) Name() string { return "lsp_get_references" }
func (t *ReferencesTool) Description() string {
	return "Lists references for a symbol."
}
func (t *ReferencesTool) Category() string { return "lsp" }
func (t *ReferencesTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "file", Type: "string", Description: "File path", Required: true},
		{Name: "symbol", Type: "string", Description: "Symbol name", Required: true},
		{Name: "line", Type: "int", Description: "Line number", Required: true},
		{Name: "character", Type: "int", Description: "Character offset", Required: true},
	}
}
func (t *ReferencesTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	file := fmt.Sprint(args["file"])
	if t.manager != nil {
		if err := t.manager.CheckFileAccess(ctx, t.agentID, framework.FileSystemRead, file); err != nil {
			return nil, err
		}
	}
	client, err := t.Proxy.clientForFile(file)
	if err != nil {
		return nil, err
	}
	req := ReferencesRequest{
		File:   file,
		Symbol: fmt.Sprint(args["symbol"]),
		Position: Position{
			Line:      toInt(args["line"]),
			Character: toInt(args["character"]),
		},
	}
	resAny, err := t.Proxy.cached("refs:"+req.File+":"+req.Symbol, func() (interface{}, error) {
		return client.GetReferences(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	res := resAny.([]Location)
	return &framework.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"locations": res,
		},
	}, nil
}
func (t *ReferencesTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return t.Proxy != nil
}

func (t *ReferencesTool) Permissions() framework.ToolPermissions {
	return framework.ToolPermissions{Permissions: framework.NewFileSystemPermissionSet("", framework.FileSystemRead, framework.FileSystemList)}
}

// HoverTool implements GetHover.
type HoverTool struct {
	Proxy *Proxy
	manager *framework.PermissionManager
	agentID string
}

func (t *HoverTool) SetPermissionManager(manager *framework.PermissionManager, agentID string) {
	t.manager = manager
	t.agentID = agentID
}

func (t *HoverTool) Name() string { return "lsp_get_hover" }
func (t *HoverTool) Description() string {
	return "Retrieves type information for a position."
}
func (t *HoverTool) Category() string { return "lsp" }
func (t *HoverTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "file", Type: "string", Required: true},
		{Name: "line", Type: "int", Required: true},
		{Name: "character", Type: "int", Required: true},
	}
}
func (t *HoverTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	file := fmt.Sprint(args["file"])
	if t.manager != nil {
		if err := t.manager.CheckFileAccess(ctx, t.agentID, framework.FileSystemRead, file); err != nil {
			return nil, err
		}
	}
	client, err := t.Proxy.clientForFile(file)
	if err != nil {
		return nil, err
	}
	req := HoverRequest{
		File: file,
		Position: Position{
			Line:      toInt(args["line"]),
			Character: toInt(args["character"]),
		},
	}
	resAny, err := t.Proxy.cached("hover:"+req.File, func() (interface{}, error) {
		return client.GetHover(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	res := resAny.(HoverResult)
	return &framework.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"type": res.TypeInfo,
			"docs": res.Docs,
		},
	}, nil
}
func (t *HoverTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return t.Proxy != nil
}

func (t *HoverTool) Permissions() framework.ToolPermissions {
	return framework.ToolPermissions{Permissions: framework.NewFileSystemPermissionSet("", framework.FileSystemRead, framework.FileSystemList)}
}

// DiagnosticsTool implements diagnostics retrieval.
type DiagnosticsTool struct {
	Proxy *Proxy
	manager *framework.PermissionManager
	agentID string
}

func (t *DiagnosticsTool) SetPermissionManager(manager *framework.PermissionManager, agentID string) {
	t.manager = manager
	t.agentID = agentID
}

func (t *DiagnosticsTool) Name() string { return "lsp_get_diagnostics" }
func (t *DiagnosticsTool) Description() string {
	return "Retrieves diagnostics for a file."
}
func (t *DiagnosticsTool) Category() string { return "lsp" }
func (t *DiagnosticsTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{{Name: "file", Type: "string", Required: true}}
}
func (t *DiagnosticsTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	file := fmt.Sprint(args["file"])
	if t.manager != nil {
		if err := t.manager.CheckFileAccess(ctx, t.agentID, framework.FileSystemRead, file); err != nil {
			return nil, err
		}
	}
	client, err := t.Proxy.clientForFile(file)
	if err != nil {
		return nil, err
	}
	resAny, err := t.Proxy.cached("diag:"+file, func() (interface{}, error) {
		return client.GetDiagnostics(ctx, file)
	})
	if err != nil {
		return nil, err
	}
	res := resAny.([]Diagnostic)
	return &framework.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"diagnostics": res,
		},
	}, nil
}
func (t *DiagnosticsTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return t.Proxy != nil
}

func (t *DiagnosticsTool) Permissions() framework.ToolPermissions {
	return framework.ToolPermissions{Permissions: framework.NewFileSystemPermissionSet("", framework.FileSystemRead, framework.FileSystemList)}
}

// SearchSymbolsTool implements symbol lookup.
type SearchSymbolsTool struct {
	Proxy *Proxy
	manager *framework.PermissionManager
	agentID string
}

func (t *SearchSymbolsTool) SetPermissionManager(manager *framework.PermissionManager, agentID string) {
	t.manager = manager
	t.agentID = agentID
}

func (t *SearchSymbolsTool) Name() string { return "lsp_search_symbols" }
func (t *SearchSymbolsTool) Description() string {
	return "Searches workspace symbols."
}
func (t *SearchSymbolsTool) Category() string { return "lsp" }
func (t *SearchSymbolsTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{{Name: "query", Type: "string", Required: true}}
}
func (t *SearchSymbolsTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	query := fmt.Sprint(args["query"])
	resAny, err := t.Proxy.cached("symbols:"+query, func() (interface{}, error) {
		t.Proxy.mu.RLock()
		defer t.Proxy.mu.RUnlock()
		var combined []SymbolInformation
		for _, client := range t.Proxy.clients {
			items, err := client.SearchSymbols(ctx, query)
			if err != nil {
				return nil, err
			}
			combined = append(combined, items...)
		}
		return combined, nil
	})
	if err != nil {
		return nil, err
	}
	res := resAny.([]SymbolInformation)
	return &framework.ToolResult{Success: true, Data: map[string]interface{}{"symbols": res}}, nil
}
func (t *SearchSymbolsTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return t.Proxy != nil
}

func (t *SearchSymbolsTool) Permissions() framework.ToolPermissions {
	return framework.ToolPermissions{Permissions: framework.NewFileSystemPermissionSet("", framework.FileSystemRead, framework.FileSystemList)}
}

// DocumentSymbolsTool returns structure of a file.
type DocumentSymbolsTool struct {
	Proxy *Proxy
	manager *framework.PermissionManager
	agentID string
}

func (t *DocumentSymbolsTool) SetPermissionManager(manager *framework.PermissionManager, agentID string) {
	t.manager = manager
	t.agentID = agentID
}

func (t *DocumentSymbolsTool) Name() string { return "lsp_document_symbols" }
func (t *DocumentSymbolsTool) Description() string {
	return "Lists symbols in a document."
}
func (t *DocumentSymbolsTool) Category() string { return "lsp" }
func (t *DocumentSymbolsTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{{Name: "file", Type: "string", Required: true}}
}
func (t *DocumentSymbolsTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	file := fmt.Sprint(args["file"])
	if t.manager != nil {
		if err := t.manager.CheckFileAccess(ctx, t.agentID, framework.FileSystemRead, file); err != nil {
			return nil, err
		}
	}
	client, err := t.Proxy.clientForFile(file)
	if err != nil {
		return nil, err
	}
	resAny, err := t.Proxy.cached("doc_symbols:"+file, func() (interface{}, error) {
		return client.GetDocumentSymbols(ctx, file)
	})
	if err != nil {
		return nil, err
	}
	res := resAny.([]SymbolInformation)
	return &framework.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"symbols": res,
		},
	}, nil
}
func (t *DocumentSymbolsTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return t.Proxy != nil
}

func (t *DocumentSymbolsTool) Permissions() framework.ToolPermissions {
	return framework.ToolPermissions{Permissions: framework.NewFileSystemPermissionSet("", framework.FileSystemRead, framework.FileSystemList)}
}

// FormatTool formats code through the LSP.
type FormatTool struct {
	Proxy *Proxy
	manager *framework.PermissionManager
	agentID string
}

func (t *FormatTool) SetPermissionManager(manager *framework.PermissionManager, agentID string) {
	t.manager = manager
	t.agentID = agentID
}

func (t *FormatTool) Name() string        { return "lsp_format" }
func (t *FormatTool) Description() string { return "Formats code using the language server." }
func (t *FormatTool) Category() string    { return "lsp" }
func (t *FormatTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "file", Type: "string", Required: true},
		{Name: "code", Type: "string", Required: true},
	}
}
func (t *FormatTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	file := fmt.Sprint(args["file"])
	if t.manager != nil {
		if err := t.manager.CheckFileAccess(ctx, t.agentID, framework.FileSystemRead, file); err != nil {
			return nil, err
		}
	}
	client, err := t.Proxy.clientForFile(file)
	if err != nil {
		return nil, err
	}
	formatted, err := client.Format(ctx, FormatRequest{
		File: file,
		Code: fmt.Sprint(args["code"]),
	})
	if err != nil {
		return nil, err
	}
	return &framework.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"code": formatted,
		},
	}, nil
}
func (t *FormatTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return t.Proxy != nil
}

func (t *FormatTool) Permissions() framework.ToolPermissions {
	return framework.ToolPermissions{Permissions: framework.NewFileSystemPermissionSet("", framework.FileSystemRead, framework.FileSystemWrite)}
}

func toInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}
