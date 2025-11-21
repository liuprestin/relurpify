package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lexcodex/relurpify/framework"
)

// GrepTool implements plain text search.
type GrepTool struct {
	BasePath string
}

func (t *GrepTool) Name() string        { return "search_grep" }
func (t *GrepTool) Description() string { return "Searches files using substring matching." }
func (t *GrepTool) Category() string    { return "search" }
func (t *GrepTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "pattern", Type: "string", Required: true},
		{Name: "directory", Type: "string", Required: false, Default: "."},
	}
}
func (t *GrepTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	root := fmt.Sprint(args["directory"])
	if root == "" {
		root = "."
	}
	root = preparePath(t.BasePath, root)
	pattern := strings.ToLower(fmt.Sprint(args["pattern"]))
	type match struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Content string `json:"content"`
	}
	var matches []match
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.Contains(path, string(filepath.Separator)+".git") {
				return filepath.SkipDir
			}
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		line := 1
		for scanner.Scan() {
			text := scanner.Text()
			if strings.Contains(strings.ToLower(text), pattern) {
				matches = append(matches, match{File: path, Line: line, Content: text})
			}
			line++
		}
		return scanner.Err()
	})
	if err != nil {
		return nil, err
	}
	return &framework.ToolResult{Success: true, Data: map[string]interface{}{"matches": matches}}, nil
}
func (t *GrepTool) IsAvailable(ctx context.Context, state *framework.Context) bool { return true }

// SimilarityTool finds similar snippets using a naive approach.
type SimilarityTool struct {
	BasePath string
}

func (t *SimilarityTool) Name() string        { return "search_find_similar" }
func (t *SimilarityTool) Description() string { return "Finds structurally similar code snippets." }
func (t *SimilarityTool) Category() string    { return "search" }
func (t *SimilarityTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "snippet", Type: "string", Required: true},
		{Name: "directory", Type: "string", Required: false, Default: "."},
	}
}
func (t *SimilarityTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	root := preparePath(t.BasePath, fmt.Sprint(args["directory"]))
	target := sanitizeSnippet(fmt.Sprint(args["snippet"]))
	type match struct {
		File     string  `json:"file"`
		Score    float64 `json:"score"`
		Fragment string  `json:"fragment"`
	}
	var matches []match
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if err == nil && info.IsDir() && strings.Contains(path, ".git") {
				return filepath.SkipDir
			}
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(data)
		score := jaccard(target, sanitizeSnippet(content))
		if score > 0.3 {
			matches = append(matches, match{File: path, Score: score, Fragment: summarize(content)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &framework.ToolResult{Success: true, Data: map[string]interface{}{"matches": matches}}, nil
}
func (t *SimilarityTool) IsAvailable(ctx context.Context, state *framework.Context) bool { return true }

// SemanticSearchTool uses a vector-like heuristic (currently substring).
type SemanticSearchTool struct {
	BasePath string
}

func (t *SemanticSearchTool) Name() string { return "search_semantic" }
func (t *SemanticSearchTool) Description() string {
	return "Performs semantic search using heuristic embeddings."
}
func (t *SemanticSearchTool) Category() string { return "search" }
func (t *SemanticSearchTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{{Name: "query", Type: "string", Required: true}}
}
func (t *SemanticSearchTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	query := strings.ToLower(fmt.Sprint(args["query"]))
	var hits []map[string]interface{}
	err := filepath.Walk(t.BasePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.Contains(path, ".git") {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := strings.ToLower(string(data))
		if strings.Contains(content, query) {
			hits = append(hits, map[string]interface{}{
				"file":    path,
				"score":   float64(len(query)) / float64(len(content)+1),
				"snippet": summarize(string(data)),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &framework.ToolResult{Success: true, Data: map[string]interface{}{"results": hits}}, nil
}
func (t *SemanticSearchTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return true
}

func sanitizeSnippet(snippet string) string {
	return strings.ToLower(strings.ReplaceAll(snippet, " ", ""))
}

func jaccard(a, b string) float64 {
	setA := make(map[rune]bool)
	for _, r := range a {
		setA[r] = true
	}
	setB := make(map[rune]bool)
	for _, r := range b {
		setB[r] = true
	}
	intersection := 0
	for r := range setA {
		if setB[r] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func summarize(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 5 {
		return strings.Join(lines[:5], "\n")
	}
	return content
}
