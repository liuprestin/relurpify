package scheduler

import "github.com/lexcodex/relurpify/framework"

// Tools returns scheduling helpers.
func Tools(basePath string) []framework.Tool {
	return []framework.Tool{
		NewCrontabTool(basePath),
		NewAtTool(basePath),
	}
}
