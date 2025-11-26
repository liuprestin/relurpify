package network

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewWgetTool exposes the wget CLI.
func NewWgetTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_wget",
		Description: "Downloads resources with wget.",
		Command:     "wget",
		Category:    "cli_network",
	})
}
