package fileops

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewFileTool exposes the file CLI.
func NewFileTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_file",
		Description: "Detects file types using the file command.",
		Command:     "file",
		Category:    "cli_files",
	})
}
