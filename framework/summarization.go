package framework

import (
	"fmt"
	"strings"
)

// SummaryLevel indicates how detailed summaries should be.
type SummaryLevel int

const (
	SummaryFull SummaryLevel = iota
	SummaryDetailed
	SummaryConcise
	SummaryMinimal
)

// FileSummary captures a summarized representation of a file.
type FileSummary struct {
	Path         string
	Level        SummaryLevel
	Summary      string
	Symbols      []string
	Dependencies []string
	TokenCount   int
	Version      string
}

// DirectorySummary aggregates summaries across a directory.
type DirectorySummary struct {
	Path       string
	Level      SummaryLevel
	Summary    string
	Files      []string
	TokenCount int
	Version    string
}

// ChunkSummary captures the description of a code chunk or symbol.
type ChunkSummary struct {
	ChunkID    string
	Level      SummaryLevel
	Summary    string
	TokenCount int
	Version    string
}

// Summarizer abstracts different summarization backends (LLMs, heuristics, etc.)
type Summarizer interface {
	Summarize(content string, level SummaryLevel) (string, error)
	SummarizeFile(path string, content string, level SummaryLevel) (*FileSummary, error)
	SummarizeDirectory(path string, files []FileSummary, level SummaryLevel) (*DirectorySummary, error)
	SummarizeChunk(chunk CodeChunk, content string, level SummaryLevel) (*ChunkSummary, error)
}

// SimpleSummarizer is a deterministic fallback that extracts the first few
// sentences from the content. While crude, it keeps the CLI usable when an LLM
// summarizer is not configured.
type SimpleSummarizer struct{}

// Summarize returns a concise snippet respecting the requested level.
func (s *SimpleSummarizer) Summarize(content string, level SummaryLevel) (string, error) {
	if content == "" {
		return "", nil
	}
	limit := 120
	switch level {
	case SummaryFull:
		return content, nil
	case SummaryDetailed:
		limit = 400
	case SummaryConcise:
		limit = 200
	case SummaryMinimal:
		limit = 80
	}
	return truncateParagraph(content, limit), nil
}

// SummarizeFile implements Summarizer.
func (s *SimpleSummarizer) SummarizeFile(path string, content string, level SummaryLevel) (*FileSummary, error) {
	text, err := s.Summarize(content, level)
	if err != nil {
		return nil, err
	}
	return &FileSummary{
		Path:       path,
		Level:      level,
		Summary:    text,
		TokenCount: estimateTokens(text),
	}, nil
}

// SummarizeDirectory implements Summarizer by concatenating file summaries.
func (s *SimpleSummarizer) SummarizeDirectory(path string, files []FileSummary, level SummaryLevel) (*DirectorySummary, error) {
	chunks := make([]string, 0, len(files))
	for _, file := range files {
		if file.Summary == "" {
			continue
		}
		chunks = append(chunks, fmt.Sprintf("%s: %s", file.Path, file.Summary))
	}
	text, err := s.Summarize(strings.Join(chunks, "\n"), level)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Path)
	}
	return &DirectorySummary{
		Path:       path,
		Level:      level,
		Summary:    text,
		Files:      names,
		TokenCount: estimateTokens(text),
	}, nil
}

// SummarizeChunk implements Summarizer for code chunks.
func (s *SimpleSummarizer) SummarizeChunk(chunk CodeChunk, content string, level SummaryLevel) (*ChunkSummary, error) {
	text, err := s.Summarize(content, level)
	if err != nil {
		return nil, err
	}
	return &ChunkSummary{
		ChunkID:    chunk.ID,
		Level:      level,
		Summary:    text,
		TokenCount: estimateTokens(text),
		Version:    chunk.ID,
	}, nil
}

func truncateParagraph(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return strings.TrimSpace(value)
	}
	trim := strings.TrimSpace(value[:max])
	return fmt.Sprintf("%s...", trim)
}
