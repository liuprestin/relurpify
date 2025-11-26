package system

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewDFTool exposes the df CLI.
func NewDFTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_df",
		Description: "Reports disk usage statistics with df.",
		Command:     "df",
		Category:    "cli_system",
	})
}
