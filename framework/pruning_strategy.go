package framework

import "sort"

// RelevanceBasedStrategy prunes/compresses based on relevance and priority.
type RelevanceBasedStrategy struct {
	MinRelevance      float64
	PreservePriority0 bool
}

// NewRelevanceBasedStrategy builds the default relevance-based strategy.
func NewRelevanceBasedStrategy() *RelevanceBasedStrategy {
	return &RelevanceBasedStrategy{
		MinRelevance:      0.1,
		PreservePriority0: true,
	}
}

// SelectForPruning chooses the lowest value items to drop entirely.
func (rbs *RelevanceBasedStrategy) SelectForPruning(items []ContextItem, targetTokens int) []ContextItem {
	sorted := append([]ContextItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		scoreI := sorted[i].RelevanceScore() * float64(10-sorted[i].Priority())
		scoreJ := sorted[j].RelevanceScore() * float64(10-sorted[j].Priority())
		return scoreI < scoreJ
	})
	selected := make([]ContextItem, 0)
	freed := 0
	for _, item := range sorted {
		if rbs.PreservePriority0 && item.Priority() == 0 {
			continue
		}
		if item.RelevanceScore() > rbs.MinRelevance {
			continue
		}
		selected = append(selected, item)
		freed += item.TokenCount()
		if freed >= targetTokens {
			break
		}
	}
	return selected
}

// SelectForCompression picks items suitable for lossy compression.
func (rbs *RelevanceBasedStrategy) SelectForCompression(items []ContextItem, targetTokens int) []ContextItem {
	sorted := append([]ContextItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		ageI := sorted[i].Age().Minutes()
		ageJ := sorted[j].Age().Minutes()
		scoreI := ageI / (sorted[i].RelevanceScore() + 0.1)
		scoreJ := ageJ / (sorted[j].RelevanceScore() + 0.1)
		return scoreI > scoreJ
	})
	selected := make([]ContextItem, 0)
	projected := 0
	for _, item := range sorted {
		if item.Priority() == 0 {
			continue
		}
		savings := item.TokenCount() / 2
		selected = append(selected, item)
		projected += savings
		if projected >= targetTokens {
			break
		}
	}
	return selected
}

// LRUStrategy removes the oldest items first.
type LRUStrategy struct{}

// SelectForPruning chooses the oldest items until enough tokens are freed.
func (lru *LRUStrategy) SelectForPruning(items []ContextItem, targetTokens int) []ContextItem {
	sorted := append([]ContextItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Age() > sorted[j].Age()
	})
	selected := make([]ContextItem, 0)
	freed := 0
	for _, item := range sorted {
		if item.Priority() == 0 {
			continue
		}
		selected = append(selected, item)
		freed += item.TokenCount()
		if freed >= targetTokens {
			break
		}
	}
	return selected
}

// SelectForCompression reuses pruning order but with a larger target to bias toward compression first.
func (lru *LRUStrategy) SelectForCompression(items []ContextItem, targetTokens int) []ContextItem {
	return lru.SelectForPruning(items, targetTokens*2)
}

// HybridStrategy combines relevance, age, and priority.
type HybridStrategy struct {
	relevanceWeight float64
	ageWeight       float64
	priorityWeight  float64
}

// NewHybridStrategy builds a hybrid scorer.
func NewHybridStrategy() *HybridStrategy {
	return &HybridStrategy{
		relevanceWeight: 0.5,
		ageWeight:       0.3,
		priorityWeight:  0.2,
	}
}

// SelectForPruning chooses items based on a composite score.
func (hs *HybridStrategy) SelectForPruning(items []ContextItem, targetTokens int) []ContextItem {
	type scored struct {
		item  ContextItem
		score float64
	}
	scoredItems := make([]scored, 0, len(items))
	for _, item := range items {
		if item.Priority() == 0 {
			continue
		}
		relevance := item.RelevanceScore()
		ageScore := 1.0 / (1.0 + item.Age().Hours())
		priorityScore := 1.0 - (float64(item.Priority()) / 10.0)
		composite := hs.relevanceWeight*relevance + hs.ageWeight*ageScore + hs.priorityWeight*priorityScore
		scoredItems = append(scoredItems, scored{item: item, score: composite})
	}
	sort.Slice(scoredItems, func(i, j int) bool {
		return scoredItems[i].score < scoredItems[j].score
	})
	selected := make([]ContextItem, 0)
	freed := 0
	for _, candidate := range scoredItems {
		selected = append(selected, candidate.item)
		freed += candidate.item.TokenCount()
		if freed >= targetTokens {
			break
		}
	}
	return selected
}

// SelectForCompression reuses the pruning selection but requires a larger freeing target.
func (hs *HybridStrategy) SelectForCompression(items []ContextItem, targetTokens int) []ContextItem {
	return hs.SelectForPruning(items, targetTokens*2)
}
