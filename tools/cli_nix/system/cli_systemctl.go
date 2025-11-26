package system

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewSystemctlTool exposes the systemctl CLI.
func NewSystemctlTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_systemctl",
		Description: "Manages systemd services via systemctl.",
		Command:     "systemctl",
		Category:    "cli_system",
	})
}
