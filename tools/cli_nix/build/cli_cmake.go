package build

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewCMakeTool exposes the cmake CLI.
func NewCMakeTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_cmake",
		Description: "Configures builds with cmake.",
		Command:     "cmake",
		Category:    "cli_build",
	})
}
