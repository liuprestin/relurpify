package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lexcodex/relurpify/framework"
	"gopkg.in/yaml.v3"
)

// Config captures every knob shared across the relurpish CLI, TUI, and server
// entry points. Keeping it as a lightweight struct makes it trivial to reuse in
// tests or future headless workflows.
type Config struct {
	Workspace      string
	ManifestPath   string
	MemoryPath     string
	LogPath        string
	ConfigPath     string
	OllamaEndpoint string
	OllamaModel    string
	AgentName      string
	ServerAddr     string
	Sandbox        framework.SandboxConfig
	AuditLimit     int
	HITLTimeout    time.Duration
}

// DefaultConfig infers sensible defaults based on the current working
// directory. Errors from os.Getwd are ignored so callers can override manually.
func DefaultConfig() Config {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return Config{
		Workspace:    cwd,
		ManifestPath: filepath.Join(cwd, "agent.manifest.yaml"),
		MemoryPath:   filepath.Join(cwd, ".relurpish", "memory"),
		LogPath:      filepath.Join(cwd, ".relurpish", "relurpish.log"),
		ConfigPath:   filepath.Join(cwd, ".relurpish", "config.yaml"),
		OllamaModel:  "deepseek-r1:7b",
		ServerAddr:   ":8080",
		AuditLimit:   512,
		HITLTimeout:  45 * time.Second,
		Sandbox: framework.SandboxConfig{
			RunscPath:        "runsc",
			ContainerRuntime: "docker",
			Platform:         "",
			NetworkIsolation: true,
			ReadOnlyRoot:     true,
		},
	}
}

// Normalize ensures every filesystem path is absolute and fills missing
// defaults so runtime initialization never has to re-check the same invariants.
func (c *Config) Normalize() error {
	if c.Workspace == "" {
		return fmt.Errorf("workspace path required")
	}
	absWorkspace, err := filepath.Abs(c.Workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}
	c.Workspace = absWorkspace
	if c.ManifestPath == "" {
		c.ManifestPath = filepath.Join(c.Workspace, "agent.manifest.yaml")
	}
	if !filepath.IsAbs(c.ManifestPath) {
		c.ManifestPath = filepath.Join(c.Workspace, c.ManifestPath)
	}
	if c.MemoryPath == "" {
		c.MemoryPath = filepath.Join(c.Workspace, ".relurpish", "memory")
	}
	if !filepath.IsAbs(c.MemoryPath) {
		c.MemoryPath = filepath.Join(c.Workspace, c.MemoryPath)
	}
	if c.LogPath == "" {
		c.LogPath = filepath.Join(c.Workspace, ".relurpish", "relurpish.log")
	}
	if !filepath.IsAbs(c.LogPath) {
		c.LogPath = filepath.Join(c.Workspace, c.LogPath)
	}
	if c.ConfigPath == "" {
		c.ConfigPath = filepath.Join(c.Workspace, ".relurpish", "config.yaml")
	}
	if !filepath.IsAbs(c.ConfigPath) {
		c.ConfigPath = filepath.Join(c.Workspace, c.ConfigPath)
	}
	if c.AgentName == "" {
		c.AgentName = "coding"
	}
	if c.OllamaEndpoint == "" {
		c.OllamaEndpoint = "http://localhost:11434"
	}
	if c.OllamaModel == "" {
		c.OllamaModel = "deepseek-r1:7b"
	}
	if c.ServerAddr == "" {
		c.ServerAddr = ":8080"
	}
	if c.AuditLimit <= 0 {
		c.AuditLimit = 256
	}
	if c.HITLTimeout <= 0 {
		c.HITLTimeout = 30 * time.Second
	}
	return nil
}

// AgentLabel returns the normalized agent identifier used across telemetry and
// UI views.
func (c Config) AgentLabel() string {
	switch c.AgentName {
	case "planner", "react", "reflection", "manual", "expert":
		return c.AgentName
	case "coding", "coder":
		return "coding"
	default:
		return "coding"
	}
}

// WorkspaceConfig captures persisted wizard selections for reuse across runs.
type WorkspaceConfig struct {
	Model             string            `yaml:"model"`
	Agents            []string          `yaml:"agents"`
	AllowedTools      []string          `yaml:"allowed_tools"`
	PermissionProfile PermissionProfile `yaml:"permission_profile"`
	LastUpdated       int64             `yaml:"last_updated"`
}

// LoadWorkspaceConfig loads the wizard configuration from disk. Missing files
// are treated as empty selections.
func LoadWorkspaceConfig(path string) (WorkspaceConfig, error) {
	if path == "" {
		return WorkspaceConfig{}, fmt.Errorf("config path required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return WorkspaceConfig{}, err
	}
	var cfg WorkspaceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return WorkspaceConfig{}, err
	}
	return cfg, nil
}

// SaveWorkspaceConfig persists selections for future sessions.
func SaveWorkspaceConfig(path string, cfg WorkspaceConfig) error {
	if path == "" {
		return fmt.Errorf("config path required")
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
