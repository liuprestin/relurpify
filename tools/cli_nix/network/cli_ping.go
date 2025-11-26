package network

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewPingTool exposes the ping CLI.
func NewPingTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_ping",
		Description: "Checks host reachability with ping.",
		Command:     "ping",
		Category:    "cli_network",
	})
}
