package framework

import "time"

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
