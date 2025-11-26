package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewUniqTool exposes the uniq CLI.
func NewUniqTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_uniq",
		Description: "Filters or counts duplicate lines with uniq.",
		Command:     "uniq",
		Category:    "cli_text",
	})
}
