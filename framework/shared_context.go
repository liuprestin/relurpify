package framework

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// DetailLevel controls how much of a file is retained inside the working set.
type DetailLevel int

const (
	DetailFull DetailLevel = iota
	DetailBodyOnly
	DetailSignature
	DetailSummary
)

// FileContext keeps metadata about a tracked file or snippet.
type FileContext struct {
	Path         string
	Language     string
	Content      string
	Summary      string
	Level        DetailLevel
	LastAccessed time.Time
	Version      string
	Pinned       bool
}

func (fc *FileContext) tokens() int {
	if fc.Content != "" && fc.Level <= DetailBodyOnly {
		return estimateCodeTokens(fc.Content)
	}
	return estimateTokens(fc.getSummaryFallback())
}

func (fc *FileContext) getSummaryFallback() string {
	if fc.Summary != "" {
		return fc.Summary
	}
	if fc.Content != "" {
		if len(fc.Content) > 256 {
			return fc.Content[:256]
		}
		return fc.Content
	}
	return ""
}

// EvictionPolicy dictates how the working set ejects files.
type EvictionPolicy int

const (
	EvictLRU EvictionPolicy = iota
	EvictByRelevance
)

// WorkingSet retains the active file contexts for an agent session.
type WorkingSet struct {
	mu             sync.RWMutex
	files          map[string]*FileContext
	maxSize        int
	evictionPolicy EvictionPolicy
}

// NewWorkingSet returns a working set with a bounded capacity.
func NewWorkingSet(maxSize int, policy EvictionPolicy) *WorkingSet {
	if maxSize <= 0 {
		maxSize = 12
	}
	return &WorkingSet{
		files:          make(map[string]*FileContext),
		maxSize:        maxSize,
		evictionPolicy: policy,
	}
}

// Add registers a file and evicts older entries if necessary.
func (ws *WorkingSet) Add(fc *FileContext) (evicted []string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.files[fc.Path] = fc
	if len(ws.files) <= ws.maxSize {
		return nil
	}
	return ws.evictLocked(len(ws.files) - ws.maxSize)
}

// Get retrieves a file context if present.
func (ws *WorkingSet) Get(path string) (*FileContext, bool) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	fc, ok := ws.files[path]
	return fc, ok
}

// List returns copies of tracked files.
func (ws *WorkingSet) List() []*FileContext {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	out := make([]*FileContext, 0, len(ws.files))
	for _, fc := range ws.files {
		out = append(out, fc)
	}
	return out
}

// Remove deletes a file from the working set.
func (ws *WorkingSet) Remove(path string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	delete(ws.files, path)
}

// SetMaxSize updates the capacity and evicts extra files immediately.
func (ws *WorkingSet) SetMaxSize(max int) []string {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if max <= 0 {
		max = 12
	}
	ws.maxSize = max
	if len(ws.files) <= ws.maxSize {
		return nil
	}
	return ws.evictLocked(len(ws.files) - ws.maxSize)
}

// EvictIfNeeded enforces the current limit without inserting new files.
func (ws *WorkingSet) EvictIfNeeded() []string {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if len(ws.files) <= ws.maxSize {
		return nil
	}
	return ws.evictLocked(len(ws.files) - ws.maxSize)
}

func (ws *WorkingSet) evictLocked(count int) []string {
	if count <= 0 {
		return nil
	}
	candidates := make([]*FileContext, 0, len(ws.files))
	for _, fc := range ws.files {
		if fc.Pinned {
			continue
		}
		candidates = append(candidates, fc)
	}
	if len(candidates) == 0 {
		return nil
	}
	switch ws.evictionPolicy {
	case EvictByRelevance:
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].LastAccessed.Before(candidates[j].LastAccessed)
		})
	default:
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].LastAccessed.Before(candidates[j].LastAccessed)
		})
	}
	evicted := make([]string, 0, count)
	for _, fc := range candidates {
		if count == 0 {
			break
		}
		delete(ws.files, fc.Path)
		evicted = append(evicted, fc.Path)
		count--
	}
	return evicted
}

// ContextTokenUsage exposes aggregated token consumption by category.
type ContextTokenUsage struct {
	Total     int
	BySection map[string]int
}

// SharedContext wraps Context with richer memory primitives (working set,
// summaries, compression) geared toward large-repo workflows.
type SharedContext struct {
	*Context

	contextBudget *ContextBudget
	workingSet    *WorkingSet
	summarizer    Summarizer

	mu                  sync.RWMutex
	conversationSummary string
	changeLogSummary    string
	fileItems           map[string]*fileBudgetItem
}

