package build

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewMakeTool exposes the make CLI.
func NewMakeTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_make",
		Description: "Runs make targets for builds.",
		Command:     "make",
		Category:    "cli_build",
	})
}
