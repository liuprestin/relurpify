package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	flagModel        string
	flagEndpoint     string
	flagWorkspace    string
	flagDisableTools bool
)

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "relurpify",
		Short: "CLI utilities for the Relurpify framework",
	}
	root.PersistentFlags().StringVar(&flagModel, "model", envOrDefault("OLLAMA_MODEL", "codellama"), "Default Ollama model")
	root.PersistentFlags().StringVar(&flagEndpoint, "ollama", envOrDefault("OLLAMA_ENDPOINT", "http://localhost:11434"), "Ollama endpoint")
	root.PersistentFlags().StringVar(&flagWorkspace, "workspace", ".", "Workspace root (used for tools and memories)")
	root.PersistentFlags().BoolVar(&flagDisableTools, "disable-tools", false, "Disable tool-calling prompts (use when model lacks function calling)")

	root.AddCommand(
		newServeCmd(),
		newTaskCmd(),
		newWorkflowCmd(),
		newMemoryCmd(),
		newDocsCmd(),
		newLSPCmd(),
		newInspectCmd(),
		newSetupCmd(),
		newShellCmd(),
	)
	return root
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