// NewSharedContext constructs a shared context with optional helpers. Passing
// nil uses sensible defaults for ad-hoc sessions.
func NewSharedContext(base *Context, budget *ContextBudget, summarizer Summarizer) *SharedContext {
	if base == nil {
		base = NewContext()
	}
	if budget == nil {
		budget = NewContextBudget(8192)
	}
	sc := &SharedContext{
		Context:       base,
		contextBudget: budget,
		workingSet:    NewWorkingSet(12, EvictLRU),
		summarizer:    summarizer,
		fileItems:     make(map[string]*fileBudgetItem),
	}
	if budget != nil {
		budget.AddListener(sc)
	}
	return sc
}

// AddFile inserts or updates a file in the working set and budget.
func (sc *SharedContext) AddFile(path, content, language string, level DetailLevel) (*FileContext, error) {
	if path == "" {
		return nil, fmt.Errorf("file path required")
	}
	fc := &FileContext{
		Path:         path,
		Language:     language,
		Content:      content,
		Level:        level,
		LastAccessed: time.Now().UTC(),
	}
	if fc.Summary == "" && sc.summarizer != nil {
		if summary, err := sc.summarizer.Summarize(content, SummaryConcise); err == nil {
			fc.Summary = summary
		}
	}
	sc.workingSet.Add(fc)
	item := newFileBudgetItem(fc, sc.summarizer)
	sc.fileItems[path] = item
	if sc.contextBudget != nil {
		_ = sc.contextBudget.Allocate("immediate", item.GetTokenCount(), item)
	}
	return fc, nil
}

// GetFile returns a tracked file if available.
func (sc *SharedContext) GetFile(path string) (*FileContext, bool) {
	return sc.workingSet.Get(path)
}

// EnsureFileLevel upgrades or downgrades a file to the desired detail level.
func (sc *SharedContext) EnsureFileLevel(path string, desired DetailLevel) (*FileContext, error) {
	fc, ok := sc.workingSet.Get(path)
	if !ok {
		return nil, fmt.Errorf("file %s not tracked", path)
	}
	if fc.Level <= desired {
		return fc, nil
	}
	switch desired {
	case DetailFull, DetailBodyOnly:
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		fc.Content = string(content)
		fc.Level = DetailFull
	case DetailSignature:
		if fc.Content == "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			fc.Content = string(data)
		}
		lines := linesAround(fc.Content, 40)
		fc.Content = lines
		fc.Level = DetailSignature
	default:
		if sc.summarizer != nil && fc.Content != "" {
			if summary, err := sc.summarizer.Summarize(fc.Content, SummaryMinimal); err == nil {
				fc.Summary = summary
			}
		}
		fc.Content = ""
		fc.Level = DetailSummary
	}
	if item, ok := sc.fileItems[path]; ok {
		item.refresh()
	}
	return fc, nil
}

// TouchFile bumps the recency metadata to keep the file pinned in the working set.
func (sc *SharedContext) TouchFile(path string) {
	if fc, ok := sc.workingSet.Get(path); ok {
		fc.LastAccessed = time.Now().UTC()
	}
}

// DowngradeOldFiles aggressively compresses files older than the target.
func (sc *SharedContext) DowngradeOldFiles(target DetailLevel, maxTokens int) error {
	files := sc.workingSet.List()
	sort.Slice(files, func(i, j int) bool {
		return files[i].LastAccessed.Before(files[j].LastAccessed)
	})
	saved := 0
	for _, fc := range files {
		if fc.Pinned || fc.Level >= target {
			continue
		}
		if sc.summarizer != nil && fc.Content != "" {
			if summary, err := sc.summarizer.Summarize(fc.Content, SummaryMinimal); err == nil {
				fc.Summary = summary
			}
		}
		fc.Content = ""
		fc.Level = target
		if item, ok := sc.fileItems[fc.Path]; ok {
			before := item.cachedTokens
			item.refresh()
			saved += before - item.cachedTokens
		}
		if maxTokens > 0 && saved >= maxTokens {
			break
		}
	}
	if sc.contextBudget != nil && saved > 0 {
		sc.contextBudget.Free("immediate", saved, "")
	}
	return nil
}

// GetConversationSummary returns the cached history summary.
func (sc *SharedContext) GetConversationSummary() string {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.conversationSummary
}

