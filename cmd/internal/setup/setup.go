package setup

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lexcodex/relurpify/cmd/internal/cliutils"
)

// Config captures the auto-detected environment so other tools can share it.
type Config struct {
	Workspace   string       `json:"workspace"`
	LastUpdated time.Time    `json:"last_updated"`
	Ollama      OllamaConfig `json:"ollama"`
	LSPServers  []LSPServer  `json:"lsp_servers"`
}

// OllamaConfig holds the current Ollama environment snapshot.
type OllamaConfig struct {
	Endpoint        string   `json:"endpoint"`
	CommandPath     string   `json:"command_path,omitempty"`
	Reachable       bool     `json:"reachable"`
	AvailableModels []string `json:"available_models"`
	SelectedModel   string   `json:"selected_model"`
	LastError       string   `json:"last_error,omitempty"`
}

// LSPServer stores availability for a supported language server.
type LSPServer struct {
	ID               string   `json:"id"`
	Language         string   `json:"language"`
	Extensions       []string `json:"extensions"`
	Commands         []string `json:"commands"`
	Available        bool     `json:"available"`
	CommandPath      string   `json:"command_path,omitempty"`
	WorkspaceMatches int      `json:"workspace_matches"`
}

// DefaultConfigPath returns the default workspace config path.
func DefaultConfigPath(workspace string) string {
	if workspace == "" {
		workspace = "."
	}
	return filepath.Join(workspace, ".relurpify", "config.json")
}

// LoadConfig reads the JSON config.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveConfig writes the config JSON, creating parent dirs as needed.
func SaveConfig(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Detect builds a Config snapshot for the workspace.
func Detect(workspace, endpoint, defaultModel string, prev *Config) (*Config, error) {
	lspServers, err := detectLSPServers(workspace)
	if err != nil {
		return nil, err
	}
	ollama := detectOllama(endpoint, defaultModel, prev)
	cfg := &Config{
		Workspace:   workspace,
		LastUpdated: time.Now(),
		Ollama:      ollama,
		LSPServers:  lspServers,
	}
	return cfg, nil
}

// ModelOrDefault returns the chosen model or fallback.
func (c *Config) ModelOrDefault(fallback string) string {
	if c == nil {
		return fallback
	}
	if c.Ollama.SelectedModel != "" {
		return c.Ollama.SelectedModel
	}
	return fallback
}

// HasModel reports if the config knows the model.
func (c *Config) HasModel(name string) bool {
	if c == nil {
		return false
	}
	for _, m := range c.Ollama.AvailableModels {
		if m == name {
			return true
		}
	}
	return false
}

// SetSelectedModel switches the model if available.
func (c *Config) SetSelectedModel(name string) bool {
	if c == nil {
		return false
	}
	if name == "" {
		return false
	}
	if c.HasModel(name) {
		c.Ollama.SelectedModel = name
		return true
	}
	return false
}

func detectOllama(endpoint, defaultModel string, prev *Config) OllamaConfig {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	binPath, _ := exec.LookPath("ollama")
	availableModels, reachable, lastErr := fetchOllamaModels(endpoint)
	selected := selectModel(defaultModel, availableModels, prev)
	return OllamaConfig{
		Endpoint:        endpoint,
		CommandPath:     binPath,
		Reachable:       reachable,
		AvailableModels: availableModels,
		SelectedModel:   selected,
		LastError:       lastErr,
	}
}

func fetchOllamaModels(endpoint string) ([]string, bool, string) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(endpoint, "/")+"/api/tags", nil)
	if err != nil {
		return nil, false, err.Error()
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, false, resp.Status
	}
	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, false, err.Error()
	}
	models := make([]string, 0, len(payload.Models))
	for _, m := range payload.Models {
		if m.Name != "" {
			models = append(models, m.Name)
		}
	}
	sort.Strings(models)
	return models, true, ""
}

func selectModel(defaultModel string, models []string, prev *Config) string {
	if prev != nil && prev.Ollama.SelectedModel != "" {
		if len(models) == 0 || contains(models, prev.Ollama.SelectedModel) {
			return prev.Ollama.SelectedModel
		}
	}
	if defaultModel != "" && contains(models, defaultModel) {
		return defaultModel
	}
	if len(models) > 0 {
		return models[0]
	}
	if defaultModel != "" {
		return defaultModel
	}
	return "codellama"
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func detectLSPServers(workspace string) ([]LSPServer, error) {
	descriptors := cliutils.CanonicalLSPDescriptors()
	counts, err := scanExtensions(workspace)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(descriptors))
	for id := range descriptors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	servers := make([]LSPServer, 0, len(ids))
	for _, id := range ids {
		desc := descriptors[id]
		commandPath := findCommand(desc.Commands)
		matches := 0
		for _, ext := range desc.Extensions {
			matches += counts[strings.ToLower(ext)]
		}
		servers = append(servers, LSPServer{
			ID:               id,
			Language:         id,
			Extensions:       desc.Extensions,
			Commands:         desc.Commands,
			Available:        commandPath != "",
			CommandPath:      commandPath,
			WorkspaceMatches: matches,
		})
	}
	return servers, nil
}

func findCommand(candidates []string) string {
	for _, cmd := range candidates {
		if cmd == "" {
			continue
		}
		if path, err := exec.LookPath(cmd); err == nil {
			return path
		}
	}
	return ""
}

func scanExtensions(workspace string) (map[string]int, error) {
	counts := map[string]int{}
	if workspace == "" {
		workspace = "."
	}
	info, err := os.Stat(workspace)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return counts, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return counts, nil
	}
	skipDirs := map[string]bool{
		".git":         true,
		".gomodcache":  true,
		".gocache":     true,
		".idea":        true,
		".vscode":      true,
		"node_modules": true,
		"vendor":       true,
		".relurpify":   true,
		".trash":       true,
	}
	err = filepath.WalkDir(workspace, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(d.Name())), ".")
		if ext == "" {
			return nil
		}
		counts[ext]++
		return nil
	})
	if err != nil {
		return nil, err
	}
	return counts, nil
}
