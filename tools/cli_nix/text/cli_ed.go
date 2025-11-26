package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewEdTool exposes the ed CLI.
func NewEdTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_ed",
		Description: "Runs the ed line editor for scripted edits.",
		Command:     "ed",
		Category:    "cli_text",
	})
}
