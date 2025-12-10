package contextual

import (
	"os"
	"strings"

	"github.com/lexcodex/relurpify/framework"
)

// EstimateContextTokens approximates current context usage for strategies.
func EstimateContextTokens(ctx *framework.SharedContext) int {
	if ctx == nil {
		return 0
	}
	usage := ctx.GetTokenUsage()
	if usage == nil {
		return 0
	}
	return usage.Total
}

// ReadFile loads the file content with a friendly error.
func ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ApproximateTokens performs a quick character-to-token conversion.
func ApproximateTokens(content string) int {
	if content == "" {
		return 0
	}
	length := len(content)
	if length == 0 {
		return 0
	}
	return max(1, length/4)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TrimLower lowers and trims strings for tokenization heuristics.
func TrimLower(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}
