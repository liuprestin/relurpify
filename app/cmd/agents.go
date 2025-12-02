package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/lexcodex/relurpify/agents"
	"github.com/lexcodex/relurpify/framework"
)

// newAgentsCmd wires the `agents` command group.
func newAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage agent manifests",
	}
	cmd.AddCommand(newAgentsListCmd(), newAgentsCreateCmd(), newAgentsTestCmd())
	return cmd
}

// newAgentsListCmd lists manifests in the configured registry.
func newAgentsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List discovered agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := buildRegistry(ensureWorkspace())
			if err != nil {
				return err
			}
			summaries := reg.List()
			if len(summaries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No agents found.")
				return nil
			}
			for _, summary := range summaries {
				fmt.Fprintf(cmd.OutOrStdout(), "%s (%s) · model=%s · %s\n", summary.Name, summary.Mode, summary.Model, summary.Source)
				if summary.Description != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", summary.Description)
				}
			}
			return nil
		},
	}
}

// newAgentsCreateCmd scaffolds a manifest using the CLI flags.
func newAgentsCreateCmd() *cobra.Command {
	var name string
	var kind string
	var model string
	var description string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new agent manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws := ensureWorkspace()
			if name == "" {
				return fmt.Errorf("--name required")
			}
			if model == "" {
				model = defaultModelName()
			}
			path := filepath.Join(agents.ConfigDir(ws), "agents")
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			file := filepath.Join(path, fmt.Sprintf("%s.yaml", sanitizeName(name)))
			if _, err := os.Stat(file); err == nil {
				return fmt.Errorf("manifest %s already exists", file)
			}
			wsGlob := filepath.ToSlash(filepath.Join(ws, "**"))
			defaultToolCalling := true
			manifest := framework.AgentManifest{
				APIVersion: "relurpify/v1alpha1",
				Kind:       "AgentManifest",
				Metadata: framework.ManifestMetadata{
					Name:        name,
					Version:     "1.0.0",
					Description: description,
				},
				Spec: framework.ManifestSpec{
					Image:   "ghcr.io/relurpify/runtime:latest",
					Runtime: "gvisor",
					Permissions: framework.PermissionSet{
						FileSystem: []framework.FileSystemPermission{
							{Action: framework.FileSystemRead, Path: wsGlob, Justification: "Read workspace"},
							{Action: framework.FileSystemList, Path: wsGlob, Justification: "List workspace"},
							{Action: framework.FileSystemWrite, Path: wsGlob, Justification: "Modify workspace"},
						},
						Executables: []framework.ExecutablePermission{
							{Binary: "bash", Args: []string{"-c"}},
							{Binary: "go", Args: []string{"*"}},
						},
						Network: []framework.NetworkPermission{
							{Direction: "egress", Protocol: "tcp", Host: "localhost", Port: 11434, Description: "Ollama"},
						},
					},
					Resources: framework.ResourceSpec{
						Limits: framework.ResourceLimit{
							CPU:    "2",
							Memory: "4Gi",
							DiskIO: "500MBps",
						},
					},
					Security: framework.SecuritySpec{
						RunAsUser:       1000,
						ReadOnlyRoot:    false,
						NoNewPrivileges: true,
					},
					Audit: framework.AuditSpec{
						Level:         "verbose",
						RetentionDays: 7,
					},
					Agent: &framework.AgentRuntimeSpec{
						Mode:              framework.AgentMode(kind),
						Version:           "1.0.0",
						Prompt:            defaultManifestPrompt(name),
						OllamaToolCalling: &defaultToolCalling,
						Model: framework.AgentModelConfig{
							Provider:    "ollama",
							Name:        model,
							Temperature: 0.2,
							MaxTokens:   4096,
						},
						Tools: framework.AgentToolMatrix{
							FileRead:       true,
							FileWrite:      true,
							FileEdit:       true,
							BashExecute:    true,
							LSPQuery:       true,
							SearchCodebase: true,
						},
						Bash: framework.AgentBashPermissions{
							Default:       framework.AgentPermissionAsk,
							AllowPatterns: []string{"git diff*", "git status"},
							DenyPatterns:  []string{"rm -rf*", "sudo*"},
						},
						Files: framework.AgentFileMatrix{
							Write: framework.AgentFilePermissionSet{AllowPatterns: []string{"**/*.go", "docs/**/*.md"}, Default: framework.AgentPermissionAsk},
							Edit:  framework.AgentFilePermissionSet{Default: framework.AgentPermissionAsk, RequireApproval: true},
						},
						Invocation: framework.AgentInvocationSpec{
							CanInvokeSubagents: true,
							MaxDepth:           2,
						},
						Context: framework.AgentContextSpec{
							MaxFiles:            20,
							MaxTokens:           20000,
							IncludeDependencies: true,
						},
						Metadata: framework.AgentMetadata{
							Author:   os.Getenv("USER"),
							Tags:     []string{"generated"},
							Priority: 5,
						},
					},
				},
			}
			if err := manifest.Validate(); err != nil {
				return err
			}
			data, err := yaml.Marshal(manifest)
			if err != nil {
				return err
			}
			if err := os.WriteFile(file, data, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", file)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Agent name")
	cmd.Flags().StringVar(&kind, "kind", string(framework.AgentModePrimary), "Agent kind (primary|subagent|system)")
	cmd.Flags().StringVar(&model, "model", "", "Model name")
	cmd.Flags().StringVar(&description, "description", "Custom agent", "Description")
	return cmd
}

// newAgentsTestCmd validates a manifest by name and prints the result.
func newAgentsTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test [name]",
		Short: "Validate an agent manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws := ensureWorkspace()
			reg, err := buildRegistry(ws)
			if err != nil {
				return err
			}
			name := args[0]
			manifest, ok := reg.Get(name)
			if !ok {
				return fmt.Errorf("agent %s not found", name)
			}
			if err := manifest.Validate(); err != nil {
				return err
			}
			modelName := ""
			if manifest.Spec.Agent != nil {
				modelName = manifest.Spec.Agent.Model.Name
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Manifest %s valid (model=%s)\n", manifest.Metadata.Name, modelName)
			return nil
		},
	}
}

// defaultManifestPrompt returns a short instruction block for generated agents.
func defaultManifestPrompt(name string) string {
	return fmt.Sprintf(`You are %s. Follow project rules, ask before destructive actions, and summarize each change.`, strings.Title(name))
}
