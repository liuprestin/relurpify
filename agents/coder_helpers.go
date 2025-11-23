package agents

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lexcodex/relurpify/framework"
)

func gatherTaskFiles(task *framework.Task, state *framework.Context) []string {
	seen := map[string]struct{}{}
	var files []string
	addString := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if strings.HasPrefix(path, "file://") {
			path = strings.TrimPrefix(path, "file://")
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}
	var addValue func(val interface{})
	addValue = func(val interface{}) {
		switch v := val.(type) {
		case string:
			addString(v)
		case []string:
			for _, item := range v {
				addString(item)
			}
		case []interface{}:
			for _, item := range v {
				addValue(item)
			}
		}
	}
	if task != nil && task.Context != nil {
		addValue(task.Context["files"])
		addValue(task.Context["file"])
		addValue(task.Context["uri"])
	}
	if state != nil {
		if v, ok := state.Get("active.file"); ok {
			addValue(v)
		}
		if v, ok := state.Get("active.uri"); ok {
			addValue(v)
		}
	}
	return files
}

func detectTaskLanguage(task *framework.Task, state *framework.Context, files []string) string {
	for _, key := range []string{"language", "lang", "language_id"} {
		if lang := contextString(task, key); lang != "" {
			return strings.ToLower(lang)
		}
	}
	if state != nil {
		if val, ok := state.Get("active.language"); ok {
			if lang, ok := val.(string); ok && lang != "" {
				return strings.ToLower(lang)
			}
		}
	}
	for _, file := range files {
		if lang := inferLanguageFromPath(file); lang != "" {
			return lang
		}
	}
	return ""
}

func contextString(task *framework.Task, key string) string {
	if task == nil || task.Context == nil {
		return ""
	}
	raw, ok := task.Context[key]
	if !ok {
		return ""
	}
	if str, ok := raw.(string); ok {
		return strings.TrimSpace(str)
	}
	return ""
}

func inferLanguageFromPath(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		return ""
	}
	switch ext {
	case "go":
		return "go"
	case "rs":
		return "rust"
	case "c", "h", "cpp", "hpp", "cc", "cxx":
		return "clangd"
	case "ts", "tsx":
		return "ts"
	case "js", "jsx":
		return "javascript"
	case "lua":
		return "lua"
	case "py":
		return "python"
	case "hs":
		return "haskell"
	default:
		return ext
	}
}

func parseCodingAnalysis(raw string) (CodingAnalysis, error) {
	snippet := extractJSONSnippet(raw)
	if snippet == "" {
		return CodingAnalysis{}, fmt.Errorf("analysis response missing JSON object")
	}
	var payload struct {
		Plan  []interface{} `json:"plan"`
		Files []interface{} `json:"files"`
		Risks []interface{} `json:"risks"`
	}
	if err := json.Unmarshal([]byte(snippet), &payload); err != nil {
		return CodingAnalysis{}, err
	}
	return CodingAnalysis{
		Plan:  normalizeInterfaceSlice(payload.Plan),
		Files: normalizeInterfaceSlice(payload.Files),
		Risks: normalizeInterfaceSlice(payload.Risks),
		Raw:   raw,
	}, nil
}

func normalizeInterfaceSlice(items []interface{}) []string {
	if len(items) == 0 {
		return []string{}
	}
	res := make([]string, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case string:
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				res = append(res, trimmed)
			}
		case map[string]interface{}, []interface{}:
			if buf, err := json.Marshal(v); err == nil {
				res = append(res, string(buf))
			}
		default:
			res = append(res, fmt.Sprint(v))
		}
	}
	return res
}

func extractJSONSnippet(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end >= start {
		return raw[start : end+1]
	}
	return ""
}

func uniqueStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
