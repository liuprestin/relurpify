package system

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewUptimeTool exposes the uptime CLI.
func NewUptimeTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_uptime",
		Description: "Shows system uptime information.",
		Command:     "uptime",
		Category:    "cli_system",
	})
}
