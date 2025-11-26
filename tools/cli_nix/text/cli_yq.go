package text

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewYQTool exposes the yq CLI.
func NewYQTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_yq",
		Description: "Processes YAML content using yq.",
		Command:     "yq",
		Category:    "cli_text",
	})
}
