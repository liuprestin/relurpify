package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewExTool exposes the ex CLI.
func NewExTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_ex",
		Description: "Executes ex for vi-style batch editing.",
		Command:     "ex",
		Category:    "cli_text",
	})
}
