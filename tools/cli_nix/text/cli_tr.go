package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewTRTool exposes the tr CLI.
func NewTRTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_tr",
		Description: "Translates or deletes characters with tr.",
		Command:     "tr",
		Category:    "cli_text",
	})
}
