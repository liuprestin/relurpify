package fileops

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewTreeTool exposes the tree CLI.
func NewTreeTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_tree",
		Description: "Displays directory trees using tree.",
		Command:     "tree",
		Category:    "cli_files",
	})
}
