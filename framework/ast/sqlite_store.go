package ast

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteStore persists AST data in a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens/creates the database at dbPath.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, err
	}
	store := &SQLiteStore{db: db}
	if err := store.initSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		path TEXT NOT NULL UNIQUE,
		relative_path TEXT,
		language TEXT,
		category TEXT,
		line_count INTEGER,
		token_count INTEGER,
		complexity INTEGER,
		content_hash TEXT,
		root_node_id TEXT,
		node_count INTEGER,
		edge_count INTEGER,
		indexed_at TIMESTAMP,
		parser_version TEXT,
		summary TEXT,
		summary_hash TEXT
	);
	CREATE TABLE IF NOT EXISTS nodes (
		id TEXT PRIMARY KEY,
		parent_id TEXT,
		file_id TEXT NOT NULL,
		type TEXT NOT NULL,
		category TEXT,
		language TEXT,
		start_line INTEGER,
		end_line INTEGER,
		start_col INTEGER,
		end_col INTEGER,
		name TEXT,
		signature TEXT,
		doc_string TEXT,
		attributes TEXT,
		is_exported BOOLEAN,
		is_deprecated BOOLEAN,
		created_at TIMESTAMP,
		updated_at TIMESTAMP,
		content_hash TEXT,
		FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS edges (
		id TEXT PRIMARY KEY,
		source_id TEXT NOT NULL,
		target_id TEXT NOT NULL,
		type TEXT NOT NULL,
		attributes TEXT,
		FOREIGN KEY(source_id) REFERENCES nodes(id) ON DELETE CASCADE,
		FOREIGN KEY(target_id) REFERENCES nodes(id) ON DELETE CASCADE
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

// Close releases the underlying database handle.
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// SaveFile upserts metadata.
func (s *SQLiteStore) SaveFile(metadata *FileMetadata) error {
	if metadata == nil {
		return errors.New("metadata required")
	}
	query := `
	INSERT INTO files (
		id, path, relative_path, language, category, line_count, token_count,
		complexity, content_hash, root_node_id, node_count, edge_count,
		indexed_at, parser_version, summary, summary_hash
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		path=excluded.path,
		relative_path=excluded.relative_path,
		language=excluded.language,
		category=excluded.category,
		line_count=excluded.line_count,
		token_count=excluded.token_count,
		complexity=excluded.complexity,
		content_hash=excluded.content_hash,
		root_node_id=excluded.root_node_id,
		node_count=excluded.node_count,
		edge_count=excluded.edge_count,
		indexed_at=excluded.indexed_at,
		parser_version=excluded.parser_version,
		summary=excluded.summary,
		summary_hash=excluded.summary_hash
	`
	_, err := s.db.Exec(query,
		metadata.ID,
		metadata.Path,
		metadata.RelativePath,
		metadata.Language,
		metadata.Category,
		metadata.LineCount,
		metadata.TokenCount,
		metadata.Complexity,
		metadata.ContentHash,
		metadata.RootNodeID,
		metadata.NodeCount,
		metadata.EdgeCount,
		metadata.IndexedAt,
		metadata.ParserVersion,
		metadata.Summary,
		metadata.SummaryHash,
	)
	return err
}

func (s *SQLiteStore) GetFile(id string) (*FileMetadata, error) {
	row := s.db.QueryRow(`SELECT id, path, relative_path, language, category, line_count,
		token_count, complexity, content_hash, root_node_id, node_count, edge_count,
		indexed_at, parser_version, summary, summary_hash FROM files WHERE id = ?`, id)
	return scanFile(row)
}

func (s *SQLiteStore) GetFileByPath(path string) (*FileMetadata, error) {
	row := s.db.QueryRow(`SELECT id, path, relative_path, language, category, line_count,
		token_count, complexity, content_hash, root_node_id, node_count, edge_count,
		indexed_at, parser_version, summary, summary_hash FROM files WHERE path = ?`, path)
	return scanFile(row)
}

func (s *SQLiteStore) ListFiles(category Category) ([]*FileMetadata, error) {
	var rows *sql.Rows
	var err error
	if category == "" {
		rows, err = s.db.Query(`SELECT id, path, relative_path, language, category, line_count,
			token_count, complexity, content_hash, root_node_id, node_count, edge_count,
			indexed_at, parser_version, summary, summary_hash FROM files`)
	} else {
		rows, err = s.db.Query(`SELECT id, path, relative_path, language, category, line_count,
			token_count, complexity, content_hash, root_node_id, node_count, edge_count,
			indexed_at, parser_version, summary, summary_hash FROM files WHERE category = ?`, category)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFiles(rows)
}

func (s *SQLiteStore) DeleteFile(id string) error {
	_, err := s.db.Exec(`DELETE FROM files WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) SaveNodes(nodes []*Node) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := insertNodes(tx, nodes); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func insertNodes(tx *sql.Tx, nodes []*Node) error {
	if len(nodes) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO nodes (
		id, parent_id, file_id, type, category, language,
		start_line, end_line, start_col, end_col, name, signature,
		doc_string, attributes, is_exported, is_deprecated,
		created_at, updated_at, content_hash
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, node := range nodes {
		if node == nil {
			continue
		}
		attrJSON, _ := json.Marshal(node.Attributes)
		if node.CreatedAt.IsZero() {
			node.CreatedAt = time.Now().UTC()
		}
		if node.UpdatedAt.IsZero() {
			node.UpdatedAt = node.CreatedAt
		}
		if _, err := stmt.Exec(
			node.ID,
			node.ParentID,
			node.FileID,
			node.Type,
			node.Category,
			node.Language,
			node.StartLine,
			node.EndLine,
			node.StartCol,
			node.EndCol,
			node.Name,
			node.Signature,
			node.DocString,
			string(attrJSON),
			node.IsExported,
			node.IsDeprecated,
			node.CreatedAt,
			node.UpdatedAt,
			node.ContentHash,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) GetNode(id string) (*Node, error) {
	row := s.db.QueryRow(`SELECT id, parent_id, file_id, type, category, language,
		start_line, end_line, start_col, end_col, name, signature, doc_string,
		attributes, is_exported, is_deprecated, created_at, updated_at, content_hash
		FROM nodes WHERE id = ?`, id)
	return scanNode(row)
}

func (s *SQLiteStore) GetNodesByFile(fileID string) ([]*Node, error) {
	rows, err := s.db.Query(`SELECT id, parent_id, file_id, type, category, language,
		start_line, end_line, start_col, end_col, name, signature, doc_string,
		attributes, is_exported, is_deprecated, created_at, updated_at, content_hash
		FROM nodes WHERE file_id = ?`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (s *SQLiteStore) GetNodesByType(nodeType NodeType) ([]*Node, error) {
	rows, err := s.db.Query(`SELECT id, parent_id, file_id, type, category, language,
		start_line, end_line, start_col, end_col, name, signature, doc_string,
		attributes, is_exported, is_deprecated, created_at, updated_at, content_hash
		FROM nodes WHERE type = ?`, nodeType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (s *SQLiteStore) GetNodesByName(name string) ([]*Node, error) {
	rows, err := s.db.Query(`SELECT id, parent_id, file_id, type, category, language,
		start_line, end_line, start_col, end_col, name, signature, doc_string,
		attributes, is_exported, is_deprecated, created_at, updated_at, content_hash
		FROM nodes WHERE name = ?`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (s *SQLiteStore) SearchNodes(query NodeQuery) ([]*Node, error) {
	builder := strings.Builder{}
	args := make([]interface{}, 0)
	builder.WriteString(`SELECT id, parent_id, file_id, type, category, language,
		start_line, end_line, start_col, end_col, name, signature, doc_string,
		attributes, is_exported, is_deprecated, created_at, updated_at, content_hash
		FROM nodes WHERE 1=1`)
	if len(query.Types) > 0 {
		builder.WriteString(" AND type IN (")
		builder.WriteString(placeholders(len(query.Types)))
		builder.WriteString(")")
		for _, t := range query.Types {
			args = append(args, t)
		}
	}
	if len(query.Categories) > 0 {
		builder.WriteString(" AND category IN (")
		builder.WriteString(placeholders(len(query.Categories)))
		builder.WriteString(")")
		for _, c := range query.Categories {
			args = append(args, c)
		}
	}
	if len(query.Languages) > 0 {
		builder.WriteString(" AND language IN (")
		builder.WriteString(placeholders(len(query.Languages)))
		builder.WriteString(")")
		for _, l := range query.Languages {
			args = append(args, l)
		}
	}
	if len(query.FileIDs) > 0 {
		builder.WriteString(" AND file_id IN (")
		builder.WriteString(placeholders(len(query.FileIDs)))
		builder.WriteString(")")
		for _, id := range query.FileIDs {
			args = append(args, id)
		}
	}
	if query.NamePattern != "" {
		builder.WriteString(" AND name LIKE ?")
		args = append(args, query.NamePattern)
	}
	if query.IsExported != nil {
		builder.WriteString(" AND is_exported = ?")
		args = append(args, *query.IsExported)
	}
	if query.Limit > 0 {
		builder.WriteString(fmt.Sprintf(" LIMIT %d", query.Limit))
	}
	if query.Offset > 0 {
		builder.WriteString(fmt.Sprintf(" OFFSET %d", query.Offset))
	}
	rows, err := s.db.Query(builder.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (s *SQLiteStore) DeleteNode(id string) error {
	_, err := s.db.Exec(`DELETE FROM nodes WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) SaveEdges(edges []*Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := insertEdges(tx, edges); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func insertEdges(tx *sql.Tx, edges []*Edge) error {
	if len(edges) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO edges (id, source_id, target_id, type, attributes)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, edge := range edges {
		if edge == nil {
			continue
		}
		attrJSON, _ := json.Marshal(edge.Attributes)
		if _, err := stmt.Exec(edge.ID, edge.SourceID, edge.TargetID, edge.Type, string(attrJSON)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) GetEdge(id string) (*Edge, error) {
	row := s.db.QueryRow(`SELECT id, source_id, target_id, type, attributes FROM edges WHERE id = ?`, id)
	return scanEdge(row)
}

func (s *SQLiteStore) GetEdgesBySource(sourceID string) ([]*Edge, error) {
	rows, err := s.db.Query(`SELECT id, source_id, target_id, type, attributes FROM edges WHERE source_id = ?`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *SQLiteStore) GetEdgesByTarget(targetID string) ([]*Edge, error) {
	rows, err := s.db.Query(`SELECT id, source_id, target_id, type, attributes FROM edges WHERE target_id = ?`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *SQLiteStore) GetEdgesByType(edgeType EdgeType) ([]*Edge, error) {
	rows, err := s.db.Query(`SELECT id, source_id, target_id, type, attributes FROM edges WHERE type = ?`, edgeType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *SQLiteStore) SearchEdges(query EdgeQuery) ([]*Edge, error) {
	builder := strings.Builder{}
	args := make([]interface{}, 0)
	builder.WriteString(`SELECT id, source_id, target_id, type, attributes FROM edges WHERE 1=1`)
	if len(query.Types) > 0 {
		builder.WriteString(" AND type IN (")
		builder.WriteString(placeholders(len(query.Types)))
		builder.WriteString(")")
		for _, t := range query.Types {
			args = append(args, t)
		}
	}
	if len(query.SourceIDs) > 0 {
		builder.WriteString(" AND source_id IN (")
		builder.WriteString(placeholders(len(query.SourceIDs)))
		builder.WriteString(")")
		for _, id := range query.SourceIDs {
			args = append(args, id)
		}
	}
	if len(query.TargetIDs) > 0 {
		builder.WriteString(" AND target_id IN (")
		builder.WriteString(placeholders(len(query.TargetIDs)))
		builder.WriteString(")")
		for _, id := range query.TargetIDs {
			args = append(args, id)
		}
	}
	if query.Limit > 0 {
		builder.WriteString(fmt.Sprintf(" LIMIT %d", query.Limit))
	}
	if query.Offset > 0 {
		builder.WriteString(fmt.Sprintf(" OFFSET %d", query.Offset))
	}
	rows, err := s.db.Query(builder.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *SQLiteStore) DeleteEdge(id string) error {
	_, err := s.db.Exec(`DELETE FROM edges WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) GetCallees(nodeID string) ([]*Node, error) {
	return s.getRelatedNodes(nodeID, EdgeTypeCalls, true)
}

func (s *SQLiteStore) GetCallers(nodeID string) ([]*Node, error) {
	return s.getRelatedNodes(nodeID, EdgeTypeCalls, false)
}

func (s *SQLiteStore) GetImports(nodeID string) ([]*Node, error) {
	return s.getRelatedNodes(nodeID, EdgeTypeImports, true)
}

func (s *SQLiteStore) GetImportedBy(nodeID string) ([]*Node, error) {
	return s.getRelatedNodes(nodeID, EdgeTypeImports, false)
}

func (s *SQLiteStore) GetReferences(nodeID string) ([]*Node, error) {
	return s.getRelatedNodes(nodeID, EdgeTypeReferences, true)
}

func (s *SQLiteStore) GetReferencedBy(nodeID string) ([]*Node, error) {
	return s.getRelatedNodes(nodeID, EdgeTypeReferences, false)
}

func (s *SQLiteStore) getRelatedNodes(nodeID string, edgeType EdgeType, outgoing bool) ([]*Node, error) {
	var query string
	if outgoing {
		query = `SELECT n.id, n.parent_id, n.file_id, n.type, n.category, n.language,
			n.start_line, n.end_line, n.start_col, n.end_col, n.name, n.signature,
			n.doc_string, n.attributes, n.is_exported, n.is_deprecated,
			n.created_at, n.updated_at, n.content_hash
			FROM nodes n
			INNER JOIN edges e ON e.target_id = n.id
			WHERE e.source_id = ? AND e.type = ?`
	} else {
		query = `SELECT n.id, n.parent_id, n.file_id, n.type, n.category, n.language,
			n.start_line, n.end_line, n.start_col, n.end_col, n.name, n.signature,
			n.doc_string, n.attributes, n.is_exported, n.is_deprecated,
			n.created_at, n.updated_at, n.content_hash
			FROM nodes n
			INNER JOIN edges e ON e.source_id = n.id
			WHERE e.target_id = ? AND e.type = ?`
	}
	rows, err := s.db.Query(query, nodeID, edgeType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (s *SQLiteStore) GetDependencies(nodeID string) ([]*Node, error) {
	query := `
	WITH RECURSIVE deps(id) AS (
		SELECT target_id FROM edges WHERE source_id = ? AND type IN ('imports', 'depends_on', 'references')
		UNION
		SELECT edges.target_id FROM edges
		INNER JOIN deps ON edges.source_id = deps.id
		WHERE edges.type IN ('imports', 'depends_on', 'references')
	)
	SELECT n.id, n.parent_id, n.file_id, n.type, n.category, n.language,
		n.start_line, n.end_line, n.start_col, n.end_col, n.name, n.signature,
		n.doc_string, n.attributes, n.is_exported, n.is_deprecated,
		n.created_at, n.updated_at, n.content_hash
	FROM nodes n
	INNER JOIN deps d ON n.id = d.id
	`
	rows, err := s.db.Query(query, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (s *SQLiteStore) GetDependents(nodeID string) ([]*Node, error) {
	query := `
	WITH RECURSIVE dependents(id) AS (
		SELECT source_id FROM edges WHERE target_id = ? AND type IN ('imports', 'depends_on', 'references')
		UNION
		SELECT edges.source_id FROM edges
		INNER JOIN dependents ON edges.target_id = dependents.id
		WHERE edges.type IN ('imports', 'depends_on', 'references')
	)
	SELECT n.id, n.parent_id, n.file_id, n.type, n.category, n.language,
		n.start_line, n.end_line, n.start_col, n.end_col, n.name, n.signature,
		n.doc_string, n.attributes, n.is_exported, n.is_deprecated,
		n.created_at, n.updated_at, n.content_hash
	FROM nodes n
	INNER JOIN dependents d ON n.id = d.id
	`
	rows, err := s.db.Query(query, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// BeginTransaction starts a batch operation.
func (s *SQLiteStore) BeginTransaction() (Transaction, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	return &sqliteTx{tx: tx}, nil
}

type sqliteTx struct {
	tx *sql.Tx
}

func (t *sqliteTx) SaveNodes(nodes []*Node) error {
	return insertNodes(t.tx, nodes)
}

func (t *sqliteTx) SaveEdges(edges []*Edge) error {
	return insertEdges(t.tx, edges)
}

func (t *sqliteTx) DeleteFile(fileID string) error {
	_, err := t.tx.Exec(`DELETE FROM files WHERE id = ?`, fileID)
	return err
}

func (t *sqliteTx) Commit() error {
	return t.tx.Commit()
}

func (t *sqliteTx) Rollback() error {
	return t.tx.Rollback()
}

// Vacuum performs database maintenance.
func (s *SQLiteStore) Vacuum() error {
	_, err := s.db.Exec(`VACUUM`)
	return err
}

// GetStats aggregates counts.
func (s *SQLiteStore) GetStats() (*IndexStats, error) {
	stats := &IndexStats{
		NodesByType:     make(map[NodeType]int),
		EdgesByType:     make(map[EdgeType]int),
		FilesByCategory: make(map[Category]int),
		LastVacuum:      time.Time{},
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&stats.TotalFiles)
	s.db.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&stats.TotalNodes)
	s.db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&stats.TotalEdges)
	rows, err := s.db.Query(`SELECT type, COUNT(*) FROM nodes GROUP BY type`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var t NodeType
			var count int
			rows.Scan(&t, &count)
			stats.NodesByType[t] = count
		}
	}
	rows, err = s.db.Query(`SELECT type, COUNT(*) FROM edges GROUP BY type`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var t EdgeType
			var count int
			rows.Scan(&t, &count)
			stats.EdgesByType[t] = count
		}
	}
	rows, err = s.db.Query(`SELECT category, COUNT(*) FROM files GROUP BY category`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var c Category
			var count int
			rows.Scan(&c, &count)
			stats.FilesByCategory[c] = count
		}
	}
	var pageCount, pageSize int
	s.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount)
	s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
	stats.DatabaseSize = int64(pageCount * pageSize)
	return stats, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

func scanFile(row *sql.Row) (*FileMetadata, error) {
	meta := &FileMetadata{}
	err := row.Scan(
		&meta.ID,
		&meta.Path,
		&meta.RelativePath,
		&meta.Language,
		&meta.Category,
		&meta.LineCount,
		&meta.TokenCount,
		&meta.Complexity,
		&meta.ContentHash,
		&meta.RootNodeID,
		&meta.NodeCount,
		&meta.EdgeCount,
		&meta.IndexedAt,
		&meta.ParserVersion,
		&meta.Summary,
		&meta.SummaryHash,
	)
	if err != nil {
		return nil, err
	}
	return meta, nil
}

func scanFiles(rows *sql.Rows) ([]*FileMetadata, error) {
	results := make([]*FileMetadata, 0)
	for rows.Next() {
		meta := &FileMetadata{}
		err := rows.Scan(
			&meta.ID,
			&meta.Path,
			&meta.RelativePath,
			&meta.Language,
			&meta.Category,
			&meta.LineCount,
			&meta.TokenCount,
			&meta.Complexity,
			&meta.ContentHash,
			&meta.RootNodeID,
			&meta.NodeCount,
			&meta.EdgeCount,
			&meta.IndexedAt,
			&meta.ParserVersion,
			&meta.Summary,
			&meta.SummaryHash,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, meta)
	}
	return results, rows.Err()
}

func scanNode(row *sql.Row) (*Node, error) {
	node := &Node{}
	var attrJSON string
	err := row.Scan(
		&node.ID,
		&node.ParentID,
		&node.FileID,
		&node.Type,
		&node.Category,
		&node.Language,
		&node.StartLine,
		&node.EndLine,
		&node.StartCol,
		&node.EndCol,
		&node.Name,
		&node.Signature,
		&node.DocString,
		&attrJSON,
		&node.IsExported,
		&node.IsDeprecated,
		&node.CreatedAt,
		&node.UpdatedAt,
		&node.ContentHash,
	)
	if err != nil {
		return nil, err
	}
	if attrJSON != "" {
		json.Unmarshal([]byte(attrJSON), &node.Attributes)
	}
	return node, nil
}

func scanNodes(rows *sql.Rows) ([]*Node, error) {
	results := make([]*Node, 0)
	for rows.Next() {
		node := &Node{}
		var attrJSON string
		err := rows.Scan(
			&node.ID,
			&node.ParentID,
			&node.FileID,
			&node.Type,
			&node.Category,
			&node.Language,
			&node.StartLine,
			&node.EndLine,
			&node.StartCol,
			&node.EndCol,
			&node.Name,
			&node.Signature,
			&node.DocString,
			&attrJSON,
			&node.IsExported,
			&node.IsDeprecated,
			&node.CreatedAt,
			&node.UpdatedAt,
			&node.ContentHash,
		)
		if err != nil {
			return nil, err
		}
		if attrJSON != "" {
			json.Unmarshal([]byte(attrJSON), &node.Attributes)
		}
		results = append(results, node)
	}
	return results, rows.Err()
}

func scanEdge(row *sql.Row) (*Edge, error) {
	edge := &Edge{}
	var attrJSON string
	err := row.Scan(&edge.ID, &edge.SourceID, &edge.TargetID, &edge.Type, &attrJSON)
	if err != nil {
		return nil, err
	}
	if attrJSON != "" {
		json.Unmarshal([]byte(attrJSON), &edge.Attributes)
	}
	return edge, nil
}

func scanEdges(rows *sql.Rows) ([]*Edge, error) {
	results := make([]*Edge, 0)
	for rows.Next() {
		edge := &Edge{}
		var attrJSON string
		err := rows.Scan(&edge.ID, &edge.SourceID, &edge.TargetID, &edge.Type, &attrJSON)
		if err != nil {
			return nil, err
		}
		if attrJSON != "" {
			json.Unmarshal([]byte(attrJSON), &edge.Attributes)
		}
		results = append(results, edge)
	}
	return results, rows.Err()
}
