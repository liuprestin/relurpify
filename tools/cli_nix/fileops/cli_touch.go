package fileops

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewTouchTool exposes the touch CLI utility.
func NewTouchTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_touch",
		Description: "Creates empty files or updates timestamps via touch.",
		Command:     "touch",
		Category:    "cli_files",
	})
}
