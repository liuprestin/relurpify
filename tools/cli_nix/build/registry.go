package build

import "github.com/lexcodex/relurpify/framework"

// Tools returns build-system helpers.
func Tools(basePath string) []framework.Tool {
	return []framework.Tool{
		NewMakeTool(basePath),
		NewCMakeTool(basePath),
		NewCargoTool(basePath),
		NewPkgConfigTool(basePath),
		NewGDBTool(basePath),
		NewValgrindTool(basePath),
		NewPatchTool(basePath),
		NewDiffTool(basePath),
		NewLddTool(basePath),
		NewObjdumpTool(basePath),
		NewPerfTool(basePath),
		NewStraceTool(basePath),
	}
}
