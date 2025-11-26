package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewDiffTool exposes the diff CLI.
func NewDiffTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_diff",
		Description: "Runs diff to compare files.",
		Command:     "diff",
		Category:    "cli_text",
	})
}
