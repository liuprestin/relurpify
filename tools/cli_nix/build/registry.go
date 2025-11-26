package build

import "github.com/lexcodex/relurpify/framework"

// Tools returns build-system helpers.
func Tools(basePath string) []framework.Tool {
	return []framework.Tool{
		NewMakeTool(basePath),
		NewCMakeTool(basePath),
		NewPkgConfigTool(basePath),
	}
}
