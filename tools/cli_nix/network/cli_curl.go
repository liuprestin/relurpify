package network

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewCurlTool exposes the curl CLI.
func NewCurlTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_curl",
		Description: "Transfers data over HTTP(S) using curl.",
		Command:     "curl",
		Category:    "cli_network",
	})
}
