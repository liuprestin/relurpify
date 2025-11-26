package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/lexcodex/relurpify/agents"
	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/tools"
)

// LSPServer implements a minimal custom LSP handler with AI extensions.
type LSPServer struct {
	Config        *framework.Config
	Agent         framework.Agent
	Context       *framework.Context
	Proxy         *tools.Proxy
	mu            sync.RWMutex
	openDocuments map[string]*Document
	logger        *log.Logger
}

// Document tracks open files from the editor.
type Document struct {
	URI        string
	LanguageID string
	Version    int
	Text       string
}

// InitializeParams partial struct.
type InitializeParams struct {
	RootURI string `json:"rootUri"`
	Client  string `json:"clientInfo"`
}

// InitializeResult partial struct.
type InitializeResult struct {
	Capabilities map[string]interface{} `json:"capabilities"`
}

// AICompleteParams describes AI commands.
type AICompleteParams struct {
	URI         string            `json:"uri"`
	Instruction string            `json:"instruction"`
	Range       [2]int            `json:"range"`
	Context     map[string]string `json:"context"`
}

// AIResult wraps the response back to editor.
type AIResult struct {
	Edits    []TextEdit             `json:"edits"`
	Metadata map[string]interface{} `json:"metadata"`
}

// TextEdit describes LSP text edit.
type TextEdit struct {
	Range   [2]int `json:"range"`
	NewText string `json:"newText"`
}

// NewLSPServer builds a server instance.
func NewLSPServer(config *framework.Config, agent framework.Agent, proxy *tools.Proxy, logger *log.Logger) *LSPServer {
	if logger == nil {
		logger = log.Default()
	}
	return &LSPServer{
		Config:        config,
		Agent:         agent,
		Context:       framework.NewContext(),
		Proxy:         proxy,
		openDocuments: make(map[string]*Document),
		logger:        logger,
	}
}

// Initialize handles the LSP initialize request.
func (s *LSPServer) Initialize(params InitializeParams) (*InitializeResult, error) {
	s.logger.Printf("LSP initialize from %s", params.Client)
	result := &InitializeResult{
		Capabilities: map[string]interface{}{
			"textDocumentSync": 2,
			"completionProvider": map[string]interface{}{
				"triggerCharacters": []string{"."},
			},
			"executeCommandProvider": map[string]interface{}{
				"commands": []string{
					"ai.complete",
					"ai.explain",
					"ai.refactor",
					"ai.fix",
					"ai.test",
					"ai.document",
				},
			},
		},
	}
	return result, nil
}

// TextDocumentDidOpen stores document state.
func (s *LSPServer) TextDocumentDidOpen(uri, languageID string, version int, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openDocuments[uri] = &Document{
		URI:        uri,
		LanguageID: languageID,
		Version:    version,
		Text:       text,
	}
	return nil
}

// TextDocumentDidChange updates document text.
func (s *LSPServer) TextDocumentDidChange(uri string, version int, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, ok := s.openDocuments[uri]
	if !ok {
		return fmt.Errorf("document %s not tracked", uri)
	}
	doc.Text = text
	doc.Version = version
	return nil
}

// AIComplete executes AI pipelines (complete/explain/refactor).
func (s *LSPServer) AIComplete(ctx context.Context, params AICompleteParams) (*AIResult, error) {
	s.logger.Printf("AI request %s", params.Instruction)
	state := s.Context.Clone()
	state.Set("active.uri", params.URI)
	state.Set("active.range", params.Range)
	for k, v := range params.Context {
		state.Set("context."+k, v)
	}
	lang := s.documentLanguage(params.URI)
	ctxMap := map[string]any{
		"uri":     params.URI,
		"files":   []string{params.URI},
		"range":   params.Range,
		"content": s.documentText(params.URI),
	}
	if lang != "" {
		ctxMap["language"] = lang
	}
	task := &framework.Task{
		ID:          fmt.Sprintf("ai-%d", time.Now().UnixNano()),
		Type:        framework.TaskTypeCodeModification,
		Instruction: params.Instruction,
		Context:     ctxMap,
	}
	result, err := s.Agent.Execute(ctx, task, state)
	if err != nil {
		return nil, err
	}
	s.Context.Merge(state)
	edits := s.buildTextEdits(result, params)
	return &AIResult{
		Edits: edits,
		Metadata: map[string]interface{}{
			"node": result.NodeID,
		},
	}, nil
}

