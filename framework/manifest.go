package framework

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentManifest defines the security contract for an agent.
type AgentManifest struct {
	APIVersion string           `yaml:"apiVersion" json:"apiVersion"`
	Kind       string           `yaml:"kind" json:"kind"`
	Metadata   ManifestMetadata `yaml:"metadata" json:"metadata"`
	Spec       ManifestSpec     `yaml:"spec" json:"spec"`
	SourcePath string           `yaml:"-" json:"-"`
}

// ManifestMetadata describes identity fields.
type ManifestMetadata struct {
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// ManifestSpec encodes runtime, permission, resource, and security sections.
type ManifestSpec struct {
	Image       string            `yaml:"image" json:"image"`
	Runtime     string            `yaml:"runtime" json:"runtime"`
	Permissions PermissionSet     `yaml:"permissions" json:"permissions"`
	Resources   ResourceSpec      `yaml:"resources" json:"resources"`
	Security    SecuritySpec      `yaml:"security" json:"security"`
	Audit       AuditSpec         `yaml:"audit" json:"audit"`
	Agent       *AgentRuntimeSpec `yaml:"agent,omitempty" json:"agent,omitempty"`
}

// ResourceSpec declares resource limits.
type ResourceSpec struct {
	Limits ResourceLimit `yaml:"limits" json:"limits"`
}

// ResourceLimit tracks CPU/memory/disk quotas.
type ResourceLimit struct {
	CPU     string `yaml:"cpu" json:"cpu"`
	Memory  string `yaml:"memory" json:"memory"`
	DiskIO  string `yaml:"disk_io" json:"disk_io"`
	Network string `yaml:"network,omitempty" json:"network,omitempty"`
}

// SecuritySpec enumerates container security toggles.
type SecuritySpec struct {
	RunAsUser       int  `yaml:"run_as_user" json:"run_as_user"`
	ReadOnlyRoot    bool `yaml:"read_only_root" json:"read_only_root"`
	NoNewPrivileges bool `yaml:"no_new_privileges" json:"no_new_privileges"`
}

// AuditSpec configures audit verbosity.
type AuditSpec struct {
	Level         string `yaml:"level" json:"level"`
	RetentionDays int    `yaml:"retention_days" json:"retention_days"`
}

// LoadAgentManifest parses and validates a manifest file.
func LoadAgentManifest(path string) (*AgentManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest AgentManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	manifest.SourcePath = path
	return &manifest, nil
}

// Validate enforces manifest semantics.
func (m *AgentManifest) Validate() error {
	if m.APIVersion == "" {
		return fmt.Errorf("manifest missing apiVersion")
	}
	if m.Kind == "" {
		return fmt.Errorf("manifest missing kind")
	}
	if m.Metadata.Name == "" {
		return fmt.Errorf("manifest missing metadata.name")
	}
	if m.Spec.Image == "" {
		return fmt.Errorf("manifest missing spec.image")
	}
	if strings.ToLower(m.Spec.Runtime) != "gvisor" {
		return fmt.Errorf("runtime must be gVisor, got %s", m.Spec.Runtime)
	}
	if err := m.Spec.Permissions.Validate(); err != nil {
		return fmt.Errorf("permissions invalid: %w", err)
	}
	if m.Spec.Agent != nil {
		if err := m.Spec.Agent.Validate(); err != nil {
			return fmt.Errorf("agent spec invalid: %w", err)
		}
	}
	return nil
}

// AgentRuntimeSpec describes CLI/runtime level configuration derived from the
// manifest. These fields are optional from the sandbox point of view but
// provide the additional metadata needed by the orchestrator.
type AgentRuntimeSpec struct {
	Implementation    string               `yaml:"implementation" json:"implementation"` // e.g. "react", "planner", "coding"
	Mode              AgentMode            `yaml:"mode" json:"mode"`
	Version           string               `yaml:"version,omitempty" json:"version,omitempty"`
	Prompt            string               `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Model             AgentModelConfig     `yaml:"model" json:"model"`
	Tools             AgentToolMatrix      `yaml:"tools" json:"tools"`
	Bash              AgentBashPermissions `yaml:"bash_permissions,omitempty" json:"bash_permissions,omitempty"`
	Files             AgentFileMatrix      `yaml:"file_permissions,omitempty" json:"file_permissions,omitempty"`
	Invocation        AgentInvocationSpec  `yaml:"invocation,omitempty" json:"invocation,omitempty"`
	Context           AgentContextSpec     `yaml:"context,omitempty" json:"context,omitempty"`
	LSP               AgentLSPSpec         `yaml:"lsp,omitempty" json:"lsp,omitempty"`
	Search            AgentSearchSpec      `yaml:"search,omitempty" json:"search,omitempty"`
	Metadata          AgentMetadata        `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	OllamaToolCalling *bool                `yaml:"ollama_tool_calling,omitempty" json:"ollama_tool_calling,omitempty"`
	Logging           *AgentLoggingSpec    `yaml:"logging,omitempty" json:"logging,omitempty"`
}

// AgentLSPSpec configures Language Server Protocol features.
type AgentLSPSpec struct {
	Servers map[string]string `yaml:"servers" json:"servers"` // "go": "gopls", "python": "pyright"
	Enabled bool              `yaml:"enabled" json:"enabled"`
	Timeout string            `yaml:"timeout" json:"timeout"`
}

// AgentSearchSpec configures search/indexing capabilities.
type AgentSearchSpec struct {
	HybridEnabled bool `yaml:"hybrid_enabled" json:"hybrid_enabled"` // Use both vector and AST
	VectorIndex   bool `yaml:"vector_index" json:"vector_index"`
	ASTIndex      bool `yaml:"ast_index" json:"ast_index"`
}

// ToolCallingEnabled reports whether Ollama tool calling should be used.
func (a *AgentRuntimeSpec) ToolCallingEnabled() bool {
	if a == nil || a.OllamaToolCalling == nil {
		return true
	}
	return *a.OllamaToolCalling
}

// AgentLoggingSpec controls debug logging toggles for the agent.
type AgentLoggingSpec struct {
	LLM   *bool `yaml:"llm,omitempty" json:"llm,omitempty"`
	Agent *bool `yaml:"agent,omitempty" json:"agent,omitempty"`
}

// AgentMode categorizes the manifest mode.
type AgentMode string

const (
	AgentModePrimary AgentMode = "primary"
	AgentModeSub     AgentMode = "subagent"
	AgentModeSystem  AgentMode = "system"
)

// AgentModelConfig describes an LLM backing the agent.
type AgentModelConfig struct {
	Provider    string  `yaml:"provider" json:"provider"`
	Name        string  `yaml:"name" json:"name"`
	Temperature float64 `yaml:"temperature" json:"temperature"`
	MaxTokens   int     `yaml:"max_tokens" json:"max_tokens"`
}

// AgentToolMatrix encodes coarse permissions for builtin tools.
type AgentToolMatrix struct {
	FileRead       bool `yaml:"file_read" json:"file_read"`
	FileWrite      bool `yaml:"file_write" json:"file_write"`
	FileEdit       bool `yaml:"file_edit" json:"file_edit"`
	BashExecute    bool `yaml:"bash_execute" json:"bash_execute"`
	LSPQuery       bool `yaml:"lsp_query" json:"lsp_query"`
	SearchCodebase bool `yaml:"search_codebase" json:"search_codebase"`
	WebSearch      bool `yaml:"web_search" json:"web_search"`
}

// AgentBashPermissions constrains shell commands.
type AgentBashPermissions struct {
	AllowPatterns []string             `yaml:"allow_patterns" json:"allow_patterns"`
	DenyPatterns  []string             `yaml:"deny_patterns" json:"deny_patterns"`
	Default       AgentPermissionLevel `yaml:"default" json:"default"`
}

// AgentFileMatrix scopes write/edit operations.
type AgentFileMatrix struct {
	Write AgentFilePermissionSet `yaml:"write" json:"write"`
	Edit  AgentFilePermissionSet `yaml:"edit" json:"edit"`
}

// AgentFilePermissionSet stores glob allow/deny rules.
type AgentFilePermissionSet struct {
	AllowPatterns     []string             `yaml:"allow_patterns" json:"allow_patterns"`
	DenyPatterns      []string             `yaml:"deny_patterns" json:"deny_patterns"`
	Default           AgentPermissionLevel `yaml:"default" json:"default"`
	RequireApproval   bool                 `yaml:"require_approval" json:"require_approval"`
	DocumentationOnly bool                 `yaml:"documentation_only" json:"documentation_only"`
}

// AgentInvocationSpec holds recursion data.
type AgentInvocationSpec struct {
	CanInvokeSubagents bool     `yaml:"can_invoke_subagents" json:"can_invoke_subagents"`
	AllowedSubagents   []string `yaml:"allowed_subagents" json:"allowed_subagents"`
	MaxDepth           int      `yaml:"max_depth" json:"max_depth"`
}

// AgentContextSpec limits context window.
type AgentContextSpec struct {
	MaxFiles            int    `yaml:"max_files" json:"max_files"`
	MaxTokens           int    `yaml:"max_tokens" json:"max_tokens"`
	IncludeGitHistory   bool   `yaml:"include_git_history" json:"include_git_history"`
	IncludeDependencies bool   `yaml:"include_dependencies" json:"include_dependencies"`
	CompressionStrategy string `yaml:"compression_strategy" json:"compression_strategy"` // "summary", "truncate", "hybrid"
	ProgressiveLoading  bool   `yaml:"progressive_loading" json:"progressive_loading"`
}

// AgentMetadata captures auxiliary metadata for display.
type AgentMetadata struct {
	Author   string   `yaml:"author" json:"author"`
	Tags     []string `yaml:"tags" json:"tags"`
	Priority int      `yaml:"priority" json:"priority"`
}

// AgentPermissionLevel enumerates allow/deny/ask.
type AgentPermissionLevel string

const (
	AgentPermissionAllow AgentPermissionLevel = "allow"
	AgentPermissionDeny  AgentPermissionLevel = "deny"
	AgentPermissionAsk   AgentPermissionLevel = "ask"
)

// Validate ensures the agent runtime section is well-formed.
func (a *AgentRuntimeSpec) Validate() error {
	if a == nil {
		return nil
	}
	if a.Mode == "" {
		return fmt.Errorf("agent mode required")
	}
	switch a.Mode {
	case AgentModePrimary, AgentModeSub, AgentModeSystem:
	default:
		return fmt.Errorf("invalid agent mode %s", a.Mode)
	}
	if err := a.Model.Validate(); err != nil {
		return fmt.Errorf("model invalid: %w", err)
	}
	if err := a.Tools.Validate(); err != nil {
		return err
	}
	if err := a.Files.Validate(); err != nil {
		return err
	}
	return nil
}

// Validate ensures model configuration is provided.
func (m AgentModelConfig) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("model name required")
	}
	if m.Provider == "" {
		return fmt.Errorf("model provider required")
	}
	return nil
}

// Validate ensures at least one capability is enabled.
func (t AgentToolMatrix) Validate() error {
	if !t.FileRead && !t.FileWrite && !t.SearchCodebase && !t.BashExecute && !t.LSPQuery {
		return fmt.Errorf("agent tools must enable at least one capability")
	}
	return nil
}

// Validate ensures file permission sets are consistent.
func (f AgentFileMatrix) Validate() error {
	if err := f.Write.validate("write"); err != nil {
		return err
	}
	if err := f.Edit.validate("edit"); err != nil {
		return err
	}
	return nil
}

func (set AgentFilePermissionSet) validate(label string) error {
	for _, pattern := range append([]string{}, append(set.AllowPatterns, set.DenyPatterns...)...) {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return fmt.Errorf("%s permission contains empty glob", label)
		}
		if strings.Contains(pattern, string(os.PathSeparator)+string(os.PathSeparator)) {
			return fmt.Errorf("%s permission glob %s invalid", label, pattern)
		}
	}
	switch set.Default {
	case AgentPermissionAllow, AgentPermissionAsk, AgentPermissionDeny, "":
	default:
		return fmt.Errorf("%s permission default %s invalid", label, set.Default)
	}
	return nil
}
