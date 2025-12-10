package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/framework/ast"
)

// DocumentSymbolToolProvider wraps the lsp_document_symbols tool so AST
// indexing can source symbols through the existing permission and proxy
// infrastructure.
type DocumentSymbolToolProvider struct {
	tool framework.Tool
}

// NewDocumentSymbolToolProvider builds a provider that executes the wrapped
// tool directly. The supplied tool should be fetched from a ToolRegistry so it
// carries the permission manager wrapper.
func NewDocumentSymbolToolProvider(tool framework.Tool) *DocumentSymbolToolProvider {
	if tool == nil {
		return nil
	}
	return &DocumentSymbolToolProvider{tool: tool}
}

// DocumentSymbols implements ast.DocumentSymbolProvider.
func (p *DocumentSymbolToolProvider) DocumentSymbols(ctx context.Context, path string) ([]ast.DocumentSymbol, error) {
	if p == nil || p.tool == nil {
		return nil, fmt.Errorf("document symbol tool unavailable")
	}
	state := framework.NewContext()
	res, err := p.tool.Execute(ctx, state, map[string]interface{}{"file": path})
	if err != nil {
		return nil, err
	}
	raw, ok := res.Data["symbols"]
	if !ok {
		return nil, fmt.Errorf("document symbols payload missing")
	}
	info, err := castSymbolInformation(raw)
	if err != nil {
		return nil, err
	}
	return convertSymbolInformation(info), nil
}

// AttachASTSymbolProvider inspects the registry for the LSP document symbols
// tool and wires it into the AST indexer when present.
func AttachASTSymbolProvider(manager *ast.IndexManager, registry *framework.ToolRegistry) {
	if manager == nil || registry == nil {
		return
	}
	tool, ok := registry.Get("lsp_document_symbols")
	if !ok || tool == nil {
		return
	}
	if !tool.IsAvailable(context.Background(), framework.NewContext()) {
		return
	}
	provider := NewDocumentSymbolToolProvider(tool)
	manager.UseSymbolProvider(provider)
}

func castSymbolInformation(raw interface{}) ([]SymbolInformation, error) {
	if raw == nil {
		return nil, fmt.Errorf("empty symbol payload")
	}
	if list, ok := raw.([]SymbolInformation); ok {
		return list, nil
	}
	// When the tool result crosses package boundaries the slice may decay to []interface{}.
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected symbol payload type %T", raw)
	}
	result := make([]SymbolInformation, 0, len(items))
	for _, item := range items {
		if sym, ok := item.(SymbolInformation); ok {
			result = append(result, sym)
			continue
		}
		if m, ok := item.(map[string]interface{}); ok {
			result = append(result, SymbolInformation{
				Name:     fmt.Sprint(m["name"]),
				Kind:     fmt.Sprint(m["kind"]),
				Location: fmt.Sprint(m["location"]),
			})
		}
	}
	return result, nil
}

func convertSymbolInformation(input []SymbolInformation) []ast.DocumentSymbol {
	result := make([]ast.DocumentSymbol, 0, len(input))
	for _, sym := range input {
		line := extractLine(sym.Location)
		nodeType := mapSymbolKind(sym.Kind)
		result = append(result, ast.DocumentSymbol{
			Name:      sym.Name,
			Kind:      nodeType,
			StartLine: line,
			EndLine:   line,
		})
	}
	return result
}

func extractLine(location string) int {
	parts := strings.Split(location, ":")
	if len(parts) < 2 {
		return 1
	}
	line, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 1
	}
	// LSP lines are zero-based; shift to one-based for AST storage.
	return line + 1
}

func mapSymbolKind(kind string) ast.NodeType {
	switch kind {
	case "5": // Class
		return ast.NodeTypeClass
	case "6": // Method
		return ast.NodeTypeMethod
	case "7", "8": // Property/Field
		return ast.NodeTypeVariable
	case "9": // Constructor
		return ast.NodeTypeFunction
	case "10": // Enum
		return ast.NodeTypeEnum
	case "11": // Interface
		return ast.NodeTypeInterface
	case "12": // Function
		return ast.NodeTypeFunction
	case "13": // Variable
		return ast.NodeTypeVariable
	case "14": // Constant
		return ast.NodeTypeConstant
	case "23": // Struct
		return ast.NodeTypeStruct
	default:
		return ast.NodeTypeSection
	}
}
