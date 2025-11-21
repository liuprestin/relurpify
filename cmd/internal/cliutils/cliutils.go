package cliutils

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/tools"
)

// BuildToolRegistry registers default file/search tools rooted at basePath.
func BuildToolRegistry(basePath string) *framework.ToolRegistry {
	registry := framework.NewToolRegistry()
	for _, tool := range tools.FileOperations(basePath) {
		_ = registry.Register(tool)
	}
	for _, tool := range []framework.Tool{
		&tools.GrepTool{BasePath: basePath},
		&tools.SemanticSearchTool{BasePath: basePath},
		&tools.SimilarityTool{BasePath: basePath},
	} {
		_ = registry.Register(tool)
	}
	return registry
}

// RegisterLSPTools wires LSP-capable tools into the registry when a proxy is present.
func RegisterLSPTools(registry *framework.ToolRegistry, proxy *tools.Proxy) {
	if proxy == nil {
		return
	}
	for _, tool := range []framework.Tool{
		&tools.DefinitionTool{Proxy: proxy},
		&tools.ReferencesTool{Proxy: proxy},
		&tools.HoverTool{Proxy: proxy},
		&tools.DiagnosticsTool{Proxy: proxy},
		&tools.SearchSymbolsTool{Proxy: proxy},
		&tools.DocumentSymbolsTool{Proxy: proxy},
		&tools.FormatTool{Proxy: proxy},
	} {
		_ = registry.Register(tool)
	}
}

// LSPDescriptor captures metadata needed to start and register an LSP client.
type LSPDescriptor struct {
	Factory    func(root string) (tools.LSPClient, error)
	Extensions []string
}

var lspDescriptorMap = map[string]LSPDescriptor{}

func init() {
	addDescriptor([]string{"go", "gopls"}, LSPDescriptor{Factory: tools.NewGoplsClient, Extensions: []string{"go"}})
	addDescriptor([]string{"rust", "rs", "rust-analyzer"}, LSPDescriptor{Factory: tools.NewRustAnalyzerClient, Extensions: []string{"rs"}})
	addDescriptor([]string{"clang", "clangd", "c", "cpp", "cc"}, LSPDescriptor{Factory: tools.NewClangdClient, Extensions: []string{"c", "h", "cpp", "hpp", "cc", "cxx"}})
	addDescriptor([]string{"haskell", "hls"}, LSPDescriptor{Factory: tools.NewHaskellClient, Extensions: []string{"hs"}})
	addDescriptor([]string{"ts", "typescript"}, LSPDescriptor{Factory: tools.NewTypeScriptClient, Extensions: []string{"ts", "tsx"}})
	addDescriptor([]string{"js", "javascript"}, LSPDescriptor{Factory: tools.NewTypeScriptClient, Extensions: []string{"js", "jsx"}})
	addDescriptor([]string{"lua"}, LSPDescriptor{Factory: tools.NewLuaClient, Extensions: []string{"lua"}})
	addDescriptor([]string{"python", "py", "pylsp"}, LSPDescriptor{Factory: tools.NewPythonLSPClient, Extensions: []string{"py"}})
}

func addDescriptor(keys []string, desc LSPDescriptor) {
	for _, key := range keys {
		lspDescriptorMap[strings.ToLower(key)] = desc
	}
}

// LookupLSPDescriptor finds the descriptor for a given key/alias.
func LookupLSPDescriptor(language string) (LSPDescriptor, bool) {
	desc, ok := lspDescriptorMap[strings.ToLower(language)]
	return desc, ok
}

// SupportedLSPKeys lists known aliases.
func SupportedLSPKeys() []string {
	keys := make([]string, 0, len(lspDescriptorMap))
	seen := map[string]bool{}
	for key := range lspDescriptorMap {
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

// NewProxyForLanguage creates a proxy and registers the descriptor's extensions. Cleanup closes the client process.
func NewProxyForLanguage(language, root string) (*tools.Proxy, func(), error) {
	if language == "" {
		return nil, nil, nil
	}
	desc, ok := LookupLSPDescriptor(language)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported language %s", language)
	}
	client, err := desc.Factory(root)
	if err != nil {
		return nil, nil, err
	}
	proxy := tools.NewProxy(time.Minute)
	for _, ext := range desc.Extensions {
		proxy.Register(ext, client)
	}
	cleanup := func() {
		if closer, ok := client.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
	return proxy, cleanup, nil
}

// InferLanguageByExtension returns a language key given a file path.
func InferLanguageByExtension(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		return ""
	}
	switch ext {
	case "go":
		return "go"
	case "rs":
		return "rust"
	case "c", "h", "cpp", "hpp", "cc", "cxx":
		return "clangd"
	case "ts", "tsx":
		return "ts"
	case "js", "jsx":
		return "javascript"
	case "lua":
		return "lua"
	case "py":
		return "python"
	case "hs":
		return "haskell"
	default:
		return ext
	}
}
