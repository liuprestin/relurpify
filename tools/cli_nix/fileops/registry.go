package fileops

import "github.com/lexcodex/relurpify/framework"

// Tools returns file navigation/search helpers.
func Tools(basePath string) []framework.Tool {
	return []framework.Tool{
		NewFindTool(basePath),
		NewFDTool(basePath),
		NewRGTool(basePath),
		NewAGTool(basePath),
		NewLocateTool(basePath),
		NewTreeTool(basePath),
		NewStatTool(basePath),
		NewFileTool(basePath),
		NewTouchTool(basePath),
		NewMkdirTool(basePath),
	}
}
