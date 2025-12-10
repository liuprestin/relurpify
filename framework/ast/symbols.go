package ast

import "context"

// DocumentSymbol captures structure information provided by external sources
// such as LSP servers. Only the subset of metadata required to construct AST
// nodes is modeled here.
type DocumentSymbol struct {
	Name      string
	Kind      NodeType
	StartLine int
	EndLine   int
	Children  []DocumentSymbol
}

// DocumentSymbolProvider supplies symbols for files that lack parsers.
type DocumentSymbolProvider interface {
	DocumentSymbols(ctx context.Context, path string) ([]DocumentSymbol, error)
}
