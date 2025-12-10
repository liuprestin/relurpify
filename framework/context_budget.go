package framework

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// BudgetItem describes any piece of context (file snippet, history chunk, etc.)
// that competes for the limited token budget.
type BudgetItem interface {
	GetID() string
	GetTokenCount() int
	GetPriority() int
	CanCompress() bool
	Compress() (BudgetItem, error)
	CanEvict() bool
}

// AllocationPolicy configures how the ContextBudget splits and protects tokens.
type AllocationPolicy struct {
	SystemReserved     int
	Allocations        map[string]float64
	AllowBorrowing     bool
	MinimumPerCategory int
}

// Allocation tracks the state of a single category.
type Allocation struct {
	Category   string
	MaxTokens  int
	UsedTokens int
	Reserved   bool
	Priority   int
	Items      []BudgetItem
}

// BudgetListener receives events emitted by the ContextBudget. Agents can hook
// into these callbacks to log or trigger additional trimming.
type BudgetListener interface {
	OnBudgetWarning(usage float64)
	OnBudgetExceeded(category string, requested, available int)
	OnCompression(category string, savedTokens int)
}

// UsageStats summarize budget consumption across all categories.
type UsageStats struct {
	TotalTokens     int
	UsedTokens      int
	AvailableTokens int
	Percentage      float64
	Categories      map[string]*CategoryStats
}

// CategoryStats describe how a specific category is performing.
type CategoryStats struct {
	Category   string
	MaxTokens  int
	UsedTokens int
	Percentage float64
	ItemCount  int
}

// TokenUsage preserves the legacy token accounting format used by older agents.
type TokenUsage struct {
	SystemTokens        int
	ToolTokens          int
	ContextTokens       int
	OutputTokens        int
	TotalTokens         int
	ContextUsagePercent float64
}

// BudgetPolicies control legacy compression behaviour.
type BudgetPolicies struct {
	WarningThreshold     float64
	CompressionThreshold float64
	CriticalThreshold    float64
	AutoCompress         bool
	AutoPrune            bool
}

// BudgetState mirrors the historical budget state enumeration.
type BudgetState int

const (
	BudgetOK BudgetState = iota
	BudgetWarning
	BudgetNeedsCompression
	BudgetCritical
)

// ContextBudget enforces token allocations and coordinates compression/eviction.
type ContextBudget struct {
	mu                  sync.RWMutex
	MaxTokens           int
	ReservedForSystem   int
	ReservedForTools    int
	ReservedForOutput   int
	AvailableForContext int
	allocations         map[string]*Allocation
	policy              *AllocationPolicy
	warningThreshold    float64
	listeners           []BudgetListener
	usage               *UsageStats
	currentUsage        TokenUsage
	legacyPolicies      BudgetPolicies
}

// NewContextBudget returns a budget using the default policy.
func NewContextBudget(maxTokens int) *ContextBudget {
	return NewContextBudgetWithPolicy(maxTokens, nil)
}

// NewContextBudgetWithPolicy exposes the full constructor for advanced callers.
func NewContextBudgetWithPolicy(maxTokens int, policy *AllocationPolicy) *ContextBudget {
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	if policy == nil {
		policy = defaultAllocationPolicy()
	}
	cb := &ContextBudget{
		MaxTokens:         maxTokens,
		ReservedForSystem: 1000,
		ReservedForTools:  1500,
		ReservedForOutput: 1000,
		policy:            policy,
		warningThreshold:  0.8,
		allocations:       make(map[string]*Allocation),
		listeners:         make([]BudgetListener, 0),
		legacyPolicies: BudgetPolicies{
			WarningThreshold:     0.70,
			CompressionThreshold: 0.85,
			CriticalThreshold:    0.95,
			AutoCompress:         true,
			AutoPrune:            true,
		},
	}
	cb.calculateAvailableLocked()
	cb.recomputeAllocations()
	return cb
}

