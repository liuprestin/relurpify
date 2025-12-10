package framework

import (
	"math"
)

// estimateTokens performs a cheap heuristic conversion from characters to tokens.
func estimateTokens(v interface{}) int {
	switch val := v.(type) {
	case string:
		return estimateTextTokens(val)
	case []Interaction:
		total := 0
		for _, i := range val {
			total += estimateTextTokens(i.Content)
		}
		return total
	case []KeyFact:
		total := 0
		for _, kf := range val {
			total += estimateTextTokens(kf.Content)
		}
		return total
	default:
		return 0
	}
}

func estimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	return maxInt(1, int(math.Ceil(float64(len(text))/4.0)))
}

func estimateCodeTokens(code string) int {
	if code == "" {
		return 0
	}
	return maxInt(1, int(math.Ceil(float64(len(code))/2.5)))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
