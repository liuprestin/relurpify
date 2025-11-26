package main

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/cmd/internal/setup"
)

func newSetupCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Auto-detect Ollama/LSP tooling and persist a shared config",
		RunE: func(cmd *cobra.Command, args []string) error {
			prev, err := loadSetupConfig(configPath)
			if err != nil {
				return err
			}
			cfg, err := setup.Detect(flagWorkspace, flagEndpoint, flagModel, prev)
			if err != nil {
				return err
			}
			if err := setup.SaveConfig(configPath, cfg); err != nil {
				return err
			}
			printSetupSummary(cmd, configPath, cfg)
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", setup.DefaultConfigPath(flagWorkspace), "Path to write the shared config")
	return cmd
}

func loadSetupConfig(path string) (*setup.Config, error) {
	cfg, err := setup.LoadConfig(path)
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return nil, err
}

func printSetupSummary(cmd *cobra.Command, path string, cfg *setup.Config) {
	cmd.Printf("Config saved to %s\n", path)
	describeConfig(cmd, cfg)
}

func describeConfig(cmd *cobra.Command, cfg *setup.Config) {
	cmd.Printf("Workspace: %s\n", cfg.Workspace)
	if len(cfg.Languages) > 0 {
		cmd.Printf("Workspace languages: %s\n", strings.Join(cfg.Languages, ", "))
	} else {
		cmd.Println("Workspace languages: (not set)")
	}
	cmd.Printf("Ollama endpoint: %s (reachable=%v)\n", cfg.Ollama.Endpoint, cfg.Ollama.Reachable)
	if cfg.Ollama.CommandPath != "" {
		cmd.Printf("Ollama binary: %s\n", cfg.Ollama.CommandPath)
	}
	if len(cfg.Ollama.AvailableModels) > 0 {
		cmd.Printf("Models: %s (selected=%s)\n", strings.Join(cfg.Ollama.AvailableModels, ", "), cfg.Ollama.SelectedModel)
	} else {
		cmd.Printf("Models: unknown (selected=%s)\n", cfg.Ollama.SelectedModel)
	}
	if cfg.Ollama.LastError != "" {
		cmd.Printf("Ollama error: %s\n", cfg.Ollama.LastError)
	}
	cmd.Println("LSP servers:")
	for _, server := range cfg.LSPServers {
		status := "missing"
		if server.Available {
			status = "available"
		}
		cmd.Printf("  - %s (%s): %s", server.Language, strings.Join(server.Extensions, ","), status)
		if server.CommandPath != "" {
			cmd.Printf(" [%s]", server.CommandPath)
		}
		if server.WorkspaceMatches > 0 {
			cmd.Printf(" files=%d", server.WorkspaceMatches)
		}
		cmd.Println()
	}
}
