package archive

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewTarTool exposes the tar CLI.
func NewTarTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_tar",
		Description: "Creates or extracts tar archives.",
		Command:     "tar",
		Category:    "cli_archive",
	})
}
