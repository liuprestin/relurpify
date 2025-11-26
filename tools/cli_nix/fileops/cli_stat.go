package fileops

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewStatTool exposes the stat CLI.
func NewStatTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_stat",
		Description: "Shows file metadata with stat.",
		Command:     "stat",
		Category:    "cli_files",
	})
}
