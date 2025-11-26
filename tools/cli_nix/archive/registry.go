package archive

import "github.com/lexcodex/relurpify/framework"

// Tools returns archiving/compression helpers.
func Tools(basePath string) []framework.Tool {
	return []framework.Tool{
		NewTarTool(basePath),
		NewGzipTool(basePath),
		NewBzip2Tool(basePath),
		NewXzTool(basePath),
	}
}
