package network

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewSSTool exposes the ss CLI.
func NewSSTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_ss",
		Description: "Inspects sockets using ss.",
		Command:     "ss",
		Category:    "cli_network",
	})
}