// GetChangeLogSummary returns a coarse summary of recent modifications.
func (sc *SharedContext) GetChangeLogSummary() string {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if sc.changeLogSummary != "" {
		return sc.changeLogSummary
	}
	return sc.conversationSummary
}

// SetChangeLogSummary lets external workflows store a higher fidelity change log.
func (sc *SharedContext) SetChangeLogSummary(summary string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.changeLogSummary = summary
}

// RefreshConversationSummary rebuilds the summary using either the latest
// compressed chunk or a fresh summarization of recent history.
func (sc *SharedContext) RefreshConversationSummary() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if len(sc.compressedHistory) > 0 {
		latest := sc.compressedHistory[len(sc.compressedHistory)-1]
		sc.conversationSummary = latest.Summary
		return
	}
	if sc.summarizer != nil && len(sc.history) > 0 {
		var builder strings.Builder
		for _, interaction := range sc.history {
			builder.WriteString(fmt.Sprintf("[%s] %s\n", interaction.Role, interaction.Content))
		}
		if summary, err := sc.summarizer.Summarize(builder.String(), SummaryConcise); err == nil {
			sc.conversationSummary = summary
			return
		}
	}
	sc.conversationSummary = ""
}

// GetTokenUsage estimates tokens consumed by files and history.
func (sc *SharedContext) GetTokenUsage() *ContextTokenUsage {
	files := sc.workingSet.List()
	usage := &ContextTokenUsage{
		BySection: make(map[string]int),
	}
	fileTokens := 0
	for _, fc := range files {
		fileTokens += fc.tokens()
	}
	historyTokens := 0
	for _, interaction := range sc.history {
		historyTokens += estimateTokens(interaction.Content)
	}
	usage.BySection["files"] = fileTokens
	usage.BySection["history"] = historyTokens
	usage.Total = fileTokens + historyTokens
	return usage
}

func linesAround(content string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 20
	}
	lines := splitLines(content)
	if len(lines) <= maxLines {
		return content
	}
	return joinLines(lines[:maxLines])
}

func splitLines(content string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lines = append(lines, content[start:i])
			start = i + 1
		}
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	out := lines[0]
	for i := 1; i < len(lines); i++ {
		out += "\n" + lines[i]
	}
	return out
}

type fileBudgetItem struct {
	file         *FileContext
	summarizer   Summarizer
	cachedID     string
	cachedTokens int
}

func newFileBudgetItem(fc *FileContext, summarizer Summarizer) *fileBudgetItem {
	item := &fileBudgetItem{
		file:       fc,
		summarizer: summarizer,
		cachedID:   fc.Path,
	}
	item.refresh()
	return item
}

func (f *fileBudgetItem) refresh() {
	f.cachedTokens = f.file.tokens()
}

func (f *fileBudgetItem) GetID() string {
	return f.cachedID
}

func (f *fileBudgetItem) GetTokenCount() int {
	return f.cachedTokens
}

func (f *fileBudgetItem) GetPriority() int {
	recency := int(time.Since(f.file.LastAccessed).Minutes())
	if recency < 0 {
		recency = 0
	}
	return recency + int(f.file.Level)*10
}

func (f *fileBudgetItem) CanCompress() bool {
	return f.file.Level < DetailSummary && !f.file.Pinned
}

func (f *fileBudgetItem) Compress() (BudgetItem, error) {
	if !f.CanCompress() {
		return f, nil
	}
	if f.summarizer != nil && f.file.Content != "" {
		if summary, err := f.summarizer.Summarize(f.file.Content, SummaryMinimal); err == nil {
			f.file.Summary = summary
		}
	}
	f.file.Content = ""
	f.file.Level = DetailSummary
	f.refresh()
	return f, nil
}

func (f *fileBudgetItem) CanEvict() bool {
	return !f.file.Pinned
}

// OnBudgetWarning responds to context budget pressure by downgrading older
// files to summaries so higher-priority snippets can fit in the prompt.
func (sc *SharedContext) OnBudgetWarning(_ float64) {
	_ = sc.DowngradeOldFiles(DetailSummary, 256)
}

// OnBudgetExceeded satisfies the BudgetListener interface; no-op.
func (sc *SharedContext) OnBudgetExceeded(string, int, int) {}

// OnCompression records that compression happened; no-op for shared context.
func (sc *SharedContext) OnCompression(string, int) {}
