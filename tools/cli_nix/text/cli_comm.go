package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewCommTool exposes the comm CLI.
func NewCommTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_comm",
		Description: "Compares sorted files using comm.",
		Command:     "comm",
		Category:    "cli_text",
	})
}
