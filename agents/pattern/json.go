package pattern

import "strings"

// ExtractJSON returns the outermost JSON object inside a string response. When
// no braces are present it returns an empty JSON object so downstream
// unmarshalling still succeeds.
func ExtractJSON(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end >= start {
		return raw[start : end+1]
	}
	return "{}"
}

// ExtractJSONSnippet returns the substring containing the JSON payload if
// present. When delimiters are missing it returns an empty string so callers
// can surface a more helpful error.
func ExtractJSONSnippet(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end >= start {
		return raw[start : end+1]
	}
	return ""
}
