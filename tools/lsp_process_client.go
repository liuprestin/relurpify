package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"go.lsp.dev/protocol"
)

// ProcessLSPConfig defines the configuration for spinning up a language server process.
type ProcessLSPConfig struct {
	Command    string
	Args       []string
	RootDir    string
	LanguageID string
}

type processLSPClient struct {
	cfg         ProcessLSPConfig
	cmd         *exec.Cmd
	conn        *jsonrpc2.Conn
	cancel      context.CancelFunc
	mu          sync.Mutex
	openedFiles map[protocol.DocumentURI]bool
	diagnostics map[protocol.DocumentURI][]protocol.Diagnostic
}

// NewProcessLSPClient launches the configured language server and performs the LSP handshake.
func NewProcessLSPClient(cfg ProcessLSPConfig) (LSPClient, error) {
	if cfg.Command == "" {
		return nil, errors.New("command is required for LSP client")
	}
	if cfg.LanguageID == "" {
		return nil, errors.New("language id is required for LSP client")
	}
	root := cfg.RootDir
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = absRoot

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}

	rwc := &stdioReadWriteCloser{reader: stdout, writer: stdin}
	stream := jsonrpc2.NewBufferedStream(rwc, jsonrpc2.VSCodeObjectCodec{})

	client := &processLSPClient{
		cfg:         cfg,
		cmd:         cmd,
		cancel:      cancel,
		openedFiles: make(map[protocol.DocumentURI]bool),
		diagnostics: make(map[protocol.DocumentURI][]protocol.Diagnostic),
	}

	handler := jsonrpc2.HandlerWithError(func(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) (interface{}, error) {
		if !req.Notif {
			return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeMethodNotFound, Message: "method not handled"}
		}
		switch req.Method {
		case "textDocument/publishDiagnostics":
			var params protocol.PublishDiagnosticsParams
			if err := json.Unmarshal(*req.Params, &params); err != nil {
				return nil, err
			}
			client.mu.Lock()
			client.diagnostics[params.URI] = params.Diagnostics
			client.mu.Unlock()
			return nil, nil
		default:
			return nil, nil
		}
	})

	conn := jsonrpc2.NewConn(ctx, stream, handler)
	client.conn = conn

	go io.Copy(os.Stderr, stderr)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	if err := client.initialize(ctx, absRoot); err != nil {
		cancel()
		_ = cmd.Process.Kill()
		return nil, err
	}

	return client, nil
}

func (c *processLSPClient) initialize(ctx context.Context, root string) error {
	params := &protocol.InitializeParams{
		ProcessID: int32(os.Getpid()),
		RootURI:   protocol.DocumentURI(pathToURI(root)),
		ClientInfo: &protocol.ClientInfo{
			Name:    "relurpify",
			Version: "0.1",
		},
		Capabilities: protocol.ClientCapabilities{
			TextDocument: &protocol.TextDocumentClientCapabilities{
				Hover:              &protocol.HoverTextDocumentClientCapabilities{},
				Definition:         &protocol.DefinitionTextDocumentClientCapabilities{},
				References:         &protocol.ReferencesTextDocumentClientCapabilities{},
				DocumentSymbol:     &protocol.DocumentSymbolClientCapabilities{},
				Formatting:         &protocol.DocumentFormattingClientCapabilities{},
				PublishDiagnostics: &protocol.PublishDiagnosticsClientCapabilities{},
			},
			Workspace: &protocol.WorkspaceClientCapabilities{
				Symbol: &protocol.WorkspaceClientCapabilitiesSymbol{},
			},
		},
	}
	var result protocol.InitializeResult
	if err := c.conn.Call(ctx, "initialize", params, &result); err != nil {
		return err
	}
	return c.conn.Notify(ctx, "initialized", &protocol.InitializedParams{})
}

// Close terminates the underlying process and JSON-RPC connection.
func (c *processLSPClient) Close() error {
	if c == nil {
		return nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
	return nil
}

func (c *processLSPClient) ensureOpen(ctx context.Context, file string) error {
	uri := protocol.DocumentURI(pathToURI(file))
	c.mu.Lock()
	if c.openedFiles[uri] {
		c.mu.Unlock()
		return nil
	}
	c.openedFiles[uri] = true
	c.mu.Unlock()

	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	params := protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        uri,
			LanguageID: protocol.LanguageIdentifier(c.cfg.LanguageID),
			Version:    1,
			Text:       string(data),
		},
	}
	return c.conn.Notify(ctx, "textDocument/didOpen", params)
}

