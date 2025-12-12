package fileops

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewMkdirTool exposes the mkdir CLI utility for directory creation.
func NewMkdirTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_mkdir",
		Description: "Creates directories via mkdir (defaults to -p).",
		Command:     "mkdir",
		Category:    "cli_files",
		DefaultArgs: []string{"-p"},
	})
}
