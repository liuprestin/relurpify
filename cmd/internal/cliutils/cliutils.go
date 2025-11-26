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
	ID         string
	Factory    func(root string) (tools.LSPClient, error)
	Extensions []string
	Commands   []string
}

var (
	lspDescriptorMap       = map[string]LSPDescriptor{}
	canonicalDescriptorMap = map[string]LSPDescriptor{}
)

func init() {
	addDescriptor([]string{"go", "gopls"}, LSPDescriptor{ID: "go", Factory: tools.NewGoplsClient, Extensions: []string{"go"}, Commands: []string{"gopls"}})
	addDescriptor([]string{"rust", "rs", "rust-analyzer"}, LSPDescriptor{ID: "rust", Factory: tools.NewRustAnalyzerClient, Extensions: []string{"rs"}, Commands: []string{"rust-analyzer"}})
	addDescriptor([]string{"clang", "clangd", "c", "cpp", "cc"}, LSPDescriptor{ID: "clangd", Factory: tools.NewClangdClient, Extensions: []string{"c", "h", "cpp", "hpp", "cc", "cxx"}, Commands: []string{"clangd"}})
	addDescriptor([]string{"haskell", "hls"}, LSPDescriptor{ID: "haskell", Factory: tools.NewHaskellClient, Extensions: []string{"hs"}, Commands: []string{"haskell-language-server-wrapper", "haskell-language-server"}})
	addDescriptor([]string{"ts", "typescript"}, LSPDescriptor{ID: "typescript", Factory: tools.NewTypeScriptClient, Extensions: []string{"ts", "tsx"}, Commands: []string{"typescript-language-server"}})
	addDescriptor([]string{"js", "javascript"}, LSPDescriptor{ID: "javascript", Factory: tools.NewTypeScriptClient, Extensions: []string{"js", "jsx"}, Commands: []string{"typescript-language-server"}})
	addDescriptor([]string{"lua"}, LSPDescriptor{ID: "lua", Factory: tools.NewLuaClient, Extensions: []string{"lua"}, Commands: []string{"lua-language-server"}})
	addDescriptor([]string{"python", "py", "pylsp"}, LSPDescriptor{ID: "python", Factory: tools.NewPythonLSPClient, Extensions: []string{"py"}, Commands: []string{"pylsp"}})
}

func addDescriptor(keys []string, desc LSPDescriptor) {
	if len(keys) == 0 {
		return
	}
	if desc.ID == "" {
		desc.ID = strings.ToLower(keys[0])
	}
	for _, key := range keys {
		lspDescriptorMap[strings.ToLower(key)] = desc
	}
	if _, ok := canonicalDescriptorMap[desc.ID]; !ok {
		canonicalDescriptorMap[desc.ID] = desc
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
func NewProxyForLanguage(language, root string) (*tools.Proxy, *tools.ProxyInstance, func(), error) {
	if language == "" {
		return nil, nil, nil, nil
	}
	desc, ok := LookupLSPDescriptor(language)
	if !ok {
		return nil, nil, nil, fmt.Errorf("unsupported language %s", language)
	}
	client, err := desc.Factory(root)
	if err != nil {
		return nil, nil, nil, err
	}
	proxy := tools.NewProxy(time.Minute)
	for _, ext := range desc.Extensions {
		proxy.Register(ext, client)
	}
	var instance *tools.ProxyInstance
	var logs <-chan string
	if emitter, ok := client.(tools.LogEmitter); ok {
		logs = emitter.Logs()
	}
	if metaProvider, ok := client.(tools.ProcessMetadataProvider); ok {
		meta := metaProvider.ProcessMetadata()
		instance = &tools.ProxyInstance{
			Language: desc.ID,
			Command:  meta.Command,
			PID:      meta.PID,
			Started:  meta.Started,
			Logs:     logs,
		}
	} else if logs != nil {
		instance = &tools.ProxyInstance{
			Language: desc.ID,
			Logs:     logs,
		}
	}
	cleanup := func() {
		if closer, ok := client.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
	return proxy, instance, cleanup, nil
}

// CanonicalLSPDescriptors returns a copy of the deduplicated descriptor map keyed by canonical ID.
func CanonicalLSPDescriptors() map[string]LSPDescriptor {
	res := make(map[string]LSPDescriptor, len(canonicalDescriptorMap))
	for k, v := range canonicalDescriptorMap {
		res[k] = v
	}
	return res
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
