package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lexcodex/relurpify/framework"
)

func TestReadWriteListFileTools(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	state := framework.NewContext()

	writeTool := &WriteFileTool{BasePath: dir, Backup: true}
	_, err := writeTool.Execute(ctx, state, map[string]interface{}{
		"path":    "hello.txt",
		"content": "hi relurpify",
	})
	assert.NoError(t, err)

	readTool := &ReadFileTool{BasePath: dir}
	readRes, err := readTool.Execute(ctx, state, map[string]interface{}{"path": "hello.txt"})
	assert.NoError(t, err)
	assert.Equal(t, "hi relurpify", readRes.Data["content"])

	listTool := &ListFilesTool{BasePath: dir}
	listRes, err := listTool.Execute(ctx, state, map[string]interface{}{
		"directory": ".",
		"pattern":   "*.txt",
	})
	assert.NoError(t, err)
	files := listRes.Data["files"].([]string)
	assert.Len(t, files, 1)
	assert.Equal(t, filepath.Join(dir, "hello.txt"), files[0])
}

func TestSearchInFilesTool(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "code.go")
	assert.NoError(t, os.WriteFile(file, []byte("package main\n// TODO: fix bug\n"), 0o644))

	tool := &SearchInFilesTool{BasePath: dir}
	res, err := tool.Execute(context.Background(), framework.NewContext(), map[string]interface{}{
		"directory": ".",
		"pattern":   "TODO",
	})
	assert.NoError(t, err)
	bytes, err := json.Marshal(res.Data["matches"])
	assert.NoError(t, err)
	var decoded []map[string]interface{}
	assert.NoError(t, json.Unmarshal(bytes, &decoded))
	assert.NotEmpty(t, decoded)
}
