package text

import "github.com/lexcodex/relurpify/framework"

// Tools returns text-processing related CLI helpers.
func Tools(basePath string) []framework.Tool {
	return []framework.Tool{
		NewAwkTool(basePath),
		NewEchoTool(basePath),
		NewSedTool(basePath),
		NewPerlTool(basePath),
		NewJQTool(basePath),
		NewYQTool(basePath),
		NewTRTool(basePath),
		NewCutTool(basePath),
		NewPasteTool(basePath),
		NewColumnTool(basePath),
		NewSortTool(basePath),
		NewUniqTool(basePath),
		NewCommTool(basePath),
		NewRevTool(basePath),
		NewWCTool(basePath),
		NewPatchTool(basePath),
		NewEdTool(basePath),
		NewExTool(basePath),
		NewXxdTool(basePath),
		NewHexdumpTool(basePath),
		NewDiffTool(basePath),
		NewColordiffTool(basePath),
	}
}
