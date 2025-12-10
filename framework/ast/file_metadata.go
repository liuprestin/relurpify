package ast

import "time"

// FileMetadata stores per-file statistics and indexing metadata.
type FileMetadata struct {
	ID            string    `json:"id"`
	Path          string    `json:"path"`
	RelativePath  string    `json:"relative_path"`
	Language      string    `json:"language"`
	Category      Category  `json:"category"`
	LineCount     int       `json:"line_count"`
	TokenCount    int       `json:"token_count"`
	Complexity    int       `json:"complexity"`
	ContentHash   string    `json:"content_hash"`
	RootNodeID    string    `json:"root_node_id"`
	NodeCount     int       `json:"node_count"`
	EdgeCount     int       `json:"edge_count"`
	IndexedAt     time.Time `json:"indexed_at"`
	ParserVersion string    `json:"parser_version"`
	Summary       string    `json:"summary"`
	SummaryHash   string    `json:"summary_hash"`
}
