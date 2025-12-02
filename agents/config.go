package agents

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const configDirName = "relurpify_cfg"

// ConfigDir returns the workspace-local configuration directory.
func ConfigDir(workspace string) string {
	if workspace == "" {
		workspace = "."
	}
	return filepath.Join(workspace, configDirName)
}

// GlobalConfig matches relurpify_cfg/config.yaml inside the workspace.
type GlobalConfig struct {
	Version      string            `yaml:"version"`
	DefaultModel ModelRef          `yaml:"default_model"`
	Models       []ModelRef        `yaml:"models"`
	Permissions  map[string]string `yaml:"permissions"`
	AgentPaths   []string          `yaml:"agent_paths"`
	Features     FeatureFlags      `yaml:"features"`
	Context      ContextConfig     `yaml:"context"`
	Logging      LoggingConfig     `yaml:"logging"`
}

// ModelRef enumerates available models.
type ModelRef struct {
	Name        string  `yaml:"name"`
	Description string  `yaml:"description"`
	Provider    string  `yaml:"provider"`
	Temperature float64 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
}

// FeatureFlags toggles runtime capabilities.
type FeatureFlags struct {
	AutoSave       bool `yaml:"auto_save"`
	AutoFormat     bool `yaml:"auto_format"`
	ShowThinking   bool `yaml:"show_thinking"`
	ParallelAgents bool `yaml:"parallel_agents"`
	MaxConcurrent  int  `yaml:"max_concurrent"`
}

// ContextConfig controls shared context.
type ContextConfig struct {
	MaxHistoryMessages int  `yaml:"max_history_messages"`
	MaxFilesInContext  int  `yaml:"max_files_in_context"`
	UseEmbeddings      bool `yaml:"use_embeddings"`
}

// LoggingConfig describes log output.
type LoggingConfig struct {
	Level string `yaml:"level"`
	File  string `yaml:"file"`
	LLM   bool   `yaml:"llm_debug"`
	Agent bool   `yaml:"agent_debug"`
}

// DefaultConfigPath returns relurpify_cfg/config.yaml within the workspace.
func DefaultConfigPath(workspace string) string {
	return filepath.Join(ConfigDir(workspace), "config.yaml")
}

// DefaultAgentPaths returns the canonical search paths rooted in relurpify_cfg.
func DefaultAgentPaths(workspace string) []string {
	return []string{filepath.Join(ConfigDir(workspace), "agents")}
}

// LoadGlobalConfig loads the config or returns defaults when missing.
func LoadGlobalConfig(path, workspace string) (*GlobalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &GlobalConfig{
				Version:     "1.0.0",
				AgentPaths:  DefaultAgentPaths(workspace),
				Permissions: map[string]string{"file_write": "ask", "file_edit": "ask", "bash_execute": "ask", "file_delete": "deny"},
			}, nil
		}
		return nil, err
	}
	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if len(cfg.AgentPaths) == 0 {
		cfg.AgentPaths = DefaultAgentPaths(workspace)
	}
	return &cfg, nil
}

// SaveGlobalConfig writes the config to disk.
func SaveGlobalConfig(path string, cfg *GlobalConfig) error {
	if cfg == nil {
		return errors.New("config missing")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// AgentSearchPaths resolves agent paths for the registry.
func (c *GlobalConfig) AgentSearchPaths(workspace string) []string {
	if c == nil {
		return DefaultAgentPaths(workspace)
	}
	if len(c.AgentPaths) == 0 {
		return DefaultAgentPaths(workspace)
	}
	resolved := make([]string, 0, len(c.AgentPaths))
	for _, path := range c.AgentPaths {
		resolved = append(resolved, expandPath(path, workspace))
	}
	return resolved
}

// expandPath resolves ~ and workspace-relative paths into absolute paths while
// leaving already absolute entries untouched.
func expandPath(path, workspace string) string {
	if path == "" {
		return path
	}
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if strings.HasPrefix(path, ".") {
		return filepath.Join(workspace, path)
	}
	return path
}
