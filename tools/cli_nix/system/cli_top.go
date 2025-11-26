package system

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewTopTool exposes the top CLI.
func NewTopTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_top",
		Description: "Monitors processes interactively with top.",
		Command:     "top",
		Category:    "cli_system",
	})
}
