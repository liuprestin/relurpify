package scheduler

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewCrontabTool exposes the crontab CLI.
func NewCrontabTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_crontab",
		Description: "Edits or lists cron jobs via crontab.",
		Command:     "crontab",
		Category:    "cli_scheduler",
	})
}
