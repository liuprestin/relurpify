package contextual

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/framework/ast"
)

// ContextStrategy defines how an agent manages context.
type ContextStrategy interface {
	// SelectContext determines what context to load initially.
	SelectContext(task *framework.Task, budget *framework.ContextBudget) (*ContextRequest, error)

	// ShouldCompress decides when to compress history.
	ShouldCompress(ctx *framework.SharedContext) bool

	// DetermineDetailLevel chooses appropriate detail for content.
	DetermineDetailLevel(file string, relevance float64) DetailLevel

	// ShouldExpandContext decides if more context is needed.
	ShouldExpandContext(ctx *framework.SharedContext, lastResult *framework.Result) bool

	// PrioritizeContext ranks context items by importance.
	PrioritizeContext(items []framework.ContextItem) []framework.ContextItem
}

// ContextRequest describes what context to load.
type ContextRequest struct {
	Files         []FileRequest
	ASTQueries    []ASTQuery
	MemoryQueries []MemoryQuery
	SearchQueries []SearchQuery
	MaxTokens     int
}

// FileRequest specifies how to load a file.
type FileRequest struct {
	Path        string
	DetailLevel DetailLevel
	Priority    int
	Pinned      bool
}

// DetailLevel controls content granularity.
type DetailLevel int

const (
	DetailFull DetailLevel = iota
	DetailDetailed
	DetailConcise
	DetailMinimal
	DetailSignatureOnly
)

func (dl DetailLevel) String() string {
	switch dl {
	case DetailFull:
		return "full"
	case DetailDetailed:
		return "detailed"
	case DetailConcise:
		return "concise"
	case DetailMinimal:
		return "minimal"
	case DetailSignatureOnly:
		return "signature_only"
	default:
		return "unknown"
	}
}

// ASTQuery requests structured code information.
type ASTQuery struct {
	Type   ASTQueryType
	Symbol string
	Filter ASTFilter
}

// ASTQueryType enumerates supported AST operations.
type ASTQueryType string

const (
	ASTQueryListSymbols     ASTQueryType = "list_symbols"
	ASTQueryGetSignature    ASTQueryType = "get_signature"
	ASTQueryFindCallers     ASTQueryType = "find_callers"
	ASTQueryFindCallees     ASTQueryType = "find_callees"
	ASTQueryGetDependencies ASTQueryType = "get_dependencies"
)

// ASTFilter narrows down AST responses.
type ASTFilter struct {
	Types        []ast.NodeType
	Categories   []ast.Category
	ExportedOnly bool
}

// MemoryQuery requests information from memory stores.
type MemoryQuery struct {
	Scope      framework.MemoryScope
	Query      string
	MaxResults int
}

// SearchQuery requests semantic/keyword search.
type SearchQuery struct {
	Query        string
	Mode         framework.SearchMode
	FilePatterns []string
	MaxResults   int
}

// ContextLoadEvent tracks context loading decisions.
type ContextLoadEvent struct {
	Timestamp   time.Time
	Trigger     string
	RequestType string
	ItemsLoaded int
	TokensAdded int
	Success     bool
	Reason      string
}

var (
	fileReferenceRegex   = regexp.MustCompile(`[\w./-]+\.[\w]+`)
	symbolReferenceRegex = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]+)\(`)
)

// ExtractFileReferences scans text for file-like tokens.
func ExtractFileReferences(text string) []string {
	matches := fileReferenceRegex.FindAllString(text, -1)
	unique := make(map[string]struct{})
	refs := make([]string, 0, len(matches))
	for _, match := range matches {
		clean := filepath.Clean(match)
		if _, ok := unique[clean]; ok {
			continue
		}
		unique[clean] = struct{}{}
		refs = append(refs, clean)
	}
	return refs
}

// ExtractSymbolReferences returns probable symbol names mentioned in the text.
func ExtractSymbolReferences(text string) []string {
	matches := symbolReferenceRegex.FindAllStringSubmatch(text, -1)
	unique := make(map[string]struct{})
	symbols := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := match[1]
		if _, ok := unique[name]; ok {
			continue
		}
		unique[name] = struct{}{}
		symbols = append(symbols, name)
	}
	return symbols
}

// ExtractKeywords returns a truncated keyword string for search queries.
func ExtractKeywords(text string) string {
	words := strings.Fields(text)
	if len(words) > 10 {
		words = words[:10]
	}
	return strings.Join(words, " ")
}

// ContainsInsensitive checks if substr appears in text ignoring case.
func ContainsInsensitive(text, substr string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(substr))
}

func countKeywords(text string, keywords []string) int {
	count := 0
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			count++
		}
	}
	return count
}
