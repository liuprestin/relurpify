package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/lexcodex/relurpify/agents"
)

type sessionSnapshot struct {
	Name      string    `yaml:"name"`
	Workspace string    `yaml:"workspace"`
	Agent     string    `yaml:"agent"`
	Mode      string    `yaml:"mode"`
	SavedAt   time.Time `yaml:"saved_at"`
}

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage saved sessions",
	}
	cmd.AddCommand(newSessionSaveCmd(), newSessionLoadCmd(), newSessionListCmd())
	return cmd
}

func newSessionSaveCmd() *cobra.Command {
	var name string
	var agent string
	var mode string

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Persist a lightweight session snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name required")
			}
			if agent == "" {
				agent = "coding"
			}
			if mode == "" {
				mode = string(agents.ModeCode)
			}
			snap := sessionSnapshot{
				Name:      name,
				Workspace: ensureWorkspace(),
				Agent:     agent,
				Mode:      mode,
				SavedAt:   time.Now(),
			}
			dir := sessionDir()
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			data, err := yaml.Marshal(snap)
			if err != nil {
				return err
			}
			file := filepath.Join(dir, sanitizeName(name)+".yaml")
			if err := os.WriteFile(file, data, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Session saved to %s\n", file)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Session name")
	cmd.Flags().StringVar(&agent, "agent", "", "Agent identifier")
	cmd.Flags().StringVar(&mode, "mode", "", "Mode to store")
	return cmd
}

func newSessionLoadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "load [name]",
		Short: "Inspect a saved session snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := filepath.Join(sessionDir(), sanitizeName(args[0])+".yaml")
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			var snap sessionSnapshot
			if err := yaml.Unmarshal(data, &snap); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Session %s · agent=%s · mode=%s · workspace=%s · saved_at=%s\n",
				snap.Name, snap.Agent, snap.Mode, snap.Workspace, snap.SavedAt.Format(time.RFC3339))
			return nil
		},
	}
}

func newSessionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := sessionDir()
			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "No saved sessions.")
					return nil
				}
				return err
			}
			var snaps []sessionSnapshot
			for _, entry := range entries {
				if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
					continue
				}
				data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
				if err != nil {
					continue
				}
				var snap sessionSnapshot
				if err := yaml.Unmarshal(data, &snap); err != nil {
					continue
				}
				snaps = append(snaps, snap)
			}
			if len(snaps) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No saved sessions.")
				return nil
			}
			sort.Slice(snaps, func(i, j int) bool {
				return snaps[i].SavedAt.After(snaps[j].SavedAt)
			})
			for _, snap := range snaps {
				fmt.Fprintf(cmd.OutOrStdout(), "%s · agent=%s · mode=%s · saved_at=%s\n", snap.Name, snap.Agent, snap.Mode, snap.SavedAt.Format(time.RFC822))
			}
			return nil
		},
	}
}
