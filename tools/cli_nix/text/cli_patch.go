package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewPatchTool exposes the patch CLI.
func NewPatchTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_patch",
		Description: "Applies unified diffs using patch.",
		Command:     "patch",
		Category:    "cli_text",
	})
}
