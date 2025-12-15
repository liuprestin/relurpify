package ast

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// IndexConfig configures the IndexManager.
type IndexConfig struct {
	WorkspacePath     string
	EnableIncremental bool
	EnableSummaries   bool
	ParallelWorkers   int
	IgnorePatterns    []string
}

// IndexManager orchestrates parsing and persistence.
type IndexManager struct {
	store            IndexStore
	parserRegistry   *ParserRegistry
	languageDetector *LanguageDetector
	mu               sync.Mutex
	indexing         map[string]bool
	config           IndexConfig
	symbolProvider   DocumentSymbolProvider
	pathFilter       func(path string, isDir bool) bool
}

// NewIndexManager builds a manager with default parsers.
func NewIndexManager(store IndexStore, config IndexConfig) *IndexManager {
	manager := &IndexManager{
		store:            store,
		parserRegistry:   NewParserRegistry(),
		languageDetector: NewLanguageDetector(),
		indexing:         make(map[string]bool),
		config:           config,
	}
	manager.registerDefaultParsers()
	return manager
}

func (im *IndexManager) registerDefaultParsers() {
	im.RegisterParser(NewGoParser())
	im.RegisterParser(NewMarkdownParser())
}

// RegisterParser makes an additional parser available.
func (im *IndexManager) RegisterParser(parser Parser) {
	if parser == nil {
		return
	}
	im.parserRegistry.Register(parser)
}

// UseSymbolProvider wires an optional document symbol source for fallback
// indexing when language-specific parsers are not available.
func (im *IndexManager) UseSymbolProvider(provider DocumentSymbolProvider) {
	im.mu.Lock()
	defer im.mu.Unlock()
	im.symbolProvider = provider
}

// SetPathFilter installs an optional filter that can skip directories/files
// during indexing (e.g. to enforce manifest filesystem permissions).
func (im *IndexManager) SetPathFilter(filter func(path string, isDir bool) bool) {
	im.mu.Lock()
	defer im.mu.Unlock()
	im.pathFilter = filter
}

// IndexFile parses and stores AST for a file path.
func (im *IndexManager) IndexFile(path string) error {
	im.mu.Lock()
	filter := im.pathFilter
	im.mu.Unlock()
	if filter != nil && !filter(path, false) {
		return nil
	}
	im.mu.Lock()
	if im.indexing[path] {
		im.mu.Unlock()
		return fmt.Errorf("index already running for %s", path)
	}
	im.indexing[path] = true
	im.mu.Unlock()
	defer func() {
		im.mu.Lock()
		delete(im.indexing, path)
		im.mu.Unlock()
	}()

	language := im.languageDetector.Detect(path)
	category := im.languageDetector.DetectCategory(language)
	parser, ok := im.parserRegistry.GetParser(language)

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	contentHash := HashContent(string(content))

	if existing, err := im.store.GetFileByPath(path); err == nil && existing != nil {
		if existing.ContentHash == contentHash {
			return nil
		}
		if err := im.store.DeleteFile(existing.ID); err != nil {
			return fmt.Errorf("delete previous index: %w", err)
		}
	}

	if !ok {
		return im.indexWithSymbols(path, string(content), language, category, contentHash)
	}

	result, err := parser.Parse(string(content), path)
	if err != nil {
		if symErr := im.indexWithSymbols(path, string(content), language, category, contentHash); symErr == nil {
			return nil
		}
		return err
	}
	return im.persist(result, contentHash)
}

