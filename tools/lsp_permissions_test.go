package tools

import (
	"testing"

	"github.com/lexcodex/relurpify/framework"
)

func TestLSPToolPermissionsValidate(t *testing.T) {
	tools := []framework.Tool{
		&DefinitionTool{},
		&ReferencesTool{},
		&HoverTool{},
		&DiagnosticsTool{},
		&SearchSymbolsTool{},
		&DocumentSymbolsTool{},
		&FormatTool{},
	}
	for _, tool := range tools {
		if err := tool.Permissions().Validate(); err != nil {
			t.Fatalf("%s permissions invalid: %v", tool.Name(), err)
		}
	}
}
