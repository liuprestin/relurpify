package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lexcodex/relurpify/agents"
)

// ensureWorkspace resolves the workspace CLI flag, defaulting to cwd.
func ensureWorkspace() string {
	if workspace == "" {
		wd, _ := os.Getwd()
		workspace = wd
	}
	return workspace
}

// buildRegistry loads manifests + rules scoped to the workspace.
func buildRegistry(workspace string) (*agents.Registry, error) {
	paths := []string{}
	if globalCfg != nil {
		paths = globalCfg.AgentSearchPaths(workspace)
	}
	rulesPath := filepath.Join(agents.ConfigDir(workspace), "rules.yaml")
	reg := agents.NewRegistry(agents.RegistryOptions{
		Workspace: workspace,
		Paths:     paths,
		RulesPath: rulesPath,
	})
	if err := reg.Load(); err != nil {
		return nil, err
	}
	return reg, nil
}

// readConfigMap deserializes config.yaml into a generic map for dotted lookups.
func readConfigMap(path string) (map[string]interface{}, error) {
	data := map[string]interface{}{}
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return data, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(bytes, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// writeConfigMap persists the config map back to YAML, creating directories.
func writeConfigMap(path string, data map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	bytes, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, bytes, 0o644)
}

// getConfigValue traverses a nested map using dotted notation.
func getConfigValue(data map[string]interface{}, key string) (interface{}, bool) {
	parts := strings.Split(key, ".")
	var current interface{} = data
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		value, ok := m[part]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

// setConfigValue mutates/creates nested keys referenced via dotted notation.
func setConfigValue(data map[string]interface{}, key string, value interface{}) error {
	parts := strings.Split(key, ".")
	current := data
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return nil
		}
		next, ok := current[part].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			current[part] = next
		}
		current = next
	}
	return nil
}

// parseValue attempts to coerce CLI input into bool/int/float before storing.
func parseValue(input string) interface{} {
	if b, err := strconv.ParseBool(input); err == nil {
		return b
	}
	if i, err := strconv.ParseInt(input, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(input, 64); err == nil {
		return f
	}
	return input
}

// prettyValue renders nested values in a human-readable one-line format.
func prettyValue(v interface{}) string {
	switch value := v.(type) {
	case []interface{}:
		var parts []string
		for _, item := range value {
			parts = append(parts, prettyValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]interface{}:
		b, _ := yaml.Marshal(value)
		return strings.TrimSpace(string(b))
	default:
		return fmt.Sprint(value)
	}
}

// sessionDir returns the path where session yaml files live.
func sessionDir() string {
	return filepath.Join(agents.ConfigDir(ensureWorkspace()), "sessions")
}

// sanitizeName normalizes user-provided identifiers for filenames.
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	return name
}
