package contextual

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/framework/ast"
	"github.com/lexcodex/relurpify/tools"
)

// ProgressiveLoader manages incremental context loading.
type ProgressiveLoader struct {
	contextManager *framework.ContextManager
	indexManager   *ast.IndexManager
	searchEngine   *framework.SearchEngine
	summarizer     framework.Summarizer
	budget         *framework.ContextBudget

	loadHistory []ContextLoadEvent
	loadedFiles map[string]DetailLevel
}

// NewProgressiveLoader builds a loader with optional helpers.
func NewProgressiveLoader(
	contextManager *framework.ContextManager,
	indexManager *ast.IndexManager,
	searchEngine *framework.SearchEngine,
	budget *framework.ContextBudget,
	summarizer framework.Summarizer,
) *ProgressiveLoader {
	return &ProgressiveLoader{
		contextManager: contextManager,
		indexManager:   indexManager,
		searchEngine:   searchEngine,
		budget:         budget,
		summarizer:     summarizer,
		loadHistory:    make([]ContextLoadEvent, 0),
		loadedFiles:    make(map[string]DetailLevel),
	}
}

// InitialLoad executes the strategy's first context request.
func (pl *ProgressiveLoader) InitialLoad(task *framework.Task, strategy ContextStrategy) error {
	if pl == nil || strategy == nil {
		return fmt.Errorf("progressive loader not initialized")
	}
	request, err := strategy.SelectContext(task, pl.budget)
	if err != nil {
		return fmt.Errorf("select context: %w", err)
	}
	return pl.ExecuteContextRequest(request, "initial")
}

// ExecuteContextRequest loads the requested artifacts.
func (pl *ProgressiveLoader) ExecuteContextRequest(request *ContextRequest, trigger string) error {
	if request == nil {
		return nil
	}
	event := ContextLoadEvent{
		Timestamp: time.Now(),
		Trigger:   trigger,
		Success:   true,
	}
	for _, fileReq := range request.Files {
		if err := pl.loadFile(fileReq); err != nil {
			event.Success = false
			event.Reason = err.Error()
			continue
		}
		event.ItemsLoaded++
	}
	for _, astQuery := range request.ASTQueries {
		if err := pl.executeASTQuery(astQuery); err != nil {
			event.Success = false
			event.Reason = err.Error()
			continue
		}
		event.ItemsLoaded++
	}
	for _, searchQuery := range request.SearchQueries {
		if err := pl.executeSearchQuery(searchQuery); err != nil {
			event.Success = false
			event.Reason = err.Error()
			continue
		}
		event.ItemsLoaded++
	}
	for _, memoryQuery := range request.MemoryQueries {
		if err := pl.executeMemoryQuery(memoryQuery); err != nil {
			event.Success = false
			event.Reason = err.Error()
			continue
		}
		event.ItemsLoaded++
	}
	pl.loadHistory = append(pl.loadHistory, event)
	return nil
}

// ExpandContext increases the detail for a file.
func (pl *ProgressiveLoader) ExpandContext(path string, level DetailLevel) error {
	if level < DetailSignatureOnly {
		level = DetailSignatureOnly
	}
	if existing, ok := pl.loadedFiles[path]; ok && existing >= level {
		return nil
	}
	return pl.loadFile(FileRequest{
		Path:        path,
		DetailLevel: level,
		Priority:    0,
	})
}

// DrillDown loads full content for a file.
func (pl *ProgressiveLoader) DrillDown(path string) error {
	return pl.ExpandContext(path, DetailFull)
}

// LoadRelatedFiles fetches dependencies for the target file.
func (pl *ProgressiveLoader) LoadRelatedFiles(path string, depth int) error {
	if pl.indexManager == nil || depth <= 0 {
		return nil
	}
	nodes, err := pl.indexManager.QuerySymbol(filepath.Base(path))
	if err != nil || len(nodes) == 0 {
		return err
	}
	deps, err := pl.indexManager.Store().GetDependencies(nodes[0].ID)
	if err != nil {
		return err
	}
	for _, dep := range deps {
		if dep == nil || dep.FileID == "" {
			continue
		}
		fileMeta, err := pl.indexManager.Store().GetFile(dep.FileID)
		if err != nil || fileMeta == nil {
			continue
		}
		_ = pl.loadFile(FileRequest{
			Path:        fileMeta.Path,
			DetailLevel: DetailConcise,
			Priority:    1,
		})
	}
	return nil
}

func (pl *ProgressiveLoader) loadFile(req FileRequest) error {
	if pl.contextManager == nil {
		return fmt.Errorf("context manager unavailable")
	}
	if req.Path == "" {
		return fmt.Errorf("file path required")
	}
	content, err := ReadFile(req.Path)
	if err != nil {
		return err
	}
	processed := pl.applyDetailLevel(content, req.Path, req.DetailLevel)
	item := &framework.FileContextItem{
		Path:         req.Path,
		Content:      processed,
		LastAccessed: time.Now(),
		Relevance:    1.0,
		PriorityVal:  req.Priority,
		Pinned:       req.Pinned,
	}
	if err := pl.contextManager.AddItem(item); err != nil {
		return fmt.Errorf("add file to context: %w", err)
	}
	pl.loadedFiles[req.Path] = req.DetailLevel
	return nil
}

