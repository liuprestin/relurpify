package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewJQTool exposes the jq CLI.
func NewJQTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_jq",
		Description: "Queries or transforms JSON using jq.",
		Command:     "jq",
		Category:    "cli_text",
	})
}