// IndexWorkspace walks the workspace and indexes files.
func (im *IndexManager) IndexWorkspace() error {
	root := im.config.WorkspacePath
	if root == "" {
		root = "."
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		im.mu.Lock()
		filter := im.pathFilter
		im.mu.Unlock()
		if d.IsDir() {
			if filter != nil && !filter(path, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if filter != nil && !filter(path, false) {
			return nil
		}
		if im.shouldIgnore(path) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return err
	}
	if im.config.ParallelWorkers > 1 {
		return im.indexFilesParallel(files)
	}
	return im.indexFilesSequential(files)
}

func (im *IndexManager) shouldIgnore(path string) bool {
	for _, pattern := range im.config.IgnorePatterns {
		match, err := filepath.Match(pattern, filepath.Base(path))
		if err == nil && match {
			return true
		}
		if strings.Contains(path, pattern) {
			return true
		}
	}
	return false
}

func (im *IndexManager) indexFilesSequential(files []string) error {
	for _, file := range files {
		if err := im.IndexFile(file); err != nil {
			log.Printf("AST index warning: %v", err)
		}
	}
	return nil
}

func (im *IndexManager) indexFilesParallel(files []string) error {
	workerCount := im.config.ParallelWorkers
	if workerCount <= 0 {
		workerCount = 2
	}
	var wg sync.WaitGroup
	fileCh := make(chan string)
	errCh := make(chan error, workerCount)
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range fileCh {
				if err := im.IndexFile(file); err != nil {
					errCh <- fmt.Errorf("%s: %w", file, err)
				}
			}
		}()
	}
	for _, file := range files {
		fileCh <- file
	}
	close(fileCh)
	wg.Wait()
	close(errCh)
	if len(errCh) > 0 {
		return <-errCh
	}
	return nil
}

func (im *IndexManager) indexWithSymbols(path, content, language string, category Category, contentHash string) error {
	im.mu.Lock()
	provider := im.symbolProvider
	im.mu.Unlock()
	if provider == nil {
		return fmt.Errorf("no parser for %s", language)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	symbols, err := provider.DocumentSymbols(ctx, path)
	if err != nil {
		return err
	}
	if len(symbols) == 0 {
		return fmt.Errorf("symbol provider returned no data for %s", path)
	}
	fileID := GenerateFileID(path)
	lines := strings.Count(content, "\n") + 1
	now := time.Now().UTC()
	rootType := NodeTypeDocument
	if category == CategoryCode {
		rootType = NodeTypePackage
	}
	root := &Node{
		ID:        fmt.Sprintf("%s:symbol-root", fileID),
		FileID:    fileID,
		Type:      rootType,
		Category:  category,
		Language:  language,
		Name:      filepath.Base(path),
		StartLine: 1,
		EndLine:   lines,
		CreatedAt: now,
		UpdatedAt: now,
	}
	nodes := []*Node{root}
	nodes = append(nodes, im.buildSymbolNodes(symbols, root.ID, fileID, category, language, now)...)
	result := &ParseResult{
		RootNode: root,
		Nodes:    nodes,
		Edges:    nil,
		Metadata: &FileMetadata{
			ID:            fileID,
			Path:          path,
			RelativePath:  filepath.Base(path),
			Language:      language,
			Category:      category,
			LineCount:     lines,
			TokenCount:    len(content),
			ContentHash:   contentHash,
			RootNodeID:    root.ID,
			NodeCount:     len(nodes),
			EdgeCount:     0,
			IndexedAt:     now,
			ParserVersion: "lsp_symbols",
		},
	}
	return im.persist(result, contentHash)
}

func (im *IndexManager) buildSymbolNodes(symbols []DocumentSymbol, parentID, fileID string, category Category, language string, timestamp time.Time) []*Node {
	var nodes []*Node
	for _, sym := range symbols {
		nodeType := sym.Kind
		if nodeType == "" {
			nodeType = NodeTypeSection
		}
		start := sym.StartLine
		if start <= 0 {
			start = 1
		}
		end := sym.EndLine
		if end < start {
			end = start
		}
		node := &Node{
			ID:        fmt.Sprintf("%s:symbol:%s:%d", fileID, sanitizeSymbolName(sym.Name), start),
			ParentID:  parentID,
			FileID:    fileID,
			Type:      nodeType,
			Category:  category,
			Language:  language,
			Name:      sym.Name,
			StartLine: start,
			EndLine:   end,
			CreatedAt: timestamp,
			UpdatedAt: timestamp,
		}
		nodes = append(nodes, node)
		if len(sym.Children) > 0 {
			nodes = append(nodes, im.buildSymbolNodes(sym.Children, node.ID, fileID, category, language, timestamp)...)
		}
	}
	return nodes
}

func sanitizeSymbolName(name string) string {
	if name == "" {
		return "symbol"
	}
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, ":", "_")
	return strings.ToLower(name)
}

