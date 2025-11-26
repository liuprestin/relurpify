package system

import "github.com/lexcodex/relurpify/framework"

// Tools returns system inspection helpers.
func Tools(basePath string) []framework.Tool {
	return []framework.Tool{
		NewLsblkTool(basePath),
		NewDFTool(basePath),
		NewDUTool(basePath),
		NewPSTool(basePath),
		NewTopTool(basePath),
		NewHtopTool(basePath),
		NewLsofTool(basePath),
		NewStraceTool(basePath),
		NewTimeTool(basePath),
		NewUptimeTool(basePath),
		NewSystemctlTool(basePath),
	}
}
