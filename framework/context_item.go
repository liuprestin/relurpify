package framework

import (
	"fmt"
	"time"
)

// ContextItem represents a unit that can be managed for budget purposes.
type ContextItem interface {
	TokenCount() int
	RelevanceScore() float64
	Priority() int
	Compress() (ContextItem, error)
	Type() ContextItemType
	Age() time.Duration
}

// ContextItemType categorizes managed items.
type ContextItemType string

const (
	ContextTypeInteraction ContextItemType = "interaction"
	ContextTypeFile        ContextItemType = "file"
	ContextTypeToolResult  ContextItemType = "tool_result"
	ContextTypeMemory      ContextItemType = "memory"
	ContextTypeObservation ContextItemType = "observation"
)

// InteractionContextItem wraps an Interaction as a context item.
type InteractionContextItem struct {
	Interaction Interaction
	Relevance   float64
	PriorityVal int
}

func (ici *InteractionContextItem) TokenCount() int {
	return len(ici.Interaction.Content) / 4
}

func (ici *InteractionContextItem) RelevanceScore() float64 {
	age := time.Since(ici.Interaction.Timestamp)
	decay := 1.0 / (1.0 + age.Hours()/24.0)
	return ici.Relevance * decay
}

func (ici *InteractionContextItem) Priority() int {
	return ici.PriorityVal
}

func (ici *InteractionContextItem) Compress() (ContextItem, error) {
	return &InteractionContextItem{
		Interaction: Interaction{
			ID:        ici.Interaction.ID,
			Role:      ici.Interaction.Role,
			Content:   truncate(ici.Interaction.Content, 100),
			Timestamp: ici.Interaction.Timestamp,
			Metadata:  ici.Interaction.Metadata,
		},
		Relevance:   ici.Relevance * 0.8,
		PriorityVal: ici.PriorityVal + 1,
	}, nil
}

func (ici *InteractionContextItem) Type() ContextItemType {
	return ContextTypeInteraction
}

func (ici *InteractionContextItem) Age() time.Duration {
	return time.Since(ici.Interaction.Timestamp)
}

// FileContextItem represents file contents tracked in context.
type FileContextItem struct {
	Path         string
	Content      string
	Summary      string
	LastAccessed time.Time
	Relevance    float64
	PriorityVal  int
	Pinned       bool
}

func (fci *FileContextItem) TokenCount() int {
	data := fci.Content
	if data == "" {
		data = fci.Summary
	}
	return len(data) / 4
}

func (fci *FileContextItem) RelevanceScore() float64 {
	if fci.Pinned {
		return 1.0
	}
	age := time.Since(fci.LastAccessed)
	decay := 1.0 / (1.0 + age.Minutes()/60.0)
	return fci.Relevance * decay
}

func (fci *FileContextItem) Priority() int {
	if fci.Pinned {
		return 0
	}
	return fci.PriorityVal
}

func (fci *FileContextItem) Compress() (ContextItem, error) {
	return &FileContextItem{
		Path:         fci.Path,
		Content:      "",
		Summary:      fci.Summary,
		LastAccessed: fci.LastAccessed,
		Relevance:    fci.Relevance * 0.9,
		PriorityVal:  fci.PriorityVal + 1,
		Pinned:       fci.Pinned,
	}, nil
}

func (fci *FileContextItem) Type() ContextItemType {
	return ContextTypeFile
}

func (fci *FileContextItem) Age() time.Duration {
	return time.Since(fci.LastAccessed)
}

// ToolResultContextItem represents structured tool outputs inside context.
type ToolResultContextItem struct {
	ToolName     string
	Result       *ToolResult
	LastAccessed time.Time
	Relevance    float64
	PriorityVal  int
}

func (tr *ToolResultContextItem) tokenPayload() string {
	if tr == nil || tr.Result == nil {
		return ""
	}
	if len(tr.Result.Data) == 0 {
		return tr.Result.Error
	}
	return fmt.Sprintf("%v", tr.Result.Data)
}

func (tr *ToolResultContextItem) TokenCount() int {
	return estimateTokens(tr.tokenPayload())
}

func (tr *ToolResultContextItem) RelevanceScore() float64 {
	if tr.Relevance == 0 {
		tr.Relevance = 0.8
	}
	age := time.Since(tr.LastAccessed)
	decay := 1.0 / (1.0 + age.Hours()/12.0)
	return tr.Relevance * decay
}

func (tr *ToolResultContextItem) Priority() int {
	return tr.PriorityVal
}

func (tr *ToolResultContextItem) Compress() (ContextItem, error) {
	payload := tr.tokenPayload()
	if len(payload) > 250 {
		payload = payload[:250] + "..."
	}
	return &ToolResultContextItem{
		ToolName:     tr.ToolName,
		Result:       &ToolResult{Success: tr.Result.Success, Data: map[string]interface{}{"summary": payload}},
		LastAccessed: tr.LastAccessed,
		Relevance:    tr.Relevance * 0.9,
		PriorityVal:  tr.PriorityVal + 1,
	}, nil
}

func (tr *ToolResultContextItem) Type() ContextItemType {
	return ContextTypeToolResult
}

func (tr *ToolResultContextItem) Age() time.Duration {
	return time.Since(tr.LastAccessed)
}
