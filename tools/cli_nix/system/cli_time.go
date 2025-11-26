package system

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewTimeTool exposes the time CLI.
func NewTimeTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_time",
		Description: "Times the execution of commands with /usr/bin/time.",
		Command:     "time",
		Category:    "cli_system",
	})
}
