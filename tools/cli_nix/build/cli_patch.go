package build

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewPatchTool creates a patch wrapper.
func NewPatchTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_patch",
		Description: "Apply a diff file to an original.",
		Command:     "patch",
		Category:    "cli_build",
	})
}

// NewDiffTool creates a diff wrapper.
func NewDiffTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_diff",
		Description: "Compare files line by line.",
		Command:     "diff",
		Category:    "cli_build",
	})
}
