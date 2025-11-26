package fileops

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewAGTool exposes the ag CLI.
func NewAGTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_ag",
		Description: "Searches codebases with the silver searcher (ag).",
		Command:     "ag",
		Category:    "cli_files",
	})
}
