package agents

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Rule describes a project-level constraint.
type Rule struct {
	Name        string `yaml:"name"`
	Scope       string `yaml:"scope"`
	Description string `yaml:"description"`
	Pattern     string `yaml:"pattern"`
	Enforcement string `yaml:"enforcement"`
}

// Ruleset groups project instructions.
type Ruleset struct {
	Rules          []Rule   `yaml:"rules"`
	CodingStandards []string `yaml:"coding_standards"`
	BestPractices  []string `yaml:"best_practices"`
}

// LoadRuleset reads relurpify_cfg/rules.yaml when present.
func LoadRuleset(path string) (*Ruleset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rules Ruleset
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return &rules, nil
}
