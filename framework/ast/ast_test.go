package ast

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLanguageDetector(t *testing.T) {
	detector := NewLanguageDetector()
	if lang := detector.Detect("main.go"); lang != "go" {
		t.Fatalf("expected go, got %s", lang)
	}
	if lang := detector.Detect("README.md"); lang != "markdown" {
		t.Fatalf("expected markdown, got %s", lang)
	}
	if cat := detector.DetectCategory("yaml"); cat != CategoryConfig {
		t.Fatalf("expected config category, got %s", cat)
	}
	if cat := detector.DetectCategory("unknown-lang"); cat != CategoryDoc {
		t.Fatalf("expected doc category fallback, got %s", cat)
	}
}

type stubParser struct {
	language string
}

func (s *stubParser) Parse(content string, path string) (*ParseResult, error) {
	return &ParseResult{
		Nodes: []*Node{},
		Edges: []*Edge{},
		Metadata: &FileMetadata{
			ID:          GenerateFileID(path),
			Path:        path,
			Language:    s.language,
			Category:    CategoryDoc,
			ContentHash: HashContent(content),
			IndexedAt:   time.Now(),
		},
	}, nil
}

func (s *stubParser) ParseIncremental(_ *ParseResult, _ []ContentChange) (*ParseResult, error) {
	return nil, nil
}

func (s *stubParser) Language() string          { return s.language }
func (s *stubParser) Category() Category        { return CategoryDoc }
func (s *stubParser) SupportsIncremental() bool { return false }

func TestParserRegistry(t *testing.T) {
	registry := NewParserRegistry()
	parser := &stubParser{language: "custom"}
	registry.Register(parser)
	if _, ok := registry.GetParser("custom"); !ok {
		t.Fatal("expected parser to be registered")
	}
	supported := registry.SupportedLanguages()
	if len(supported) != 1 || supported[0] != "custom" {
		t.Fatalf("unexpected supported languages: %v", supported)
	}
}

func TestGoParserParse(t *testing.T) {
	source := `package sample
import "fmt"
func Hello(name string) string {
	return fmt.Sprintf("hi %s", name)
}`
	parser := NewGoParser()
	result, err := parser.Parse(source, "sample.go")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if result.Metadata == nil || result.Metadata.Language != "go" {
		t.Fatalf("metadata not populated: %#v", result.Metadata)
	}
	if len(result.Nodes) < 3 {
		t.Fatalf("expected several nodes, got %d", len(result.Nodes))
	}
	if result.RootNode == nil || result.RootNode.Type != NodeTypePackage {
		t.Fatalf("root node incorrect: %#v", result.RootNode)
	}
	if len(result.Edges) == 0 {
		t.Fatalf("expected import edges, got %d", len(result.Edges))
	}
}

func TestMarkdownParserParse(t *testing.T) {
	content := "# Title\n\nSome text.\n\n## Section\n\n```go\nfmt.Println(\"hi\")\n```\n"
	parser := NewMarkdownParser()
	result, err := parser.Parse(content, "doc.md")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if result.RootNode == nil || result.RootNode.Type != NodeTypeDocument {
		t.Fatalf("expected document root, got %#v", result.RootNode)
	}
	if len(result.Nodes) < 3 {
		t.Fatalf("expected heading and code nodes, got %d", len(result.Nodes))
	}
}

func TestSQLiteStoreCRUD(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(tmpDir, "index.db"))
	if err != nil {
		t.Fatalf("sqlite init failed: %v", err)
	}
	defer store.Close()
	meta := &FileMetadata{
		ID:           "file1",
		Path:         "sample.go",
		RelativePath: "sample.go",
		Language:     "go",
		Category:     CategoryCode,
		ContentHash:  "hash",
		IndexedAt:    time.Now(),
	}
	if err := store.SaveFile(meta); err != nil {
		t.Fatalf("save file failed: %v", err)
	}
	nodes := []*Node{
		{
			ID:        "n1",
			FileID:    meta.ID,
			Type:      NodeTypePackage,
			Category:  CategoryCode,
			Language:  "go",
			Name:      "sample",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			ID:        "n2",
			FileID:    meta.ID,
			Type:      NodeTypeFunction,
			Category:  CategoryCode,
			Language:  "go",
			Name:      "Hello",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	if err := store.SaveNodes(nodes); err != nil {
		t.Fatalf("save nodes failed: %v", err)
	}
	edges := []*Edge{{
		ID:       "e1",
		SourceID: "n1",
		TargetID: "n2",
		Type:     EdgeTypeContains,
	}}
	if err := store.SaveEdges(edges); err != nil {
		t.Fatalf("save edges failed: %v", err)
	}
	fetched, err := store.GetFile(meta.ID)
	if err != nil || fetched == nil {
		t.Fatalf("get file failed: %v", err)
	}
	node, err := store.GetNode("n2")
	if err != nil || node == nil {
		t.Fatalf("get node failed: %v", err)
	}
	results, err := store.SearchNodes(NodeQuery{NamePattern: "Hello"})
	if err != nil || len(results) == 0 {
		t.Fatalf("search nodes failed: %v", err)
	}
	stats, err := store.GetStats()
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}
	if stats.TotalFiles == 0 || stats.TotalNodes == 0 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

type fakeSymbolProvider struct {
	symbols []DocumentSymbol
}

func (f fakeSymbolProvider) DocumentSymbols(ctx context.Context, path string) ([]DocumentSymbol, error) {
	return f.symbols, nil
}

func TestIndexManagerSymbolFallback(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(tmpDir, "index.db"))
	if err != nil {
		t.Fatalf("sqlite init failed: %v", err)
	}
	defer store.Close()
	manager := NewIndexManager(store, IndexConfig{WorkspacePath: tmpDir})
	manager.UseSymbolProvider(fakeSymbolProvider{
		symbols: []DocumentSymbol{{
			Name:      "handler",
			Kind:      NodeTypeFunction,
			StartLine: 1,
			EndLine:   3,
		}},
	})
	path := filepath.Join(tmpDir, "main.py")
	if err := os.WriteFile(path, []byte("print('hi')"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := manager.IndexFile(path); err != nil {
		t.Fatalf("index file failed: %v", err)
	}
	meta, err := store.GetFileByPath(path)
	if err != nil || meta == nil {
		t.Fatalf("expected metadata, got err=%v", err)
	}
	if meta.Language != "python" {
		t.Fatalf("expected python language, got %s", meta.Language)
	}
	nodes, err := store.GetNodesByFile(meta.ID)
	if err != nil {
		t.Fatalf("fetch nodes failed: %v", err)
	}
	if len(nodes) < 2 {
		t.Fatalf("expected symbol nodes, got %d", len(nodes))
	}
}
