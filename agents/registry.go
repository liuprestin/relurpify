package agents

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// RegistryOptions configures the agent discovery behavior.
type RegistryOptions struct {
	Workspace string
	Paths     []string
	RulesPath string
}

// Registry tracks loaded manifests and supports hot reloading.
type Registry struct {
	opts    RegistryOptions
	mu      sync.RWMutex
	agents  map[string]*framework.AgentManifest
	watchCh []chan struct{}
	rules   *Ruleset
	loaded  time.Time
}

// NewRegistry builds an empty registry.
func NewRegistry(opts RegistryOptions) *Registry {
	return &Registry{
		opts:   opts,
		agents: make(map[string]*framework.AgentManifest),
	}
}

// Load scans the configured directories for manifests.
func (r *Registry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents = make(map[string]*framework.AgentManifest)
	for _, dir := range r.searchPaths() {
		r.loadDir(dir)
	}
	if r.opts.RulesPath != "" {
		if rules, err := LoadRuleset(r.opts.RulesPath); err == nil {
			r.rules = rules
		}
	}
	r.loaded = time.Now()
	r.broadcast()
	return nil
}

// Reload rescans the filesystem and notifies subscribers.
func (r *Registry) Reload() error {
	return r.Load()
}

// List returns summaries of available agents.
func (r *Registry) List() []AgentSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	summaries := make([]AgentSummary, 0, len(r.agents))
	for _, manifest := range r.agents {
		summaries = append(summaries, summarizeManifest(manifest, r.opts.Workspace))
	}
	return summaries
}

// Get retrieves a manifest by name.
func (r *Registry) Get(name string) (*framework.AgentManifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	manifest, ok := r.agents[name]
	return manifest, ok
}

// Rules returns the project ruleset when available.
func (r *Registry) Rules() *Ruleset {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rules
}

// Watch registers a listener notified on reload events.
func (r *Registry) Watch() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan struct{}, 1)
	r.watchCh = append(r.watchCh, ch)
	return ch
}

func (r *Registry) broadcast() {
	for _, ch := range r.watchCh {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (r *Registry) loadDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if manifest, err := framework.LoadAgentManifest(path); err == nil {
			r.agents[manifest.Metadata.Name] = manifest
		}
	}
}

func (r *Registry) searchPaths() []string {
	paths := r.opts.Paths
	if len(paths) == 0 {
		paths = DefaultAgentPaths(r.opts.Workspace)
	}
	set := make(map[string]struct{})
	var resolved []string
	for _, path := range paths {
		path = expandPath(path, r.opts.Workspace)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if _, exists := set[path]; exists {
			continue
		}
		set[path] = struct{}{}
		resolved = append(resolved, path)
	}
	return resolved
}

// StartWatcher polls for filesystem changes.
func (r *Registry) StartWatcher(stop <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		last := time.Time{}
		for {
			select {
			case <-ticker.C:
				info := r.snapshot()
				if info.After(last) {
					_ = r.Load()
					last = info
				}
			case <-stop:
				return
			}
		}
	}()
}

func (r *Registry) snapshot() time.Time {
	paths := r.searchPaths()
	var newest time.Time
	for _, path := range paths {
		filepath.WalkDir(path, func(current string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if ts := fetchModTime(current); ts.After(newest) {
				newest = ts
			}
			return nil
		})
	}
	return newest
}

func fetchModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// ErrAgentNotFound indicates lookup failure.
var ErrAgentNotFound = errors.New("agent not found")

// AgentSummary is a lightweight view of available manifests.
type AgentSummary struct {
	Name        string
	Description string
	Mode        framework.AgentMode
	Model       string
	Source      string
}

func summarizeManifest(m *framework.AgentManifest, workspace string) AgentSummary {
	source := m.SourcePath
	if workspace != "" {
		if rel, err := filepath.Rel(workspace, source); err == nil {
			source = rel
		}
	}
	var mode framework.AgentMode
	var modelName string
	if m.Spec.Agent != nil {
		mode = m.Spec.Agent.Mode
		modelName = m.Spec.Agent.Model.Name
	}
	return AgentSummary{
		Name:        m.Metadata.Name,
		Description: m.Metadata.Description,
		Mode:        mode,
		Model:       modelName,
		Source:      source,
	}
}
