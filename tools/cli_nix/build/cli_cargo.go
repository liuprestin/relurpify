package build

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewCargoTool exposes the cargo CLI for Rust builds.
func NewCargoTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_cargo",
		Description: "Executes Rust cargo commands inside the workspace.",
		Command:     "cargo",
		Category:    "cli_build",
	})
}
