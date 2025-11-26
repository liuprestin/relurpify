package system

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewHtopTool exposes the htop CLI.
func NewHtopTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_htop",
		Description: "Runs htop for interactive process monitoring.",
		Command:     "htop",
		Category:    "cli_system",
	})
}