// Allocate reserves tokens inside a category, optionally associating a concrete
// item so the budget can compress or evict it later if needed.
func (cb *ContextBudget) Allocate(category string, tokens int, item BudgetItem) error {
	if tokens < 0 {
		return fmt.Errorf("cannot allocate negative tokens")
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	alloc, ok := cb.allocations[category]
	if !ok {
		return fmt.Errorf("unknown category %q", category)
	}
	if tokens == 0 && item != nil {
		tokens = item.GetTokenCount()
	}
	if tokens == 0 {
		return nil
	}
	if !cb.ensureCapacityLocked(alloc, tokens) {
		cb.emitExceeded(category, tokens, alloc.MaxTokens-alloc.UsedTokens)
		return fmt.Errorf("context budget exhausted for %s", category)
	}
	alloc.UsedTokens += tokens
	if item != nil {
		alloc.Items = append(alloc.Items, item)
	}
	cb.updateUsageLocked()
	return nil
}

// Free releases tokens from a category. If an item ID is supplied the entry is
// removed entirely; otherwise the raw token delta is applied.
func (cb *ContextBudget) Free(category string, tokens int, itemID string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	alloc, ok := cb.allocations[category]
	if !ok {
		return
	}
	if itemID != "" {
		filtered := alloc.Items[:0]
		for _, it := range alloc.Items {
			if it.GetID() == itemID {
				alloc.UsedTokens -= it.GetTokenCount()
				continue
			}
			filtered = append(filtered, it)
		}
		alloc.Items = filtered
	} else if tokens > 0 {
		alloc.UsedTokens -= tokens
	}
	if alloc.UsedTokens < 0 {
		alloc.UsedTokens = 0
	}
	cb.updateUsageLocked()
}

// GetUsage returns a copy of the current usage statistics.
func (cb *ContextBudget) GetUsage() *UsageStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cloneUsage(cb.usage)
}

// GetRemainingBudget reports how many tokens remain available for the category.
func (cb *ContextBudget) GetRemainingBudget(category string) int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	alloc, ok := cb.allocations[category]
	if !ok {
		return 0
	}
	remaining := alloc.MaxTokens - alloc.UsedTokens
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ShouldCompress indicates whether the overall usage is close to the limit.
func (cb *ContextBudget) ShouldCompress() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	if cb.usage == nil || cb.MaxTokens == 0 {
		return false
	}
	return cb.usage.Percentage >= cb.warningThreshold
}

