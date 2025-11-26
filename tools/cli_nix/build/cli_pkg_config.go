package build

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewPkgConfigTool exposes the pkg-config CLI.
func NewPkgConfigTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_pkg_config",
		Description: "Queries compiler flags with pkg-config.",
		Command:     "pkg-config",
		Category:    "cli_build",
	})
}
