package archive

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewGzipTool exposes the gzip CLI.
func NewGzipTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_gzip",
		Description: "Compresses data with gzip.",
		Command:     "gzip",
		Category:    "cli_archive",
	})
}
