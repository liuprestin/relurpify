package archive

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewXzTool exposes the xz CLI.
func NewXzTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_xz",
		Description: "Compresses data with xz.",
		Command:     "xz",
		Category:    "cli_archive",
	})
}
