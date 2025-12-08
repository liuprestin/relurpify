package framework

import (
	"context"
	"fmt"
)

type stubLLM struct {
	text string
}

func (s *stubLLM) Generate(ctx context.Context, prompt string, options *LLMOptions) (*LLMResponse, error) {
	return &LLMResponse{Text: s.text}, nil
}

func (s *stubLLM) GenerateStream(ctx context.Context, prompt string, options *LLMOptions) (<-chan string, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubLLM) Chat(ctx context.Context, messages []Message, options *LLMOptions) (*LLMResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubLLM) ChatWithTools(ctx context.Context, messages []Message, tools []Tool, options *LLMOptions) (*LLMResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

type stubCompressionStrategy struct {
	compressed *CompressedContext
	should     bool
	recent     int
}

func (s *stubCompressionStrategy) Compress(interactions []Interaction, llm LanguageModel) (*CompressedContext, error) {
	return s.compressed, nil
}

func (s *stubCompressionStrategy) ShouldCompress(ctx *Context, budget *ContextBudget) bool {
	return s.should
}

func (s *stubCompressionStrategy) EstimateTokens(cc *CompressedContext) int {
	if cc == nil {
		return 0
	}
	return cc.CompressedTokens
}

func (s *stubCompressionStrategy) KeepRecent() int {
	if s.recent == 0 {
		return 5
	}
	return s.recent
}
