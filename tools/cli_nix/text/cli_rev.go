package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewRevTool exposes the rev CLI.
func NewRevTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_rev",
		Description: "Reverses lines character-wise using rev.",
		Command:     "rev",
		Category:    "cli_text",
	})
}
