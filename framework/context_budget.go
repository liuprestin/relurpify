package framework

import (
	"fmt"
	"sort"
	"sync"
)

// ContextBudget manages token allocation across context sections.
type ContextBudget struct {
	mu sync.RWMutex

	MaxTokens           int
	ReservedForSystem   int
	ReservedForTools    int
	ReservedForOutput   int
	AvailableForContext int

	currentUsage TokenUsage
	policies     BudgetPolicies
}

// TokenUsage tracks the current token consumption.
type TokenUsage struct {
	SystemTokens        int
	ToolTokens          int
	ContextTokens       int
	OutputTokens        int
	TotalTokens         int
	ContextUsagePercent float64
}

// BudgetPolicies define how the budget reacts to pressure.
type BudgetPolicies struct {
	WarningThreshold     float64
	CompressionThreshold float64
	CriticalThreshold    float64
	AutoCompress         bool
	AutoPrune            bool
}

// NewContextBudget builds a budget with sane defaults.
func NewContextBudget(maxTokens int) *ContextBudget {
	cb := &ContextBudget{
		MaxTokens:         maxTokens,
		ReservedForSystem: 1000,
		ReservedForTools:  2000,
		ReservedForOutput: 2000,
		policies: BudgetPolicies{
			WarningThreshold:     0.70,
			CompressionThreshold: 0.80,
			CriticalThreshold:    0.90,
			AutoCompress:         true,
			AutoPrune:            true,
		},
	}
	cb.calculateAvailable()
	return cb
}

// SetReservations updates the reserved buckets.
func (cb *ContextBudget) SetReservations(system, tools, output int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.ReservedForSystem = system
	cb.ReservedForTools = tools
	cb.ReservedForOutput = output
	cb.calculateAvailable()
}

// SetPolicies updates the budget policies.
func (cb *ContextBudget) SetPolicies(policies BudgetPolicies) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.policies = policies
}

// GetCurrentUsage returns the current tracked usage.
func (cb *ContextBudget) GetCurrentUsage() TokenUsage {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.currentUsage
}

// UpdateUsage recalculates usage from the shared context and tool schemas.
func (cb *ContextBudget) UpdateUsage(ctx *Context, toolSchemas []Tool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	systemTokens := cb.ReservedForSystem
	toolTokens := 0
	for _, tool := range toolSchemas {
		toolTokens += estimateToolTokens(tool)
	}
	contextTokens := estimateContextTokens(ctx)

	cb.currentUsage = TokenUsage{
		SystemTokens:  systemTokens,
		ToolTokens:    toolTokens,
		ContextTokens: contextTokens,
		OutputTokens:  cb.ReservedForOutput,
		TotalTokens:   systemTokens + toolTokens + contextTokens + cb.ReservedForOutput,
	}

	if cb.AvailableForContext > 0 {
		cb.currentUsage.ContextUsagePercent = float64(contextTokens) / float64(cb.AvailableForContext)
	} else {
		cb.currentUsage.ContextUsagePercent = 1.0
	}
}

// CheckBudget reports the health of the context budget.
func (cb *ContextBudget) CheckBudget() BudgetState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	usage := cb.currentUsage.ContextUsagePercent
	switch {
	case usage >= cb.policies.CriticalThreshold:
		return BudgetCritical
	case usage >= cb.policies.CompressionThreshold:
		return BudgetNeedsCompression
	case usage >= cb.policies.WarningThreshold:
		return BudgetWarning
	default:
		return BudgetOK
	}
}

// CanAddTokens returns true if the context has capacity for the requested tokens.
func (cb *ContextBudget) CanAddTokens(tokens int) bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	projected := cb.currentUsage.ContextTokens + tokens
	return projected <= cb.AvailableForContext
}

// GetAvailableTokens returns the remaining budget for context tokens.
func (cb *ContextBudget) GetAvailableTokens() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	remaining := cb.AvailableForContext - cb.currentUsage.ContextTokens
	if remaining < 0 {
		return 0
	}
	return remaining
}

// BudgetState captures the bucket health.
type BudgetState int

const (
	BudgetOK BudgetState = iota
	BudgetWarning
	BudgetNeedsCompression
	BudgetCritical
)

func (bs BudgetState) String() string {
	switch bs {
	case BudgetOK:
		return "OK"
	case BudgetWarning:
		return "Warning"
	case BudgetNeedsCompression:
		return "Needs Compression"
	case BudgetCritical:
		return "Critical"
	default:
		return "Unknown"
	}
}

func (cb *ContextBudget) calculateAvailable() {
	cb.AvailableForContext = cb.MaxTokens - cb.ReservedForSystem - cb.ReservedForTools - cb.ReservedForOutput
	if cb.AvailableForContext < 0 {
		cb.AvailableForContext = 0
	}
}

func estimateToolTokens(tool Tool) int {
	if tool == nil {
		return 0
	}
	base := len(tool.Name())/4 + len(tool.Description())/4
	params := tool.Parameters()
	paramTokens := len(params) * 50
	return base + paramTokens
}

func estimateContextTokens(ctx *Context) int {
	if ctx == nil {
		return 0
	}
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	total := 0
	for _, cc := range ctx.compressedHistory {
		total += cc.CompressedTokens
	}
	for _, interaction := range ctx.history {
		total += len(interaction.Content) / 4
	}
	total += len(fmt.Sprint(ctx.state)) / 4
	total += len(fmt.Sprint(ctx.variables)) / 4
	total += len(fmt.Sprint(ctx.knowledge)) / 4
	return total
}

// SortByRelevance helps selecting highest impact items first.
func SortByRelevance(items []ContextItem) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].RelevanceScore() > items[j].RelevanceScore()
	})
}
