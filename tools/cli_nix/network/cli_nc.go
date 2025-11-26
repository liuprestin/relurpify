package network

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewNCTool exposes the nc CLI.
func NewNCTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_nc",
		Description: "Creates TCP/UDP connections via netcat (nc).",
		Command:     "nc",
		Category:    "cli_network",
	})
}
