package framework

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"time"
)

// SymbolKind enumerates basic symbol categories.
type SymbolKind string

const (
	SymbolFunction SymbolKind = "function"
	SymbolMethod   SymbolKind = "method"
	SymbolClass    SymbolKind = "class"
	SymbolType     SymbolKind = "type"
	SymbolVariable SymbolKind = "variable"
)

// Symbol records a definition discovered while indexing.
type Symbol struct {
	Name      string
	Kind      SymbolKind
	File      string
	Line      int
	Column    int
	Signature string
	DocString string
	Scope     string
}

// SymbolLocation maps a name to specific coordinates.
type SymbolLocation struct {
	File   string
	Line   int
	Column int
	Symbol *Symbol
}

// FileMetadata keeps track of stats and summary.
type FileMetadata struct {
	Path         string
	Language     string
	LOC          int
	Size         int64
	LastModified time.Time
	Hash         string
	Symbols      []Symbol
	Imports      []string
	Complexity   int
	Summary      string
	LastIndexed  time.Time
}

// ChunkKind enumerates the chunk type.
type ChunkKind string

const (
	ChunkFunction ChunkKind = "function"
	ChunkMethod   ChunkKind = "method"
	ChunkClass    ChunkKind = "class"
	ChunkBlock    ChunkKind = "block"
)

// CodeChunk stores snippet metadata used by search/context builders.
type CodeChunk struct {
	ID           string
	File         string
	Kind         ChunkKind
	Name         string
	StartLine    int
	EndLine      int
	Summary      string
	TokenCount   int
	Dependencies []string
	Preview      string
}

// Hash returns a stable identifier suitable for caches.
func (c *CodeChunk) Hash() string {
	h := sha1.Sum([]byte(c.File + c.Name + fmt.Sprint(c.StartLine, c.EndLine)))
	return hex.EncodeToString(h[:])
}

// CodeIndex defines the capabilities required by planners/context builders.
type CodeIndex interface {
	GetFileMetadata(path string) (*FileMetadata, bool)
	ListFiles() []string
	GetSymbolsByName(name string) ([]SymbolLocation, error)
	GetSymbolDefinition(name string) (*SymbolLocation, error)
	GetSymbolReferences(name string) ([]SymbolLocation, error)
	GetFileDependencies(path string) []string
	GetDependents(path string) []string
	GetChunksForFile(path string) []*CodeChunk
	GetChunkByID(id string) (*CodeChunk, bool)
	FindChunksByName(name string) []*CodeChunk
	FindChunksByFileAndRange(path string, start, end int) []*CodeChunk
	SearchChunks(query string, limit int) []*CodeChunk
}
