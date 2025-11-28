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
}

// ManifestMetadata describes identity fields.
type ManifestMetadata struct {
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// ManifestSpec encodes runtime, permission, resource, and security sections.
type ManifestSpec struct {
	Image       string        `yaml:"image" json:"image"`
	Runtime     string        `yaml:"runtime" json:"runtime"`
	Permissions PermissionSet `yaml:"permissions" json:"permissions"`
	Resources   ResourceSpec  `yaml:"resources" json:"resources"`
	Security    SecuritySpec  `yaml:"security" json:"security"`
	Audit       AuditSpec     `yaml:"audit" json:"audit"`
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
	return nil
}
