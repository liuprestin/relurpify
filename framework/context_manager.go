package framework

import (
	"fmt"
	"sync"
)

// ContextManager orchestrates context items within a budget.
type ContextManager struct {
	mu       sync.RWMutex
	budget   *ContextBudget
	items    []ContextItem
	strategy PruningStrategy
}

// PruningStrategy defines selection rules for compression/pruning.
type PruningStrategy interface {
	SelectForPruning(items []ContextItem, targetTokens int) []ContextItem
	SelectForCompression(items []ContextItem, targetTokens int) []ContextItem
}

// NewContextManager builds a manager with the default strategy.
func NewContextManager(budget *ContextBudget) *ContextManager {
	return &ContextManager{
		budget:   budget,
		items:    make([]ContextItem, 0),
		strategy: NewRelevanceBasedStrategy(),
	}
}

// AddItem registers a new context item, enforcing the budget.
func (cm *ContextManager) AddItem(item ContextItem) error {
	if item == nil {
		return fmt.Errorf("nil context item")
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if !cm.budget.CanAddTokens(item.TokenCount()) {
		if err := cm.makeSpaceLocked(item.TokenCount()); err != nil {
			return fmt.Errorf("cannot add item: %w", err)
		}
	}
	cm.items = append(cm.items, item)
	cm.updateBudgetLocked()
	return nil
}

// makeSpace frees tokens via compression or pruning.
func (cm *ContextManager) makeSpace(neededTokens int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.makeSpaceLocked(neededTokens)
}

// MakeSpace is the exported wrapper for freeing capacity.
func (cm *ContextManager) MakeSpace(tokens int) error {
	return cm.makeSpace(tokens)
}

func (cm *ContextManager) makeSpaceLocked(neededTokens int) error {
	state := cm.budget.CheckBudget()
	switch state {
	case BudgetNeedsCompression:
		return cm.compressItemsLocked(neededTokens)
	case BudgetCritical:
		if err := cm.compressItemsLocked(neededTokens); err == nil {
			return nil
		}
		return cm.pruneItemsLocked(neededTokens)
	default:
		return fmt.Errorf("insufficient budget")
	}
}

func (cm *ContextManager) compressItemsLocked(targetTokens int) error {
	toCompress := cm.strategy.SelectForCompression(cm.items, targetTokens)
	if len(toCompress) == 0 {
		return fmt.Errorf("no items available for compression")
	}
	freed := 0
	replacements := make(map[ContextItem]ContextItem)
	for _, item := range toCompress {
		compressed, err := item.Compress()
		if err != nil {
			continue
		}
		freed += item.TokenCount() - compressed.TokenCount()
		replacements[item] = compressed
		if freed >= targetTokens {
			break
		}
	}
	if freed < targetTokens {
		return fmt.Errorf("compression freed only %d tokens, needed %d", freed, targetTokens)
	}
	cm.replaceItemsLocked(replacements)
	cm.updateBudgetLocked()
	return nil
}

func (cm *ContextManager) pruneItemsLocked(targetTokens int) error {
	toPrune := cm.strategy.SelectForPruning(cm.items, targetTokens)
	if len(toPrune) == 0 {
		return fmt.Errorf("no items available for pruning")
	}
	freed := 0
	removeSet := make(map[ContextItem]struct{})
	for _, item := range toPrune {
		freed += item.TokenCount()
		removeSet[item] = struct{}{}
	}
	if freed < targetTokens {
		return fmt.Errorf("pruning would free only %d tokens, needed %d", freed, targetTokens)
	}
	filtered := make([]ContextItem, 0, len(cm.items))
	for _, item := range cm.items {
		if _, remove := removeSet[item]; !remove {
			filtered = append(filtered, item)
		}
	}
	cm.items = filtered
	cm.updateBudgetLocked()
	return nil
}

// GetItems returns all items tracked by the manager.
func (cm *ContextManager) GetItems() []ContextItem {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return append([]ContextItem(nil), cm.items...)
}

// GetItemsByType returns the subset of items matching the provided type.
func (cm *ContextManager) GetItemsByType(t ContextItemType) []ContextItem {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	result := make([]ContextItem, 0)
	for _, item := range cm.items {
		if item.Type() == t {
			result = append(result, item)
		}
	}
	return result
}

// Clear removes all context items.
func (cm *ContextManager) Clear() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.items = make([]ContextItem, 0)
	cm.updateBudgetLocked()
}

// GetStats reports aggregated item/budget information.
func (cm *ContextManager) GetStats() ContextStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	stats := ContextStats{
		TotalItems:  len(cm.items),
		TotalTokens: 0,
		ItemsByType: make(map[ContextItemType]int),
	}
	for _, item := range cm.items {
		stats.TotalTokens += item.TokenCount()
		stats.ItemsByType[item.Type()]++
	}
	stats.BudgetUsage = cm.budget.GetCurrentUsage()
	stats.BudgetState = cm.budget.CheckBudget()
	return stats
}

// ContextStats captures context management metrics.
type ContextStats struct {
	TotalItems  int
	TotalTokens int
	ItemsByType map[ContextItemType]int
	BudgetUsage TokenUsage
	BudgetState BudgetState
}

func (cm *ContextManager) updateBudgetLocked() {
	total := 0
	for _, item := range cm.items {
		total += item.TokenCount()
	}
	usage := cm.budget.GetCurrentUsage()
	usage.ContextTokens = total
	usage.TotalTokens = usage.SystemTokens + usage.ToolTokens + total + usage.OutputTokens
	if cm.budget.AvailableForContext > 0 {
		usage.ContextUsagePercent = float64(total) / float64(cm.budget.AvailableForContext)
	}
	cm.budget.currentUsage = usage
}

func (cm *ContextManager) replaceItemsLocked(replacements map[ContextItem]ContextItem) {
	replaced := make([]ContextItem, 0, len(cm.items))
	for _, item := range cm.items {
		if replacement, ok := replacements[item]; ok {
			replaced = append(replaced, replacement)
		} else {
			replaced = append(replaced, item)
		}
	}
	cm.items = replaced
}
