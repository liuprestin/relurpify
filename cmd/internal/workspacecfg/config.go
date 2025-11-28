package workspacecfg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lexcodex/relurpify/framework"
	"gopkg.in/yaml.v3"
)

// WorkspaceConfig models the persisted workspace metadata.
type WorkspaceConfig struct {
	Workspace     string        `json:"workspace"`
	DefaultAgent  string        `json:"default_agent"`
	Agents        []AgentConfig `json:"agents"`
	AllowedTools  []string      `json:"allowed_tools"`
	Prerequisites []string      `json:"prerequisites"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// AgentConfig ties an agent entry to its manifest.
type AgentConfig struct {
	Name        string `json:"name"`
	Manifest    string `json:"manifest"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

// ConfigDir resolves the directory storing workspace settings.
func ConfigDir(workspace string) string {
	if workspace == "" {
		workspace = "."
	}
	return filepath.Join(workspace, "relurpify_config")
}

// WorkspaceFile returns the workspace JSON path.
func WorkspaceFile(workspace string) string {
	return filepath.Join(ConfigDir(workspace), "workspace.json")
}

// Load reads the workspace configuration if present.
func Load(workspace string) (*WorkspaceConfig, error) {
	path := WorkspaceFile(workspace)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg WorkspaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Workspace == "" {
		cfg.Workspace = workspace
	}
	return &cfg, nil
}

// Save writes the configuration back to disk.
func Save(cfg *WorkspaceConfig) error {
	if cfg == nil {
		return errors.New("workspace config missing")
	}
	if cfg.Workspace == "" {
		return errors.New("workspace path missing")
	}
	dir := ConfigDir(cfg.Workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := WorkspaceFile(cfg.Workspace)
	cfg.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// AgentManifestPath returns the absolute manifest path for an agent.
func AgentManifestPath(workspace, agentName string) string {
	agentName = strings.ToLower(strings.TrimSpace(agentName))
	if agentName == "" {
		agentName = "agent"
	}
	return filepath.Join(ConfigDir(workspace), "agents", fmt.Sprintf("%s.manifest.yaml", agentName))
}

// EnsureManifests writes default manifests for any missing agents.
func EnsureManifests(cfg *WorkspaceConfig) error {
	if cfg == nil {
		return errors.New("workspace config missing")
	}
	for i, agent := range cfg.Agents {
		if !agent.Enabled {
			continue
		}
		path := agent.Manifest
		if path == "" {
			path = AgentManifestPath(cfg.Workspace, agent.Name)
			cfg.Agents[i].Manifest = path
		}
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		manifest := defaultManifest(agent.Name)
		data, err := yaml.Marshal(manifest)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return Save(cfg)
}

func defaultManifest(agentName string) *framework.AgentManifest {
	return &framework.AgentManifest{
		APIVersion: "agent.framework/v1",
		Kind:       "Agent",
		Metadata: framework.ManifestMetadata{
			Name:    agentName,
			Version: "1.0.0",
		},
		Spec: framework.ManifestSpec{
			Image:   "relurpify/agent:latest",
			Runtime: "gvisor",
			Permissions: framework.PermissionSet{
				FileSystem: []framework.FileSystemPermission{
					{Action: framework.FileSystemRead, Path: "${workspace}/**", Justification: "Read workspace"},
					{Action: framework.FileSystemWrite, Path: "${workspace}/**", Justification: "Modify workspace"},
					{Action: framework.FileSystemList, Path: "${workspace}/**", Justification: "List workspace"},
					{Action: framework.FileSystemExecute, Path: "${workspace}/**", Justification: "Execute binaries"},
				},
				Executables: []framework.ExecutablePermission{
					{Binary: "bash", Args: []string{"*"}},
					{Binary: "sh", Args: []string{"*"}},
				},
				Network:      []framework.NetworkPermission{},
				Capabilities: []framework.CapabilityPermission{},
				IPC:          []framework.IPCPermission{},
			},
			Resources: framework.ResourceSpec{
				Limits: framework.ResourceLimit{
					CPU:    "2",
					Memory: "4Gi",
					DiskIO: "100Mi/s",
				},
			},
			Security: framework.SecuritySpec{
				RunAsUser:       1000,
				ReadOnlyRoot:    true,
				NoNewPrivileges: true,
			},
			Audit: framework.AuditSpec{
				Level:         "detailed",
				RetentionDays: 90,
			},
		},
	}
}

// ManifestForAgent returns the manifest path for the provided agent.
func (w *WorkspaceConfig) ManifestForAgent(name string) (string, bool) {
	if w == nil {
		return "", false
	}
	if name == "" {
		name = w.DefaultAgent
	}
	for _, agent := range w.Agents {
		if strings.EqualFold(agent.Name, name) && agent.Enabled {
			return agent.Manifest, true
		}
	}
	return "", false
}

// RestrictRegistry filters the registry to the allowed tool list.
func RestrictRegistry(registry *framework.ToolRegistry, allowed []string) {
	if registry == nil || len(allowed) == 0 {
		return
	}
	registry.RestrictTo(allowed)
}

// PrereqStatus captures the status of a prerequisite probe.
type PrereqStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Details string `json:"details,omitempty"`
}

// CheckPrerequisites probes well-known prerequisites.
func CheckPrerequisites(ctx context.Context, cfg *WorkspaceConfig) []PrereqStatus {
	var results []PrereqStatus
	if cfg == nil || len(cfg.Prerequisites) == 0 {
		return results
	}
	for _, item := range cfg.Prerequisites {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "" {
			continue
		}
		switch name {
		case "ollama":
			results = append(results, checkOllama(ctx))
		case "docker":
			results = append(results, checkBinary("docker"))
		case "runsc":
			results = append(results, checkBinary("runsc"))
		default:
			results = append(results, PrereqStatus{Name: name, Status: "unknown", Details: "no checker available"})
		}
	}
	return results
}

func checkBinary(bin string) PrereqStatus {
	_, err := exec.LookPath(bin)
	if err != nil {
		return PrereqStatus{Name: bin, Status: "missing", Details: err.Error()}
	}
	return PrereqStatus{Name: bin, Status: "ok"}
}

func checkOllama(ctx context.Context) PrereqStatus {
	status := checkBinary("ollama")
	if status.Status != "ok" {
		return status
	}
	client := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:11434/api/tags", nil)
	resp, err := client.Do(req)
	if err != nil {
		status.Status = "unreachable"
		status.Details = err.Error()
		return status
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		status.Status = "unreachable"
		status.Details = resp.Status
		return status
	}
	status.Details = "endpoint reachable"
	return status
}
