package framework

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// AgentDefinition defines the configuration for a single agent.
type AgentDefinition struct {
	Name        string           `yaml:"name" json:"name"`
	Description string           `yaml:"description,omitempty" json:"description,omitempty"`
	Spec        AgentRuntimeSpec `yaml:"spec" json:"spec"`
}

// LoadAgentDefinition parses an agent definition file.
func LoadAgentDefinition(path string) (*AgentDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def AgentDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	if err := def.Spec.Validate(); err != nil {
		return nil, fmt.Errorf("agent spec invalid: %w", err)
	}
	return &def, nil
}
