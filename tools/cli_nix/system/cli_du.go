package system

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewDUTool exposes the du CLI.
func NewDUTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_du",
		Description: "Summarizes directory usage with du.",
		Command:     "du",
		Category:    "cli_system",
	})
}
