package network

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewNslookupTool exposes the nslookup CLI.
func NewNslookupTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_nslookup",
		Description: "Performs DNS lookups via nslookup.",
		Command:     "nslookup",
		Category:    "cli_network",
	})
}
