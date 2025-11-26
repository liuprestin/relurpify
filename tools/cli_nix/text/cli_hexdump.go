package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewHexdumpTool exposes the hexdump CLI.
func NewHexdumpTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_hexdump",
		Description: "Inspects binary data using hexdump.",
		Command:     "hexdump",
		Category:    "cli_text",
	})
}
