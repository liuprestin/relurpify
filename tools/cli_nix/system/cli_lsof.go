package system

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewLsofTool exposes the lsof CLI.
func NewLsofTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_lsof",
		Description: "Lists open files and sockets via lsof.",
		Command:     "lsof",
		Category:    "cli_system",
	})
}
