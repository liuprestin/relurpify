package ast

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// MarkdownParser extracts headings, code blocks, and links.
type MarkdownParser struct {
	heading   *regexp.Regexp
	codeBlock *regexp.Regexp
	link      *regexp.Regexp
}

// NewMarkdownParser creates a parser.
func NewMarkdownParser() *MarkdownParser {
	return &MarkdownParser{
		heading:   regexp.MustCompile(`^(#{1,6})\s+(.+)$`),
		codeBlock: regexp.MustCompile("```([a-zA-Z0-9_]*)\\n([\\s\\S]*?)```"),
		link:      regexp.MustCompile(`\[([^\]]+)\]\(([^\)]+)\)`),
	}
}

func (mp *MarkdownParser) Language() string          { return "markdown" }
func (mp *MarkdownParser) Category() Category        { return CategoryDoc }
func (mp *MarkdownParser) SupportsIncremental() bool { return false }
func (mp *MarkdownParser) ParseIncremental(*ParseResult, []ContentChange) (*ParseResult, error) {
	return nil, fmt.Errorf("markdown incremental parsing not implemented")
}

// Parse converts markdown into hierarchical nodes.
func (mp *MarkdownParser) Parse(content string, filePath string) (*ParseResult, error) {
	lines := strings.Split(content, "\n")
	fileID := GenerateFileID(filePath)
	now := time.Now().UTC()
	root := &Node{
		ID:        fmt.Sprintf("%s:root", fileID),
		FileID:    fileID,
		Type:      NodeTypeDocument,
		Category:  CategoryDoc,
		Language:  "markdown",
		Name:      filepath.Base(filePath),
		StartLine: 1,
		EndLine:   len(lines),
		CreatedAt: now,
		UpdatedAt: now,
	}
	result := &ParseResult{
		RootNode: root,
		Nodes:    []*Node{root},
		Edges:    make([]*Edge, 0),
	}

	parentStack := []string{root.ID}
	for idx, line := range lines {
		match := mp.heading.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		level := len(match[1])
		for len(parentStack) > level {
			parentStack = parentStack[:len(parentStack)-1]
		}
		node := &Node{
			ID:        fmt.Sprintf("%s:heading:%d", fileID, idx),
			ParentID:  parentStack[len(parentStack)-1],
			FileID:    fileID,
			Type:      NodeTypeHeading,
			Category:  CategoryDoc,
			Language:  "markdown",
			Name:      strings.TrimSpace(match[2]),
			StartLine: idx + 1,
			EndLine:   idx + 1,
			CreatedAt: now,
			UpdatedAt: now,
			Attributes: map[string]interface{}{
				"level": level,
			},
		}
		result.Nodes = append(result.Nodes, node)
		parentStack = append(parentStack, node.ID)
	}

	codeBlocks := mp.codeBlock.FindAllStringSubmatchIndex(content, -1)
	for i, block := range codeBlocks {
		if len(block) < 6 {
			continue
		}
		lang := content[block[2]:block[3]]
		body := content[block[4]:block[5]]
		node := &Node{
			ID:        fmt.Sprintf("%s:code:%d", fileID, i),
			ParentID:  root.ID,
			FileID:    fileID,
			Type:      NodeTypeCodeBlock,
			Category:  CategoryDoc,
			Language:  "markdown",
			Name:      fmt.Sprintf("Code Block %d", i+1),
			CreatedAt: now,
			UpdatedAt: now,
			Attributes: map[string]interface{}{
				"language": lang,
				"content":  body,
			},
		}
		result.Nodes = append(result.Nodes, node)
	}

	links := mp.link.FindAllStringSubmatch(content, -1)
	for i, link := range links {
		if len(link) < 3 {
			continue
		}
		node := &Node{
			ID:        fmt.Sprintf("%s:link:%d", fileID, i),
			ParentID:  root.ID,
			FileID:    fileID,
			Type:      NodeTypeLink,
			Category:  CategoryDoc,
			Language:  "markdown",
			Name:      link[1],
			CreatedAt: now,
			UpdatedAt: now,
			Attributes: map[string]interface{}{
				"url": link[2],
			},
		}
		result.Nodes = append(result.Nodes, node)
	}

	result.Metadata = &FileMetadata{
		ID:            fileID,
		Path:          filePath,
		RelativePath:  filepath.Base(filePath),
		Language:      "markdown",
		Category:      CategoryDoc,
		LineCount:     len(lines),
		TokenCount:    len(content),
		Complexity:    0,
		ContentHash:   HashContent(content),
		RootNodeID:    root.ID,
		NodeCount:     len(result.Nodes),
		EdgeCount:     len(result.Edges),
		IndexedAt:     now,
		ParserVersion: "0.1.0",
	}
	return result, nil
}
