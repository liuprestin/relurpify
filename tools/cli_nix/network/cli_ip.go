package network

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewIPTool exposes the ip CLI.
func NewIPTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_ip",
		Description: "Manages network interfaces with ip.",
		Command:     "ip",
		Category:    "cli_network",
	})
}