func (pl *ProgressiveLoader) applyDetailLevel(content, path string, level DetailLevel) string {
	switch level {
	case DetailFull:
		return content
	case DetailDetailed:
		if data := pl.formatDetailed(path); data != "" {
			return data
		}
		return content
	case DetailConcise:
		if summary := pl.fileSummary(path); summary != "" {
			return summary
		}
		if len(content) > 500 {
			return content[:500] + "..."
		}
		return content
	case DetailMinimal:
		if summary := pl.fileStats(path); summary != "" {
			return summary
		}
		return filepath.Base(path)
	case DetailSignatureOnly:
		if data := pl.formatSignaturesOnly(path); data != "" {
			return data
		}
		return filepath.Base(path)
	default:
		return content
	}
}

func (pl *ProgressiveLoader) formatDetailed(path string) string {
	nodes := pl.nodesForFile(path)
	if len(nodes) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if node.Type == ast.NodeTypeFunction || node.Type == ast.NodeTypeMethod {
			sb.WriteString(node.Signature)
			sb.WriteString("\n")
			if node.DocString != "" {
				sb.WriteString("  // ")
				sb.WriteString(node.DocString)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (pl *ProgressiveLoader) formatSignaturesOnly(path string) string {
	nodes := pl.nodesForFile(path)
	if len(nodes) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, node := range nodes {
		if node == nil {
			continue
		}
		switch node.Type {
		case ast.NodeTypeFunction, ast.NodeTypeMethod, ast.NodeTypeClass:
			sb.WriteString("- ")
			sb.WriteString(node.Name)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (pl *ProgressiveLoader) nodesForFile(path string) []*ast.Node {
	if pl.indexManager == nil {
		return nil
	}
	meta, err := pl.indexManager.Store().GetFileByPath(path)
	if err != nil || meta == nil {
		return nil
	}
	nodes, err := pl.indexManager.Store().GetNodesByFile(meta.ID)
	if err != nil {
		return nil
	}
	return nodes
}

func (pl *ProgressiveLoader) fileSummary(path string) string {
	if pl.indexManager == nil {
		return ""
	}
	meta, err := pl.indexManager.Store().GetFileByPath(path)
	if err != nil || meta == nil {
		return ""
	}
	if meta.Summary != "" {
		return meta.Summary
	}
	if pl.summarizer != nil {
		if content, err := ReadFile(path); err == nil {
			if summary, err := pl.summarizer.Summarize(content, framework.SummaryConcise); err == nil {
				return summary
			}
		}
	}
	return ""
}

func (pl *ProgressiveLoader) fileStats(path string) string {
	if pl.indexManager == nil {
		return ""
	}
	meta, err := pl.indexManager.Store().GetFileByPath(path)
	if err != nil || meta == nil {
		return ""
	}
	return fmt.Sprintf("%s: %d lines, %d tokens", filepath.Base(path), meta.LineCount, meta.TokenCount)
}

func (pl *ProgressiveLoader) executeASTQuery(query ASTQuery) error {
	if pl.indexManager == nil {
		return fmt.Errorf("ast index unavailable")
	}
	tool := tools.NewASTTool(pl.indexManager)
	if tool == nil {
		return fmt.Errorf("cannot create ast tool")
	}
	params := map[string]interface{}{
		"action": string(query.Type),
		"symbol": query.Symbol,
	}
	if len(query.Filter.Types) > 0 {
		params["type"] = string(query.Filter.Types[0])
	}
	if len(query.Filter.Categories) > 0 {
		params["category"] = string(query.Filter.Categories[0])
	}
	if query.Filter.ExportedOnly {
		params["exported_only"] = true
	}
	result, err := tool.Execute(context.Background(), framework.NewContext(), params)
	if err != nil {
		return err
	}
	item := &framework.ToolResultContextItem{
		ToolName:     "query_ast",
		Result:       result,
		LastAccessed: time.Now(),
		Relevance:    0.8,
		PriorityVal:  1,
	}
	if pl.contextManager != nil {
		return pl.contextManager.AddItem(item)
	}
	return nil
}

func (pl *ProgressiveLoader) executeSearchQuery(query SearchQuery) error {
	if pl.searchEngine == nil {
		return nil
	}
	searchQuery := framework.SearchQuery{
		Text:         query.Query,
		Mode:         query.Mode,
		FilePatterns: query.FilePatterns,
		MaxResults:   query.MaxResults,
	}
	results, err := pl.searchEngine.Search(searchQuery)
	if err != nil {
		return err
	}
	for i, result := range results {
		if i >= query.MaxResults {
			break
		}
		_ = pl.loadFile(FileRequest{
			Path:        result.File,
			DetailLevel: DetailConcise,
			Priority:    1,
		})
	}
	return nil
}

func (pl *ProgressiveLoader) executeMemoryQuery(query MemoryQuery) error {
	// Hook memory subsystem when available.
	_ = query
	return nil
}
