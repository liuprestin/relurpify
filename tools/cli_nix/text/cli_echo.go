package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewEchoTool exposes the echo CLI utility.
func NewEchoTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_echo",
		Description: "Writes text to standard output using echo.",
		Command:     "echo",
		Category:    "cli_text",
	})
}
