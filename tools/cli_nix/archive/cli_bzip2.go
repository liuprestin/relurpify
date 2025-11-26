package archive

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewBzip2Tool exposes the bzip2 CLI.
func NewBzip2Tool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_bzip2",
		Description: "Compresses data using bzip2.",
		Command:     "bzip2",
		Category:    "cli_archive",
	})
}
