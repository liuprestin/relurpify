package fileops

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewFDTool exposes the fd CLI.
func NewFDTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_fd",
		Description: "Performs fast file searches with fd.",
		Command:     "fd",
		Category:    "cli_files",
	})
}
