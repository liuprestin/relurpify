package fileops

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewRGTool exposes the rg CLI.
func NewRGTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_rg",
		Description: "Uses ripgrep (rg) for recursive code search.",
		Command:     "rg",
		Category:    "cli_files",
	})
}
