package ast

import "time"

// IndexStore persists AST entities.
type IndexStore interface {
	SaveFile(metadata *FileMetadata) error
	GetFile(fileID string) (*FileMetadata, error)
	GetFileByPath(path string) (*FileMetadata, error)
	ListFiles(category Category) ([]*FileMetadata, error)
	DeleteFile(fileID string) error
	SaveNodes(nodes []*Node) error
	GetNode(nodeID string) (*Node, error)
	GetNodesByFile(fileID string) ([]*Node, error)
	GetNodesByType(nodeType NodeType) ([]*Node, error)
	GetNodesByName(name string) ([]*Node, error)
	SearchNodes(query NodeQuery) ([]*Node, error)
	DeleteNode(nodeID string) error
	SaveEdges(edges []*Edge) error
	GetEdge(edgeID string) (*Edge, error)
	GetEdgesBySource(sourceID string) ([]*Edge, error)
	GetEdgesByTarget(targetID string) ([]*Edge, error)
	GetEdgesByType(edgeType EdgeType) ([]*Edge, error)
	SearchEdges(query EdgeQuery) ([]*Edge, error)
	DeleteEdge(edgeID string) error
	GetCallees(nodeID string) ([]*Node, error)
	GetCallers(nodeID string) ([]*Node, error)
	GetImports(nodeID string) ([]*Node, error)
	GetImportedBy(nodeID string) ([]*Node, error)
	GetReferences(nodeID string) ([]*Node, error)
	GetReferencedBy(nodeID string) ([]*Node, error)
	GetDependencies(nodeID string) ([]*Node, error)
	GetDependents(nodeID string) ([]*Node, error)
	BeginTransaction() (Transaction, error)
	Vacuum() error
	GetStats() (*IndexStats, error)
}

// NodeQuery filters nodes.
type NodeQuery struct {
	Types       []NodeType
	Categories  []Category
	Languages   []string
	NamePattern string
	FileIDs     []string
	IsExported  *bool
	Limit       int
	Offset      int
}

// EdgeQuery filters edges.
type EdgeQuery struct {
	Types     []EdgeType
	SourceIDs []string
	TargetIDs []string
	Limit     int
	Offset    int
}

// Transaction abstracts batched operations.
type Transaction interface {
	SaveNodes(nodes []*Node) error
	SaveEdges(edges []*Edge) error
	DeleteFile(fileID string) error
	Commit() error
	Rollback() error
}

// IndexStats exposes counts.
type IndexStats struct {
	TotalFiles      int
	TotalNodes      int
	TotalEdges      int
	NodesByType     map[NodeType]int
	EdgesByType     map[EdgeType]int
	FilesByCategory map[Category]int
	DatabaseSize    int64
	LastVacuum      time.Time
}
