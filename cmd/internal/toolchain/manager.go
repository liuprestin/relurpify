package toolchain

import (
	"fmt"
	"strings"
	"sync"

	"github.com/lexcodex/relurpify/cmd/internal/cliutils"
	"github.com/lexcodex/relurpify/cmd/internal/setup"
	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/tools"
)

// Manager tracks long-lived tooling such as LSP proxies so shell sessions can reuse them.
type Manager struct {
	workspace string
	supported map[string]setup.LSPServer

	mu       sync.RWMutex
	proxies  map[string]*tools.Proxy
	cleanups map[string]func()
}

// NewManager warms up LSP servers for the provided workspace and descriptor set.
func NewManager(workspace string, servers []setup.LSPServer) (*Manager, error) {
	m := &Manager{
		workspace: workspace,
		supported: make(map[string]setup.LSPServer, len(servers)),
		proxies:   map[string]*tools.Proxy{},
		cleanups:  map[string]func(){},
	}
	for _, srv := range servers {
		m.supported[srv.ID] = srv
	}
	return m, nil
}

// BuildRegistry constructs a tool registry that reuses the manager's proxies. Optional language
// hints ensure specific proxies are available for the request.
func (m *Manager) BuildRegistry(languages ...string) (*framework.ToolRegistry, error) {
	for _, lang := range languages {
		if lang == "" {
			continue
		}
		if _, err := m.ensureProxy(lang); err != nil {
			return nil, err
		}
	}
	registry := cliutils.BuildToolRegistry(m.workspace)
	for _, proxy := range m.allProxies() {
		cliutils.RegisterLSPTools(registry, proxy)
	}
	return registry, nil
}

// WarmLanguages attempts to start proxies for the provided languages, ignoring unavailable servers.
func (m *Manager) WarmLanguages(languages []string) error {
	if len(languages) == 0 {
		return nil
	}
	var errs []string
	for _, lang := range uniqueLangs(languages) {
		srv, ok := m.supported[lang]
		if !ok || !srv.Available {
			continue
		}
		if _, err := m.ensureProxy(lang); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", lang, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}

// EnsureLanguage makes sure a proxy exists for the requested language.
func (m *Manager) EnsureLanguage(language string) error {
	if language == "" {
		return nil
	}
	_, err := m.ensureProxy(language)
	return err
}

// ActiveLanguages lists LSP languages that currently have running proxies.
func (m *Manager) ActiveLanguages() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.proxies))
	for lang := range m.proxies {
		names = append(names, lang)
	}
	return names
}

// Describe returns metadata suitable for storing in framework.Context.
func (m *Manager) Describe() map[string]any {
	langs := m.ActiveLanguages()
	supported := m.supportedLanguages()
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := make([]map[string]any, 0, len(m.proxies))
	for lang, proxy := range m.proxies {
		status = append(status, map[string]any{
			"language": lang,
			"proxy":    fmt.Sprintf("%T", proxy),
		})
	}
	return map[string]any{
		"workspace": m.workspace,
		"languages": langs,
		"supported": supported,
		"services":  status,
	}
}

// Close shuts down every tracked proxy.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, cleanup := range m.cleanups {
		if cleanup != nil {
			cleanup()
		}
		delete(m.cleanups, key)
		delete(m.proxies, key)
	}
}

// SupportsLanguage reports whether detection saw the language/server combination.
func (m *Manager) SupportsLanguage(language string) bool {
	if language == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.supported[language]
	return ok
}

func (m *Manager) supportedLanguages() []string {
	langs := make([]string, 0, len(m.supported))
	for lang, srv := range m.supported {
		if srv.Available {
			langs = append(langs, lang)
		}
	}
	return langs
}

func (m *Manager) ensureProxy(language string) (*tools.Proxy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureProxyLocked(language)
}

func (m *Manager) ensureProxyLocked(language string) (*tools.Proxy, error) {
	if language == "" {
		return nil, fmt.Errorf("language is required")
	}
	if proxy, ok := m.proxies[language]; ok {
		return proxy, nil
	}
	proxy, cleanup, err := cliutils.NewProxyForLanguage(language, m.workspace)
	if err != nil {
		return nil, err
	}
	if proxy != nil {
		m.proxies[language] = proxy
		m.cleanups[language] = cleanup
	}
	return proxy, nil
}

func (m *Manager) allProxies() []*tools.Proxy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*tools.Proxy, 0, len(m.proxies))
	for _, proxy := range m.proxies {
		list = append(list, proxy)
	}
	return list
}

func uniqueLangs(langs []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(langs))
	for _, lang := range langs {
		lang = strings.ToLower(strings.TrimSpace(lang))
		if lang == "" {
			continue
		}
		if _, ok := seen[lang]; ok {
			continue
		}
		seen[lang] = struct{}{}
		out = append(out, lang)
	}
	return out
}
