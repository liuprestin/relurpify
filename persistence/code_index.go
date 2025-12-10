package persistence

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// IndexData is the persisted representation of the code index.
type IndexData struct {
	RootPath       string
	Version        string
	LastUpdate     time.Time
	Symbols        map[string][]framework.SymbolLocation
	Files          map[string]*framework.FileMetadata
	Dependencies   map[string][]string
	ReverseImports map[string][]string
	Chunks         map[string]*framework.CodeChunk
	ChunksByFile   map[string][]string
}

// CodeIndex coordinates parsing, indexing, and persistence.
type CodeIndex struct {
	mu        sync.RWMutex
	rootPath  string
	indexPath string
	data      *IndexData
}

// NewCodeIndex returns a ready-to-use indexer.
func NewCodeIndex(rootPath string, indexPath string) (*CodeIndex, error) {
	if rootPath == "" {
		return nil, fmt.Errorf("root path required")
	}
	if indexPath == "" {
		indexPath = filepath.Join(rootPath, ".relurpify", "code_index.json")
	}
	index := &CodeIndex{
		rootPath:  rootPath,
		indexPath: indexPath,
	}
	if err := index.load(); err != nil {
		return nil, err
	}
	return index, nil
}

func (ci *CodeIndex) load() error {
	data, err := os.ReadFile(ci.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			ci.data = ci.newIndexData()
			return nil
		}
		return err
	}
	if len(data) == 0 {
		ci.data = ci.newIndexData()
		return nil
	}
	var indexData IndexData
	if err := json.Unmarshal(data, &indexData); err != nil {
		return err
	}
	ci.data = &indexData
	return nil
}