func (im *IndexManager) persist(result *ParseResult, contentHash string) error {
	if result.Metadata == nil {
		return fmt.Errorf("parse result missing metadata")
	}
	if result.Metadata.ContentHash == "" {
		result.Metadata.ContentHash = contentHash
	}
	tx, err := im.store.BeginTransaction()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := im.store.SaveFile(result.Metadata); err != nil {
		return err
	}
	if err := tx.SaveNodes(result.Nodes); err != nil {
		return err
	}
	if err := tx.SaveEdges(result.Edges); err != nil {
		return err
	}
	return tx.Commit()
}

// QuerySymbol looks up nodes whose name matches the pattern.
func (im *IndexManager) QuerySymbol(pattern string) ([]*Node, error) {
	return im.store.SearchNodes(NodeQuery{
		NamePattern: pattern,
		Limit:       100,
	})
}

// SearchNodes routes to the underlying store.
func (im *IndexManager) SearchNodes(query NodeQuery) ([]*Node, error) {
	return im.store.SearchNodes(query)
}

// CallGraph summarizes direct callers/callees.
type CallGraph struct {
	Root    *Node
	Callees map[string][]*Node
	Callers map[string][]*Node
}

// GetCallGraph returns the call relationships for the identified symbol.
func (im *IndexManager) GetCallGraph(symbol string) (*CallGraph, error) {
	nodes, err := im.QuerySymbol(symbol)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("symbol not found: %s", symbol)
	}
	root := nodes[0]
	callees, err := im.store.GetCallees(root.ID)
	if err != nil {
		return nil, err
	}
	callers, err := im.store.GetCallers(root.ID)
	if err != nil {
		return nil, err
	}
	return &CallGraph{
		Root:    root,
		Callees: map[string][]*Node{root.ID: callees},
		Callers: map[string][]*Node{root.ID: callers},
	}, nil
}

// DependencyGraph expresses dependencies and dependents.
type DependencyGraph struct {
	Root         *Node
	Dependencies []*Node
	Dependents   []*Node
}

// GetDependencyGraph resolves dependencies for a symbol.
func (im *IndexManager) GetDependencyGraph(symbol string) (*DependencyGraph, error) {
	nodes, err := im.QuerySymbol(symbol)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("symbol not found: %s", symbol)
	}
	root := nodes[0]
	deps, err := im.store.GetDependencies(root.ID)
	if err != nil {
		return nil, err
	}
	dependents, err := im.store.GetDependents(root.ID)
	if err != nil {
		return nil, err
	}
	return &DependencyGraph{
		Root:         root,
		Dependencies: deps,
		Dependents:   dependents,
	}, nil
}

// Stats proxies store.GetStats for callers.
func (im *IndexManager) Stats() (*IndexStats, error) {
	return im.store.GetStats()
}

// LastIndexedAt fetches the timestamp recorded for a path, if any.
func (im *IndexManager) LastIndexedAt(path string) (time.Time, error) {
	file, err := im.store.GetFileByPath(path)
	if err != nil {
		return time.Time{}, err
	}
	if file == nil {
		return time.Time{}, nil
	}
	return file.IndexedAt, nil
}

// Store exposes the underlying IndexStore for advanced queries.
func (im *IndexManager) Store() IndexStore {
	return im.store
}
