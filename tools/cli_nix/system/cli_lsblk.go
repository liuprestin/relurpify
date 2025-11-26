package system

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewLsblkTool exposes the lsblk CLI.
func NewLsblkTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_lsblk",
		Description: "Lists block devices via lsblk.",
		Command:     "lsblk",
		Category:    "cli_system",
	})
}