// BuildIndex scans the entire repository to refresh metadata.
func (ci *CodeIndex) BuildIndex(ctx context.Context) error {
	data := ci.newIndexData()
	err := filepath.WalkDir(ci.rootPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".git") || strings.Contains(path, "/.relurpify") {
				return filepath.SkipDir
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if meta, chunks := ci.indexFile(path); meta != nil {
			rel, _ := filepath.Rel(ci.rootPath, path)
			meta.Path = rel
			data.Files[rel] = meta
			for _, chunk := range chunks {
				data.Chunks[chunk.ID] = chunk
				data.ChunksByFile[rel] = append(data.ChunksByFile[rel], chunk.ID)
			}
			for name, locs := range ci.extractSymbols(meta, chunks) {
				data.Symbols[name] = append(data.Symbols[name], locs...)
			}
			data.Dependencies[rel] = meta.Imports
			for _, imp := range meta.Imports {
				data.ReverseImports[imp] = append(data.ReverseImports[imp], rel)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	data.LastUpdate = time.Now().UTC()
	data.Version = fmt.Sprintf("%d", data.LastUpdate.Unix())
	ci.mu.Lock()
	ci.data = data
	ci.mu.Unlock()
	return nil
}

// UpdateIncremental refreshes the index for the provided files only.
func (ci *CodeIndex) UpdateIncremental(files []string) error {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	if ci.data == nil {
		ci.data = ci.newIndexData()
	}
	for _, file := range files {
		if meta, chunks := ci.indexFile(filepath.Join(ci.rootPath, file)); meta != nil {
			meta.Path = file
			ci.data.Files[file] = meta
			ci.data.ChunksByFile[file] = nil
			for _, chunk := range chunks {
				ci.data.Chunks[chunk.ID] = chunk
				ci.data.ChunksByFile[file] = append(ci.data.ChunksByFile[file], chunk.ID)
			}
		}
	}
	ci.data.LastUpdate = time.Now().UTC()
	return nil
}

// Save persists the index.
func (ci *CodeIndex) Save() error {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	if ci.data == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(ci.indexPath), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(ci.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ci.indexPath, payload, 0o644)
}

// Version returns the current index version identifier.
func (ci *CodeIndex) Version() string {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	if ci.data == nil {
		return ""
	}
	return ci.data.Version
}

// GetFileMetadata fetches metadata for a file.
func (ci *CodeIndex) GetFileMetadata(path string) (*framework.FileMetadata, bool) {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	meta, ok := ci.data.Files[path]
	return meta, ok
}

// ListFiles returns all indexed files.
func (ci *CodeIndex) ListFiles() []string {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	paths := make([]string, 0, len(ci.data.Files))
	for path := range ci.data.Files {
		paths = append(paths, path)
	}
	return paths
}

// GetSymbolsByName finds definitions for the provided symbol.
func (ci *CodeIndex) GetSymbolsByName(name string) ([]framework.SymbolLocation, error) {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	if ci.data == nil {
		return nil, fmt.Errorf("index not loaded")
	}
	return append([]framework.SymbolLocation(nil), ci.data.Symbols[name]...), nil
}

// GetSymbolDefinition returns the first matching definition.
func (ci *CodeIndex) GetSymbolDefinition(name string) (*framework.SymbolLocation, error) {
	locations, err := ci.GetSymbolsByName(name)
	if err != nil || len(locations) == 0 {
		return nil, err
	}
	return &locations[0], nil
}

// GetSymbolReferences returns all recorded references for a symbol.
func (ci *CodeIndex) GetSymbolReferences(name string) ([]framework.SymbolLocation, error) {
	return ci.GetSymbolsByName(name)
}

// GetFileDependencies returns the direct imports of a file.
func (ci *CodeIndex) GetFileDependencies(path string) []string {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	return append([]string(nil), ci.data.Dependencies[path]...)
}

// GetDependents returns files that import the provided file.
func (ci *CodeIndex) GetDependents(path string) []string {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	return append([]string(nil), ci.data.ReverseImports[path]...)
}

// GetChunksForFile retrieves all chunk IDs for the file.
func (ci *CodeIndex) GetChunksForFile(path string) []*framework.CodeChunk {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	ids := ci.data.ChunksByFile[path]
	chunks := make([]*framework.CodeChunk, 0, len(ids))
	for _, id := range ids {
		if chunk, ok := ci.data.Chunks[id]; ok {
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}

// GetChunkByID fetches a chunk by ID.
func (ci *CodeIndex) GetChunkByID(id string) (*framework.CodeChunk, bool) {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	chunk, ok := ci.data.Chunks[id]
	return chunk, ok
}

// FindChunksByName returns matching chunks using substring matching.
func (ci *CodeIndex) FindChunksByName(name string) []*framework.CodeChunk {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	results := make([]*framework.CodeChunk, 0)
	for _, chunk := range ci.data.Chunks {
		if strings.Contains(strings.ToLower(chunk.Name), strings.ToLower(name)) {
			results = append(results, chunk)
		}
	}
	return results
}

// FindChunksByFileAndRange returns chunks overlapping the provided range.
func (ci *CodeIndex) FindChunksByFileAndRange(path string, start, end int) []*framework.CodeChunk {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	results := make([]*framework.CodeChunk, 0)
	for _, id := range ci.data.ChunksByFile[path] {
		chunk := ci.data.Chunks[id]
		if chunk.StartLine <= end && chunk.EndLine >= start {
			results = append(results, chunk)
		}
	}
	return results
}

// SearchChunks performs a simple substring search across chunk previews.
func (ci *CodeIndex) SearchChunks(query string, limit int) []*framework.CodeChunk {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	results := make([]*framework.CodeChunk, 0, limit)
	query = strings.ToLower(query)
	for _, chunk := range ci.data.Chunks {
		if strings.Contains(strings.ToLower(chunk.Preview), query) {
			results = append(results, chunk)
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	return results
}

func (ci *CodeIndex) indexFile(path string) (*framework.FileMetadata, []*framework.CodeChunk) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	text := string(content)
	lines := strings.Split(text, "\n")
	loc := len(lines)
	language := inferLanguage(path)
	meta := &framework.FileMetadata{
		Path:         path,
		Language:     language,
		LOC:          loc,
		Size:         info.Size(),
		LastModified: info.ModTime().UTC(),
		Hash:         hashString(text),
		Summary:      summarize(text),
		LastIndexed:  time.Now().UTC(),
	}
	meta.Imports = extractImports(lines)
	chunk := &framework.CodeChunk{
		ID:         hashString(path),
		File:       path,
		Kind:       framework.ChunkBlock,
		Name:       filepath.Base(path),
		StartLine:  1,
		EndLine:    loc,
		Summary:    meta.Summary,
		TokenCount: len(text) / 4,
		Preview:    preview(text),
	}
	return meta, []*framework.CodeChunk{chunk}
}

func (ci *CodeIndex) extractSymbols(meta *framework.FileMetadata, chunks []*framework.CodeChunk) map[string][]framework.SymbolLocation {
	result := make(map[string][]framework.SymbolLocation)
	for _, chunk := range chunks {
		lines := strings.Split(chunk.Preview, "\n")
		for idx, line := range lines {
			if sym := parseSymbol(line, chunk.File, idx+chunk.StartLine); sym != nil {
				meta.Symbols = append(meta.Symbols, *sym)
				result[sym.Name] = append(result[sym.Name], framework.SymbolLocation{
					File:   sym.File,
					Line:   sym.Line,
					Column: sym.Column,
					Symbol: sym,
				})
			}
		}
	}
	return result
}

func parseSymbol(line, file string, lineNum int) *framework.Symbol {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "func "):
		name := strings.Fields(strings.TrimPrefix(trimmed, "func "))[0]
		return &framework.Symbol{Name: name, Kind: framework.SymbolFunction, File: file, Line: lineNum, Signature: trimmed}
	case strings.HasPrefix(trimmed, "type "):
		name := strings.Fields(strings.TrimPrefix(trimmed, "type "))[0]
		return &framework.Symbol{Name: name, Kind: framework.SymbolType, File: file, Line: lineNum, Signature: trimmed}
	case strings.HasPrefix(trimmed, "class "):
		name := strings.Fields(strings.TrimPrefix(trimmed, "class "))[0]
		return &framework.Symbol{Name: name, Kind: framework.SymbolClass, File: file, Line: lineNum, Signature: trimmed}
	}
	return nil
}

func hashString(input string) string {
	sum := sha1.Sum([]byte(input))
	return hex.EncodeToString(sum[:])
}

func (ci *CodeIndex) newIndexData() *IndexData {
	return &IndexData{
		RootPath:       ci.rootPath,
		Version:        "",
		Symbols:        make(map[string][]framework.SymbolLocation),
		Files:          make(map[string]*framework.FileMetadata),
		Dependencies:   make(map[string][]string),
		ReverseImports: make(map[string][]string),
		Chunks:         make(map[string]*framework.CodeChunk),
		ChunksByFile:   make(map[string][]string),
	}
}

func summarize(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return ""
	}
	if len(lines) == 1 {
		return strings.TrimSpace(lines[0])
	}
	return strings.TrimSpace(strings.Join(lines[:min(len(lines), 5)], " "))
}

func extractImports(lines []string) []string {
	imports := make([]string, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "import "):
			imports = append(imports, trimmed)
		case strings.HasPrefix(trimmed, "#include"):
			imports = append(imports, trimmed)
		}
	}
	return imports
}

func preview(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	return strings.Join(lines, "\n")
}

func inferLanguage(path string) string {
	switch filepath.Ext(path) {
	case ".go":
		return "go"
	case ".js", ".jsx", ".ts", ".tsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h", ".cc", ".cpp":
		return "c"
	default:
		return "text"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
