package scheduler

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewAtTool exposes the at CLI.
func NewAtTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_at",
		Description: "Schedules one-off jobs using at.",
		Command:     "at",
		Category:    "cli_scheduler",
	})
}
