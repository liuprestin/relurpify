package agents

import contextual "github.com/lexcodex/relurpify/agents/contextual"

type (
	ContextStrategy  = contextual.ContextStrategy
	ContextRequest   = contextual.ContextRequest
	FileRequest      = contextual.FileRequest
	DetailLevel      = contextual.DetailLevel
	ASTQuery         = contextual.ASTQuery
	ASTQueryType     = contextual.ASTQueryType
	ASTFilter        = contextual.ASTFilter
	MemoryQuery      = contextual.MemoryQuery
	SearchQuery      = contextual.SearchQuery
	ContextLoadEvent = contextual.ContextLoadEvent
)

const (
	DetailFull          = contextual.DetailFull
	DetailDetailed      = contextual.DetailDetailed
	DetailConcise       = contextual.DetailConcise
	DetailMinimal       = contextual.DetailMinimal
	DetailSignatureOnly = contextual.DetailSignatureOnly

	ASTQueryListSymbols     = contextual.ASTQueryListSymbols
	ASTQueryGetSignature    = contextual.ASTQueryGetSignature
	ASTQueryFindCallers     = contextual.ASTQueryFindCallers
	ASTQueryFindCallees     = contextual.ASTQueryFindCallees
	ASTQueryGetDependencies = contextual.ASTQueryGetDependencies
)

func NewAggressiveStrategy() *contextual.AggressiveStrategy {
	return contextual.NewAggressiveStrategy()
}
func NewConservativeStrategy() *contextual.ConservativeStrategy {
	return contextual.NewConservativeStrategy()
}
func NewAdaptiveStrategy() *contextual.AdaptiveStrategy { return contextual.NewAdaptiveStrategy() }

func ExtractFileReferences(text string) []string   { return contextual.ExtractFileReferences(text) }
func ExtractSymbolReferences(text string) []string { return contextual.ExtractSymbolReferences(text) }
func ExtractKeywords(text string) string           { return contextual.ExtractKeywords(text) }
func ContainsInsensitive(text, substr string) bool {
	return contextual.ContainsInsensitive(text, substr)
}