// AddListener registers a listener for budgeting events.
func (cb *ContextBudget) AddListener(listener BudgetListener) {
	if listener == nil {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.listeners = append(cb.listeners, listener)
}

// Categories exposes the set of managed categories.
func (cb *ContextBudget) Categories() []string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	names := make([]string, 0, len(cb.allocations))
	for k := range cb.allocations {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func (cb *ContextBudget) ensureCapacityLocked(alloc *Allocation, tokens int) bool {
	if alloc.UsedTokens+tokens <= alloc.MaxTokens {
		return true
	}
	shortfall := alloc.UsedTokens + tokens - alloc.MaxTokens
	if cb.policy.AllowBorrowing && shortfall > 0 {
		cb.borrowTokensLocked(alloc.Category, shortfall)
	}
	if alloc.UsedTokens+tokens <= alloc.MaxTokens {
		return true
	}
	if cb.compressLocked(alloc.Category, shortfall) {
		return alloc.UsedTokens+tokens <= alloc.MaxTokens
	}
	return false
}

func (cb *ContextBudget) borrowTokensLocked(target string, tokens int) {
	if tokens <= 0 {
		return
	}
	var donors []*Allocation
	for name, alloc := range cb.allocations {
		if name == target || alloc.Reserved {
			continue
		}
		if alloc.MaxTokens-alloc.UsedTokens <= cb.policy.MinimumPerCategory {
			continue
		}
		donors = append(donors, alloc)
	}
	sort.Slice(donors, func(i, j int) bool {
		return donors[i].Priority > donors[j].Priority
	})
	for _, donor := range donors {
		available := donor.MaxTokens - donor.UsedTokens - cb.policy.MinimumPerCategory
		if available <= 0 {
			continue
		}
		transfer := minInt(tokens, available)
		donor.MaxTokens -= transfer
		cb.allocations[target].MaxTokens += transfer
		tokens -= transfer
		if tokens <= 0 {
			break
		}
	}
}

func (cb *ContextBudget) compressLocked(category string, target int) bool {
	if target <= 0 {
		return true
	}
	alloc, ok := cb.allocations[category]
	if !ok || len(alloc.Items) == 0 {
		return false
	}
	sort.SliceStable(alloc.Items, func(i, j int) bool {
		return alloc.Items[i].GetPriority() > alloc.Items[j].GetPriority()
	})
	saved := 0
	replacements := make([]BudgetItem, 0, len(alloc.Items))
	for _, item := range alloc.Items {
		if !item.CanCompress() {
			replacements = append(replacements, item)
			continue
		}
		compressed, err := item.Compress()
		if err != nil {
			replacements = append(replacements, item)
			continue
		}
		saved += item.GetTokenCount() - compressed.GetTokenCount()
		replacements = append(replacements, compressed)
		if saved >= target {
			break
		}
	}
	if saved > 0 {
		alloc.Items = replacements
		alloc.UsedTokens -= saved
		if alloc.UsedTokens < 0 {
			alloc.UsedTokens = 0
		}
		cb.emitCompression(category, saved)
	}
	return saved >= target
}

func (cb *ContextBudget) emitWarning(usage float64) {
	for _, listener := range cb.listeners {
		listener.OnBudgetWarning(usage)
	}
}

func (cb *ContextBudget) emitExceeded(category string, requested, available int) {
	for _, listener := range cb.listeners {
		listener.OnBudgetExceeded(category, requested, available)
	}
}

func (cb *ContextBudget) emitCompression(category string, saved int) {
	for _, listener := range cb.listeners {
		listener.OnCompression(category, saved)
	}
}

func (cb *ContextBudget) recomputeAllocations() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.allocations = make(map[string]*Allocation, len(cb.policy.Allocations)+1)
	cb.calculateAvailableLocked()
	remaining := cb.MaxTokens - cb.policy.SystemReserved
	if remaining < 0 {
		remaining = 0
	}
	priority := len(cb.policy.Allocations)
	for category, pct := range cb.policy.Allocations {
		maxTokens := int(float64(remaining) * pct)
		if maxTokens < cb.policy.MinimumPerCategory {
			maxTokens = cb.policy.MinimumPerCategory
		}
		cb.allocations[category] = &Allocation{
			Category:  category,
			MaxTokens: maxTokens,
			Priority:  priority,
			Items:     make([]BudgetItem, 0),
		}
		priority--
	}
	cb.allocations["system"] = &Allocation{
		Category:  "system",
		MaxTokens: cb.policy.SystemReserved,
		Reserved:  true,
		Priority:  priority,
		Items:     nil,
	}
	cb.updateUsageLocked()
}

func (cb *ContextBudget) updateUsageLocked() {
	stats := &UsageStats{
		TotalTokens: cb.MaxTokens,
		Categories:  make(map[string]*CategoryStats, len(cb.allocations)),
	}
	totalUsed := 0
	for name, alloc := range cb.allocations {
		stats.Categories[name] = &CategoryStats{
			Category:   name,
			MaxTokens:  alloc.MaxTokens,
			UsedTokens: alloc.UsedTokens,
			ItemCount:  len(alloc.Items),
		}
		if alloc.MaxTokens > 0 {
			stats.Categories[name].Percentage = float64(alloc.UsedTokens) / float64(alloc.MaxTokens)
		}
		totalUsed += alloc.UsedTokens
	}
	stats.UsedTokens = totalUsed
	stats.AvailableTokens = cb.MaxTokens - totalUsed
	if stats.AvailableTokens < 0 {
		stats.AvailableTokens = 0
	}
	if cb.MaxTokens > 0 {
		stats.Percentage = float64(totalUsed) / float64(cb.MaxTokens)
	}
	cb.usage = stats
	if stats.Percentage >= cb.warningThreshold {
		cb.emitWarning(stats.Percentage)
	}
}

func cloneUsage(src *UsageStats) *UsageStats {
	if src == nil {
		return nil
	}
	clone := &UsageStats{
		TotalTokens:     src.TotalTokens,
		UsedTokens:      src.UsedTokens,
		AvailableTokens: src.AvailableTokens,
		Percentage:      src.Percentage,
		Categories:      make(map[string]*CategoryStats, len(src.Categories)),
	}
	for name, cat := range src.Categories {
		clone.Categories[name] = &CategoryStats{
			Category:   cat.Category,
			MaxTokens:  cat.MaxTokens,
			UsedTokens: cat.UsedTokens,
			Percentage: cat.Percentage,
			ItemCount:  cat.ItemCount,
		}
	}
	return clone
}

func defaultAllocationPolicy() *AllocationPolicy {
	return &AllocationPolicy{
		SystemReserved: 1000,
		Allocations: map[string]float64{
			"tools":      0.15,
			"immediate":  0.35,
			"recent":     0.15,
			"search":     0.10,
			"history":    0.15,
			"background": 0.10,
		},
		AllowBorrowing:     true,
		MinimumPerCategory: 128,
	}
}

func (cb *ContextBudget) calculateAvailableLocked() {
	cb.AvailableForContext = cb.MaxTokens - cb.ReservedForSystem - cb.ReservedForTools - cb.ReservedForOutput
	if cb.AvailableForContext < 0 {
		cb.AvailableForContext = 0
	}
}

// ErrInvalidBudget signals misconfiguration in the policy.
var ErrInvalidBudget = errors.New("invalid context budget configuration")

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Legacy compatibility helpers ------------------------------------------------

// SetReservations mirrors the previous API for reserving system/tool/output tokens.
func (cb *ContextBudget) SetReservations(system, tools, output int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.ReservedForSystem = system
	cb.ReservedForTools = tools
	cb.ReservedForOutput = output
	cb.calculateAvailableLocked()
}

// SetPolicies configures the legacy warning/compression thresholds.
func (cb *ContextBudget) SetPolicies(policies BudgetPolicies) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.legacyPolicies = policies
}