func (c *processLSPClient) GetDefinition(ctx context.Context, req DefinitionRequest) (DefinitionResult, error) {
	if err := c.ensureOpen(ctx, req.File); err != nil {
		return DefinitionResult{}, err
	}
	params := protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(pathToURI(req.File))},
			Position:     protocol.Position{Line: uint32(req.Position.Line), Character: uint32(req.Position.Character)},
		},
	}
	var resp []protocol.Location
	if err := c.conn.Call(ctx, "textDocument/definition", params, &resp); err != nil {
		return DefinitionResult{}, err
	}
	if len(resp) == 0 {
		return DefinitionResult{}, errors.New("definition not found")
	}
	loc := resp[0]
	snippet, _ := readSnippet(uriToPath(string(loc.URI)), loc.Range)
	return DefinitionResult{
		Location: Location{
			URI:   string(loc.URI),
			Range: [2]int64{int64(loc.Range.Start.Line), int64(loc.Range.End.Line)},
		},
		Snippet:   snippet,
		Signature: snippet,
	}, nil
}

func (c *processLSPClient) GetReferences(ctx context.Context, req ReferencesRequest) ([]Location, error) {
	if err := c.ensureOpen(ctx, req.File); err != nil {
		return nil, err
	}
	params := protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(pathToURI(req.File))},
			Position:     protocol.Position{Line: uint32(req.Position.Line), Character: uint32(req.Position.Character)},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: false},
	}
	var resp []protocol.Location
	if err := c.conn.Call(ctx, "textDocument/references", params, &resp); err != nil {
		return nil, err
	}
	locations := make([]Location, 0, len(resp))
	for _, loc := range resp {
		locations = append(locations, Location{
			URI:   string(loc.URI),
			Range: [2]int64{int64(loc.Range.Start.Line), int64(loc.Range.End.Line)},
		})
	}
	return locations, nil
}

func (c *processLSPClient) GetHover(ctx context.Context, req HoverRequest) (HoverResult, error) {
	if err := c.ensureOpen(ctx, req.File); err != nil {
		return HoverResult{}, err
	}
	params := protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(pathToURI(req.File))},
			Position:     protocol.Position{Line: uint32(req.Position.Line), Character: uint32(req.Position.Character)},
		},
	}
	var resp protocol.Hover
	if err := c.conn.Call(ctx, "textDocument/hover", params, &resp); err != nil {
		return HoverResult{}, err
	}
	return HoverResult{
		TypeInfo: fmt.Sprint(resp.Contents.Value),
		Docs:     fmt.Sprint(resp.Contents.Value),
	}, nil
}

func (c *processLSPClient) GetDiagnostics(ctx context.Context, file string) ([]Diagnostic, error) {
	if err := c.ensureOpen(ctx, file); err != nil {
		return nil, err
	}
	uri := protocol.DocumentURI(pathToURI(file))
	// wait for publishDiagnostics notification
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		c.mu.Lock()
		if diag, ok := c.diagnostics[uri]; ok {
			c.mu.Unlock()
			return convertDiagnostics(diag), nil
		}
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, errors.New("diagnostics timeout")
		case <-ticker.C:
		}
	}
}

func (c *processLSPClient) SearchSymbols(ctx context.Context, query string) ([]SymbolInformation, error) {
	params := protocol.WorkspaceSymbolParams{Query: query}
	var resp []protocol.SymbolInformation
	if err := c.conn.Call(ctx, "workspace/symbol", params, &resp); err != nil {
		return nil, err
	}
	result := make([]SymbolInformation, 0, len(resp))
	for _, sym := range resp {
		result = append(result, SymbolInformation{
			Name:     sym.Name,
			Kind:     fmt.Sprintf("%d", int(sym.Kind)),
			Location: fmt.Sprintf("%s:%d", string(sym.Location.URI), int(sym.Location.Range.Start.Line)),
		})
	}
	return result, nil
}

func (c *processLSPClient) GetDocumentSymbols(ctx context.Context, file string) ([]SymbolInformation, error) {
	if err := c.ensureOpen(ctx, file); err != nil {
		return nil, err
	}
	params := protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(pathToURI(file))},
	}
	var raw json.RawMessage
	if err := c.conn.Call(ctx, "textDocument/documentSymbol", params, &raw); err != nil {
		return nil, err
	}
	var symbols []SymbolInformation
	var docSymbols []protocol.DocumentSymbol
	if err := json.Unmarshal(raw, &docSymbols); err == nil && len(docSymbols) > 0 {
		collectDocumentSymbols(&symbols, file, docSymbols)
		return symbols, nil
	}
	var infoSymbols []protocol.SymbolInformation
	if err := json.Unmarshal(raw, &infoSymbols); err == nil {
		for _, sym := range infoSymbols {
			symbols = append(symbols, SymbolInformation{
				Name:     sym.Name,
				Kind:     fmt.Sprintf("%d", int(sym.Kind)),
				Location: fmt.Sprintf("%s:%d", string(sym.Location.URI), int(sym.Location.Range.Start.Line)),
			})
		}
		return symbols, nil
	}
	return nil, errors.New("document symbol response not understood")
}

