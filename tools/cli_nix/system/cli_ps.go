package system

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewPSTool exposes the ps CLI.
func NewPSTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_ps",
		Description: "Inspects running processes via ps.",
		Command:     "ps",
		Category:    "cli_system",
	})
}
