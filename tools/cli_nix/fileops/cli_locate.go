package fileops

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewLocateTool exposes the locate CLI.
func NewLocateTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_locate",
		Description: "Queries the file database via locate.",
		Command:     "locate",
		Category:    "cli_files",
	})
}