func (c *processLSPClient) Format(ctx context.Context, req FormatRequest) (string, error) {
	if err := c.ensureOpen(ctx, req.File); err != nil {
		return "", err
	}
	params := protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(pathToURI(req.File))},
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}
	var edits []protocol.TextEdit
	if err := c.conn.Call(ctx, "textDocument/formatting", params, &edits); err != nil {
		return "", err
	}
	if len(edits) == 0 {
		return req.Code, nil
	}
	// If server replaces entire document, return that.
	if len(edits) == 1 && edits[0].Range.Start.Line == 0 {
		return edits[0].NewText, nil
	}
	content := req.Code
	for _, edit := range edits {
		content = edit.NewText
	}
	return content, nil
}

func convertDiagnostics(diags []protocol.Diagnostic) []Diagnostic {
	result := make([]Diagnostic, 0, len(diags))
	for _, d := range diags {
		result = append(result, Diagnostic{
			Severity: fmt.Sprintf("%d", int(d.Severity)),
			Message:  d.Message,
			Source:   d.Source,
			Line:     int(d.Range.Start.Line),
		})
	}
	return result
}

type stdioReadWriteCloser struct {
	reader io.ReadCloser
	writer io.WriteCloser
}

func (s *stdioReadWriteCloser) Read(p []byte) (int, error)  { return s.reader.Read(p) }
func (s *stdioReadWriteCloser) Write(p []byte) (int, error) { return s.writer.Write(p) }
func (s *stdioReadWriteCloser) Close() error {
	_ = s.reader.Close()
	return s.writer.Close()
}

func pathToURI(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ReplaceAll(path, "\\", "/")
		return "file:///" + strings.ReplaceAll(path, ":", "%3A")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "file://" + path
}

func uriToPath(uri string) string {
	uri = strings.TrimPrefix(uri, "file://")
	uri = strings.ReplaceAll(uri, "%3A", ":")
	return filepath.FromSlash(uri)
}

func readSnippet(path string, rng protocol.Range) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	start := int(rng.Start.Line)
	if start >= len(lines) {
		return "", nil
	}
	end := int(rng.End.Line)
	if end >= len(lines) {
		end = len(lines) - 1
	}
	return strings.Join(lines[start:end+1], "\n"), nil
}

func collectDocumentSymbols(dst *[]SymbolInformation, file string, symbols []protocol.DocumentSymbol) {
	for _, sym := range symbols {
		*dst = append(*dst, SymbolInformation{
			Name:     sym.Name,
			Kind:     fmt.Sprintf("%d", int(sym.Kind)),
			Location: fmt.Sprintf("%s:%d", file, int(sym.Range.Start.Line)),
		})
		if len(sym.Children) > 0 {
			collectDocumentSymbols(dst, file, sym.Children)
		}
	}
}

// Wrapper helpers for known servers.

func NewRustAnalyzerClient(root string) (LSPClient, error) {
	return NewProcessLSPClient(ProcessLSPConfig{
		Command:    "rust-analyzer",
		RootDir:    root,
		LanguageID: "rust",
	})
}

func NewGoplsClient(root string) (LSPClient, error) {
	return NewProcessLSPClient(ProcessLSPConfig{
		Command:    "gopls",
		Args:       []string{"serve"},
		RootDir:    root,
		LanguageID: "go",
	})
}

func NewClangdClient(root string) (LSPClient, error) {
	return NewProcessLSPClient(ProcessLSPConfig{
		Command:    "clangd",
		RootDir:    root,
		LanguageID: "c",
	})
}

func NewHaskellClient(root string) (LSPClient, error) {
	return NewProcessLSPClient(ProcessLSPConfig{
		Command:    "haskell-language-server-wrapper",
		Args:       []string{"--lsp"},
		RootDir:    root,
		LanguageID: "haskell",
	})
}

func NewTypeScriptClient(root string) (LSPClient, error) {
	return NewProcessLSPClient(ProcessLSPConfig{
		Command:    "typescript-language-server",
		Args:       []string{"--stdio"},
		RootDir:    root,
		LanguageID: "typescript",
	})
}

func NewLuaClient(root string) (LSPClient, error) {
	return NewProcessLSPClient(ProcessLSPConfig{
		Command:    "lua-language-server",
		RootDir:    root,
		LanguageID: "lua",
	})
}

func NewPythonLSPClient(root string) (LSPClient, error) {
	return NewProcessLSPClient(ProcessLSPConfig{
		Command:    "pylsp",
		RootDir:    root,
		LanguageID: "python",
	})
}
