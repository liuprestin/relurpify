package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CompressedContext represents historical context in a condensed form.
type CompressedContext struct {
	Summary            string    `json:"summary"`
	KeyFacts           []KeyFact `json:"key_facts"`
	CompressedAt       time.Time `json:"compressed_at"`
	OriginalTokens     int       `json:"original_tokens"`
	CompressedTokens   int       `json:"compressed_tokens"`
	InteractionsCount  int       `json:"interactions_count"`
	StartInteractionID int       `json:"start_interaction_id"`
	EndInteractionID   int       `json:"end_interaction_id"`
}

// KeyFact represents an important piece of information extracted during compression.
type KeyFact struct {
	Type      string                 `json:"type"`
	Content   string                 `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
	Relevance float64                `json:"relevance"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// CompressionStrategy defines how to compress context.
type CompressionStrategy interface {
	Compress(interactions []Interaction, llm LanguageModel) (*CompressedContext, error)
	ShouldCompress(ctx *Context, budget *ContextBudget) bool
	EstimateTokens(cc *CompressedContext) int
	KeepRecent() int
}

// SimpleCompressionStrategy uses an LLM to summarize interactions.
type SimpleCompressionStrategy struct {
	PromptTemplate         string
	KeepRecentCount        int
	MinInteractionsTrigger int
}

// NewSimpleCompressionStrategy builds the default summarization strategy.
func NewSimpleCompressionStrategy() *SimpleCompressionStrategy {
	return &SimpleCompressionStrategy{
		PromptTemplate: `Summarize the following agent interactions into key facts and decisions.
Focus on: decisions made, files modified, errors encountered, important observations.
Interactions:
{{.Interactions}}

Provide a concise summary (max 200 words) and extract 5-10 key facts.`,
		KeepRecentCount:        5,
		MinInteractionsTrigger: 10,
	}
}

// Compress summarizes the provided interactions via the configured LLM.
func (s *SimpleCompressionStrategy) Compress(interactions []Interaction, llm LanguageModel) (*CompressedContext, error) {
	if len(interactions) == 0 {
		return nil, fmt.Errorf("no interactions to compress")
	}
	if llm == nil {
		return nil, fmt.Errorf("compression requires a language model")
	}
	prompt := s.buildPrompt(interactions)
	resp, err := llm.Generate(context.Background(), prompt, &LLMOptions{
		MaxTokens:   500,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("compression failed: %w", err)
	}
	cc, err := s.parseCompressionResponse(resp.Text, interactions)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compression: %w", err)
	}
	return cc, nil
}

// ShouldCompress determines whether the strategy recommends compression.
func (s *SimpleCompressionStrategy) ShouldCompress(ctx *Context, budget *ContextBudget) bool {
	if ctx == nil {
		return false
	}
	ctx.mu.RLock()
	historyLen := len(ctx.history)
	ctx.mu.RUnlock()
	if historyLen < s.MinInteractionsTrigger {
		return false
	}
	if budget != nil {
		usage := budget.GetCurrentUsage()
		if usage.ContextUsagePercent < 0.7 {
			return false
		}
	}
	return historyLen-s.KeepRecentCount > 0
}

// EstimateTokens estimates the compressed output size.
func (s *SimpleCompressionStrategy) EstimateTokens(cc *CompressedContext) int {
	if cc == nil {
		return 0
	}
	return estimateTokens(cc.Summary) + estimateTokens(cc.KeyFacts)
}

// KeepRecent returns the number of interactions to keep uncompressed.
func (s *SimpleCompressionStrategy) KeepRecent() int {
	return s.KeepRecentCount
}

func (s *SimpleCompressionStrategy) buildPrompt(interactions []Interaction) string {
	var sb strings.Builder
	sb.WriteString("Summarize these agent interactions:\n\n")
	for idx, interaction := range interactions {
		sb.WriteString(fmt.Sprintf("[%d] %s: %s\n", idx+1, interaction.Role, truncate(interaction.Content, 200)))
	}
	sb.WriteString("\nProvide:\n1. A concise summary (max 200 words)\n2. Key facts in JSON format: [{\"type\": \"decision\", \"content\": \"...\", \"relevance\": 0.9}]\n")
	return sb.String()
}

func (s *SimpleCompressionStrategy) parseCompressionResponse(response string, interactions []Interaction) (*CompressedContext, error) {
	parts := strings.Split(response, "Key Facts:")
	summary := strings.TrimSpace(strings.TrimPrefix(parts[0], "Summary:"))
	var keyFacts []KeyFact
	if len(parts) > 1 {
		factsJSON := strings.TrimSpace(parts[1])
		if err := json.Unmarshal([]byte(factsJSON), &keyFacts); err != nil {
			keyFacts = []KeyFact{
				{
					Type:      "summary",
					Content:   summary,
					Timestamp: time.Now().UTC(),
					Relevance: 1.0,
				},
			}
		}
	}
	originalTokens := estimateTokens(interactions)
	compressedTokens := estimateTokens(summary) + estimateTokens(keyFacts)
	return &CompressedContext{
		Summary:           summary,
		KeyFacts:          keyFacts,
		CompressedAt:      time.Now().UTC(),
		OriginalTokens:    originalTokens,
		CompressedTokens:  compressedTokens,
		InteractionsCount: len(interactions),
	}, nil
}

func estimateTokens(v interface{}) int {
	switch val := v.(type) {
	case string:
		return len(val) / 4
	case []Interaction:
		total := 0
		for _, i := range val {
			total += len(i.Content) / 4
		}
		return total
	case []KeyFact:
		total := 0
		for _, kf := range val {
			total += len(kf.Content) / 4
		}
		return total
	default:
		return 0
	}
}

func truncate(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}
