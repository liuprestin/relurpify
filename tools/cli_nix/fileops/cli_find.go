package fileops

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewFindTool exposes the find CLI.
func NewFindTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_find",
		Description: "Searches the filesystem using find.",
		Command:     "find",
		Category:    "cli_files",
	})
}
