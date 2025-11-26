package network

import "github.com/lexcodex/relurpify/framework"

// Tools returns networking helpers.
func Tools(basePath string) []framework.Tool {
	return []framework.Tool{
		NewCurlTool(basePath),
		NewWgetTool(basePath),
		NewNCTool(basePath),
		NewDigTool(basePath),
		NewNslookupTool(basePath),
		NewIPTool(basePath),
		NewSSTool(basePath),
		NewPingTool(basePath),
	}
}
