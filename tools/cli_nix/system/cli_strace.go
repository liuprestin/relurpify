package system

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewStraceTool exposes the strace CLI.
func NewStraceTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_strace",
		Description: "Traces syscalls made by a process using strace.",
		Command:     "strace",
		Category:    "cli_system",
	})
}
