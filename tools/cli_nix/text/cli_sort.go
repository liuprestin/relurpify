package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewSortTool exposes the sort CLI.
func NewSortTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_sort",
		Description: "Sorts lines of text with the sort utility.",
		Command:     "sort",
		Category:    "cli_text",
	})
}
