package framework

import (
	"fmt"
	"os"
	"path/filepath"
)

// FileLocation identifies a range inside a file.
type FileLocation struct {
	Path      string
	StartLine int
	EndLine   int
}

// FileChunk represents a snippet ready to be inserted into a prompt.
type FileChunk struct {
	Path     string
	Language string
	Region   FileLocation
	Content  string
	Kind     string
}

// SummaryChunk captures summarized context (files, directories, history, etc.).
type SummaryChunk struct {
	Kind    string
	ID      string
	Level   SummaryLevel
	Content string
}

// InteractionChunk mirrors conversation snippets embedded in the prompt.
type InteractionChunk struct {
	Role    string
	Content string
}

// ContextPack represents the curated payload delivered to the LLM.
type ContextPack struct {
	SystemPrompt    string
	Task            *Task
	Files           []FileChunk
	Summaries       []SummaryChunk
	History         []InteractionChunk
	Metadata        map[string]any
	EstimatedTokens int
}

// ContextBuilder produces budget-aware context packs for different workflows.
type ContextBuilder interface {
	BuildForEdit(task *Task, loc FileLocation) (*ContextPack, error)
	BuildForQuestion(task *Task) (*ContextPack, error)
	BuildForRefactorStep(task *Task, step *PlanStep) (*ContextPack, error)
}

// DefaultContextBuilder wires the SharedContext, CodeIndex, and SearchEngine
// together to produce context packs. Agents can embed or extend it.
type DefaultContextBuilder struct {
	SharedContext *SharedContext
	Search        *SearchEngine
	Index         CodeIndex
	Budget        *ContextBudget
}

// BuildForEdit focuses on the file being edited plus a shallow cone of influence.
func (b *DefaultContextBuilder) BuildForEdit(task *Task, loc FileLocation) (*ContextPack, error) {
	if b.SharedContext == nil {
		return nil, fmt.Errorf("shared context required")
	}
	pack := b.newPack(task)
	targetPath := loc.Path
	if targetPath == "" && task != nil && task.Metadata != nil {
		targetPath = task.Metadata["file"]
	}
	if targetPath == "" {
		return pack, nil
	}
	targetChunk, err := b.loadFileChunk(targetPath, loc, "target")
	if err != nil {
		return nil, err
	}
	pack.Files = append(pack.Files, targetChunk)
	dependencies := b.fetchCone(targetPath)
	for _, dep := range dependencies {
		pack.Summaries = append(pack.Summaries, SummaryChunk{
			Kind:    "file",
			ID:      dep.Path,
			Level:   SummaryConcise,
			Content: dep.Summary,
		})
	}
	pack.History = b.recentHistory()
	pack.EstimatedTokens = b.estimatePackTokens(pack)
	return pack, nil
}

// BuildForQuestion relies on summaries and history, avoiding heavy file content.
func (b *DefaultContextBuilder) BuildForQuestion(task *Task) (*ContextPack, error) {
	pack := b.newPack(task)
	if b.SharedContext != nil {
		pack.Summaries = append(pack.Summaries, SummaryChunk{
			Kind:    "history",
			ID:      "conversation",
			Level:   SummaryConcise,
			Content: b.SharedContext.GetConversationSummary(),
		})
	}
	if b.Search != nil && task != nil {
		results, err := b.Search.SearchWithBudget(SearchQuery{
			Text:           task.Instruction,
			Mode:           SearchHybrid,
			MaxResults:     5,
			IncludeSummary: true,
		}, 500)
		if err != nil {
			return nil, err
		}
		for _, result := range results {
			pack.Summaries = append(pack.Summaries, SummaryChunk{
				Kind:    result.RelevanceType,
				ID:      result.File,
				Level:   SummaryConcise,
				Content: result.Snippet,
			})
		}
	}
	pack.History = b.recentHistory()
	pack.EstimatedTokens = b.estimatePackTokens(pack)
	return pack, nil
}

// BuildForRefactorStep builds upon BuildForEdit but annotates with plan metadata.
func (b *DefaultContextBuilder) BuildForRefactorStep(task *Task, step *PlanStep) (*ContextPack, error) {
	targetPath := ""
	if task != nil && task.Metadata != nil {
		targetPath = task.Metadata["file"]
	}
	pack, err := b.BuildForEdit(task, FileLocation{Path: targetPath})
	if err != nil {
		return nil, err
	}
	if pack.Metadata == nil {
		pack.Metadata = make(map[string]any)
	}
	pack.Metadata["plan_step"] = step
	return pack, nil
}

func (b *DefaultContextBuilder) newPack(task *Task) *ContextPack {
	return &ContextPack{
		Task:      task,
		Files:     make([]FileChunk, 0),
		Summaries: make([]SummaryChunk, 0),
		History:   make([]InteractionChunk, 0),
		Metadata:  make(map[string]any),
	}
}

func (b *DefaultContextBuilder) loadFileChunk(path string, loc FileLocation, kind string) (FileChunk, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return FileChunk{}, err
	}
	lines := string(content)
	return FileChunk{
		Path:     path,
		Language: inferLanguage(path),
		Region:   loc,
		Content:  lines,
		Kind:     kind,
	}, nil
}

func (b *DefaultContextBuilder) fetchCone(path string) []FileSummary {
	if b.Index == nil {
		return nil
	}
	deps := b.Index.GetFileDependencies(path)
	summaries := make([]FileSummary, 0, len(deps))
	for _, dep := range deps {
		meta, ok := b.Index.GetFileMetadata(dep)
		if !ok {
			continue
		}
		summaries = append(summaries, FileSummary{
			Path:    meta.Path,
			Level:   SummaryConcise,
			Summary: meta.Summary,
		})
	}
	return summaries
}

func (b *DefaultContextBuilder) recentHistory() []InteractionChunk {
	if b.SharedContext == nil {
		return nil
	}
	history := b.SharedContext.History()
	max := 5
	if len(history) > max {
		history = history[len(history)-max:]
	}
	chunks := make([]InteractionChunk, 0, len(history))
	for _, interaction := range history {
		chunks = append(chunks, InteractionChunk{
			Role:    interaction.Role,
			Content: interaction.Content,
		})
	}
	return chunks
}

func (b *DefaultContextBuilder) estimatePackTokens(pack *ContextPack) int {
	total := estimateTokens(pack.SystemPrompt)
	if pack.Task != nil {
		total += estimateTokens(pack.Task.Instruction)
	}
	for _, file := range pack.Files {
		total += estimateCodeTokens(file.Content)
	}
	for _, summary := range pack.Summaries {
		total += estimateTokens(summary.Content)
	}
	for _, history := range pack.History {
		total += estimateTokens(history.Content)
	}
	return total
}

func inferLanguage(path string) string {
	switch filepath.Ext(path) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	default:
		return "text"
	}
}
