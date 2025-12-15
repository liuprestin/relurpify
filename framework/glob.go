package framework

import (
	"path/filepath"
	"regexp"
	"strings"
)

// MatchGlob supports both filepath.Match and the '**' recursive glob pattern.
func MatchGlob(pattern, value string) bool {
	if pattern == "" {
		return false
	}
	if pattern == permissionMatchAll {
		return true
	}
	pattern = filepath.ToSlash(pattern)
	value = filepath.ToSlash(value)
	if !strings.Contains(pattern, "**") {
		ok, err := filepath.Match(pattern, value)
		if err != nil {
			return false
		}
		return ok
	}
	regexPattern := globToRegexPublic(pattern)
	regex, err := regexp.Compile(regexPattern)
	if err != nil {
		return false
	}
	return regex.MatchString(value)
}

func globToRegexPublic(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch ch {
		case '*':
			peek := ""
			if i+1 < len(runes) {
				peek = string(runes[i+1])
			}
			if peek == "*" {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString(".")
		case '.', '+', '(', ')', '|', '^', '$', '[', ']', '{', '}', '\\':
			b.WriteRune('\\')
			b.WriteRune(ch)
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteString("$")
	return b.String()
}