// AIExplain reuses AIComplete with explain instruction.
func (s *LSPServer) AIExplain(ctx context.Context, params AICompleteParams) (*AIResult, error) {
	params.Instruction = "Explain the selected code."
	return s.AIComplete(ctx, params)
}

// AIRefactor modifies instruction.
func (s *LSPServer) AIRefactor(ctx context.Context, params AICompleteParams) (*AIResult, error) {
	params.Instruction = "Refactor the selected code."
	return s.AIComplete(ctx, params)
}

func (s *LSPServer) documentText(uri string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if doc, ok := s.openDocuments[uri]; ok {
		return doc.Text
	}
	return ""
}

func (s *LSPServer) documentLanguage(uri string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if doc, ok := s.openDocuments[uri]; ok {
		return doc.LanguageID
	}
	return ""
}

func (s *LSPServer) buildTextEdits(result *framework.Result, params AICompleteParams) []TextEdit {
	if result == nil || result.Data == nil {
		return nil
	}
	var edits []TextEdit
	if text, ok := result.Data["text"].(string); ok && text != "" {
		edits = append(edits, TextEdit{
			Range:   params.Range,
			NewText: text,
		})
	} else if changes, ok := result.Data["edits"].([]TextEdit); ok {
		edits = append(edits, changes...)
	}
	return edits
}

// ContextBuilder gathers data from underlying language servers.
type ContextBuilder struct {
	Proxy *tools.Proxy
}

// Build collects structure and diagnostics from LSP proxies.
func (b *ContextBuilder) Build(ctx context.Context, uri string) (map[string]interface{}, error) {
	if b.Proxy == nil {
		return nil, fmt.Errorf("missing LSP proxy")
	}
	defTool := &tools.DefinitionTool{Proxy: b.Proxy}
	hoverTool := &tools.HoverTool{Proxy: b.Proxy}
	diagTool := &tools.DiagnosticsTool{Proxy: b.Proxy}

	state := framework.NewContext()
	if _, err := defTool.Execute(ctx, state, map[string]interface{}{
		"file":      uri,
		"symbol":    "",
		"line":      0,
		"character": 0,
	}); err != nil {
		return nil, err
	}
	hoverRes, err := hoverTool.Execute(ctx, state, map[string]interface{}{
		"file":      uri,
		"line":      0,
		"character": 0,
	})
	if err != nil {
		return nil, err
	}
	diagRes, err := diagTool.Execute(ctx, state, map[string]interface{}{"file": uri})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"hover":       hoverRes.Data,
		"diagnostics": diagRes.Data,
	}, nil
}

// AgentFactory builds default agent stack for the server.
func AgentFactory(model framework.LanguageModel, toolsRegistry *framework.ToolRegistry, mem framework.MemoryStore, cfg *framework.Config) framework.Agent {
	var delegate framework.Agent
	if cfg != nil && cfg.DisableToolCalling {
		manual := &agents.ManualCodingAgent{
			Model:  model,
			Tools:  toolsRegistry,
			Config: cfg,
		}
		_ = manual.Initialize(cfg)
		reflection := &agents.ReflectionAgent{
			Reviewer: model,
			Delegate: manual,
		}
		_ = reflection.Initialize(cfg)
		return reflection
	} else {
		expert := &agents.ExpertCoderAgent{
			Model:  model,
			Tools:  toolsRegistry,
			Memory: mem,
		}
		if err := expert.Initialize(cfg); err == nil {
			return expert
		}
		coding := &agents.CodingAgent{
			Model:  model,
			Tools:  toolsRegistry,
			Memory: mem,
		}
		_ = coding.Initialize(cfg)
		delegate = coding
	}
	reflection := &agents.ReflectionAgent{
		Reviewer: model,
		Delegate: delegate,
	}
	_ = reflection.Initialize(cfg)
	return reflection
}

// Serialize AI results for HTTP responses.
func (res *AIResult) Serialize() []byte {
	data, _ := json.Marshal(res)
	return data
}