// UpdateUsage recomputes legacy usage metrics to keep older agents functional.
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
	}
}

// GetCurrentUsage exposes the legacy token accounting snapshot.
func (cb *ContextBudget) GetCurrentUsage() TokenUsage {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.currentUsage
}

// CheckBudget returns the previous BudgetState categorization.
func (cb *ContextBudget) CheckBudget() BudgetState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	usage := cb.currentUsage.ContextUsagePercent
	switch {
	case usage >= cb.legacyPolicies.CriticalThreshold:
		return BudgetCritical
	case usage >= cb.legacyPolicies.CompressionThreshold:
		return BudgetNeedsCompression
	case usage >= cb.legacyPolicies.WarningThreshold:
		return BudgetWarning
	default:
		return BudgetOK
	}
}

// CanAddTokens retains the previous helper for quick capacity checks.
func (cb *ContextBudget) CanAddTokens(tokens int) bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	projected := cb.currentUsage.ContextTokens + tokens
	return projected <= cb.AvailableForContext
}

// GetAvailableTokens reports remaining capacity in the legacy format.
func (cb *ContextBudget) GetAvailableTokens() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	remaining := cb.AvailableForContext - cb.currentUsage.ContextTokens
	if remaining < 0 {
		return 0
	}
	return remaining
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
