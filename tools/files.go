package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lexcodex/relurpify/framework"
)

var errBinaryFile = errors.New("binary file detected")

// ReadFileTool reads files from disk.
type ReadFileTool struct {
	BasePath string
}

func (t *ReadFileTool) Name() string        { return "file_read" }
func (t *ReadFileTool) Description() string { return "Reads a UTF-8 file from disk." }
func (t *ReadFileTool) Category() string    { return "file" }
func (t *ReadFileTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{{Name: "path", Type: "string", Required: true}}
}
func (t *ReadFileTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	path := t.preparePath(fmt.Sprint(args["path"]))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !isText(data) {
		return nil, errBinaryFile
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &framework.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"content": string(data),
			"size":    info.Size(),
			"mode":    info.Mode().String(),
		},
	}, nil
}
func (t *ReadFileTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return true
}

// WriteFileTool writes content to disk.
type WriteFileTool struct {
	BasePath string
	Backup   bool
}

func (t *WriteFileTool) Name() string        { return "file_write" }
func (t *WriteFileTool) Description() string { return "Writes content to a file with backup." }
func (t *WriteFileTool) Category() string    { return "file" }
func (t *WriteFileTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "path", Type: "string", Required: true},
		{Name: "content", Type: "string", Required: true},
	}
}
func (t *WriteFileTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	path := t.preparePath(fmt.Sprint(args["path"]))
	content := []byte(fmt.Sprint(args["content"]))
	if t.Backup {
		if _, err := os.Stat(path); err == nil {
			backup := path + ".bak"
			if err := copyFile(path, backup); err != nil {
				return nil, err
			}
		}
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return nil, err
	}
	return &framework.ToolResult{Success: true, Data: map[string]interface{}{"path": path}}, nil
}
func (t *WriteFileTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return true
}

// ListFilesTool lists files filtered by pattern.
type ListFilesTool struct {
	BasePath string
}

func (t *ListFilesTool) Name() string        { return "file_list" }
func (t *ListFilesTool) Description() string { return "Lists files recursively using glob filtering." }
func (t *ListFilesTool) Category() string    { return "file" }
func (t *ListFilesTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "directory", Type: "string", Required: false, Default: "."},
		{Name: "pattern", Type: "string", Required: false, Default: "*"},
	}
}
func (t *ListFilesTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	dir := t.preparePath(fmt.Sprint(args["directory"]))
	pattern := fmt.Sprint(args["pattern"])
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".git") {
				return fs.SkipDir
			}
			return nil
		}
		match, _ := filepath.Match(pattern, filepath.Base(path))
		if match {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &framework.ToolResult{Success: true, Data: map[string]interface{}{"files": files}}, nil
}
func (t *ListFilesTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return true
}

// SearchInFilesTool greps for a pattern.
type SearchInFilesTool struct {
	BasePath string
}

func (t *SearchInFilesTool) Name() string        { return "file_search" }
func (t *SearchInFilesTool) Description() string { return "Searches text inside files." }
func (t *SearchInFilesTool) Category() string    { return "file" }
func (t *SearchInFilesTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "directory", Type: "string", Required: false, Default: "."},
		{Name: "pattern", Type: "string", Required: true},
	}
}
func (t *SearchInFilesTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	dir := t.preparePath(fmt.Sprint(args["directory"]))
	pattern := fmt.Sprint(args["pattern"])
	type match struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Content string `json:"content"`
	}
	var matches []match
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".git") {
				return fs.SkipDir
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
			if strings.Contains(scanner.Text(), pattern) {
				matches = append(matches, match{
					File:    path,
					Line:    line,
					Content: scanner.Text(),
				})
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
func (t *SearchInFilesTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return true
}

// CreateFileTool creates a file from a template string.
type CreateFileTool struct {
	BasePath string
}

func (t *CreateFileTool) Name() string        { return "file_create" }
func (t *CreateFileTool) Description() string { return "Creates a new file if it does not exist." }
func (t *CreateFileTool) Category() string    { return "file" }
func (t *CreateFileTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "path", Type: "string", Required: true},
		{Name: "content", Type: "string", Required: false},
	}
}
func (t *CreateFileTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	path := t.preparePath(fmt.Sprint(args["path"]))
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("file %s already exists", path)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprint(args["content"])), 0o644); err != nil {
		return nil, err
	}
	return &framework.ToolResult{Success: true, Data: map[string]interface{}{"path": path}}, nil
}
func (t *CreateFileTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return true
}

// DeleteFileTool moves a file to .trash folder instead of deleting permanently.
type DeleteFileTool struct {
	BasePath string
	TrashDir string
}

func (t *DeleteFileTool) Name() string        { return "file_delete" }
func (t *DeleteFileTool) Description() string { return "Deletes a file after confirmation." }
func (t *DeleteFileTool) Category() string    { return "file" }
func (t *DeleteFileTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{{Name: "path", Type: "string", Required: true}}
}
func (t *DeleteFileTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	path := t.preparePath(fmt.Sprint(args["path"]))
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	trash := t.TrashDir
	if trash == "" {
		trash = filepath.Join(t.BasePath, ".trash")
	}
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return nil, err
	}
	dest := filepath.Join(trash, info.Name())
	if err := os.Rename(path, dest); err != nil {
		return nil, err
	}
	return &framework.ToolResult{Success: true, Data: map[string]interface{}{"path": dest}}, nil
}
func (t *DeleteFileTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return true
}

func (t *ReadFileTool) preparePath(path string) string  { return preparePath(t.BasePath, path) }
func (t *WriteFileTool) preparePath(path string) string { return preparePath(t.BasePath, path) }
func (t *ListFilesTool) preparePath(path string) string { return preparePath(t.BasePath, path) }
func (t *SearchInFilesTool) preparePath(path string) string {
	return preparePath(t.BasePath, path)
}
func (t *CreateFileTool) preparePath(path string) string { return preparePath(t.BasePath, path) }
func (t *DeleteFileTool) preparePath(path string) string { return preparePath(t.BasePath, path) }

func preparePath(base, path string) string {
	if base == "" {
		return filepath.Clean(path)
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}

func isText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	return true
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return nil
}

// FileOperations registers default file tools into a registry.
func FileOperations(basePath string) []framework.Tool {
	return []framework.Tool{
		&ReadFileTool{BasePath: basePath},
		&WriteFileTool{BasePath: basePath, Backup: true},
		&ListFilesTool{BasePath: basePath},
		&SearchInFilesTool{BasePath: basePath},
		&CreateFileTool{BasePath: basePath},
		&DeleteFileTool{BasePath: basePath},
	}
}

// FileLock protects operations that cannot race (write/delete).
type FileLock struct {
	mu sync.Mutex
}

func (l *FileLock) Run(fn func() error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return fn()
}
