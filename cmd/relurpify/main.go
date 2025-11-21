package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/cmd/internal/cliutils"
	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/llm"
	"github.com/lexcodex/relurpify/persistence"
	"github.com/lexcodex/relurpify/server"
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

	root.AddCommand(newServeCmd(), newTaskCmd(), newWorkflowCmd(), newMemoryCmd(), newDocsCmd(), newLSPCmd(), newInspectCmd())
	return root
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func newServeCmd() *cobra.Command {
	var addr string
	var memDir string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := &framework.Config{
				Model:              flagModel,
				OllamaEndpoint:     flagEndpoint,
				DefaultAgent:       "coding",
				MaxIterations:      8,
				DisableToolCalling: flagDisableTools,
			}
			memory, err := framework.NewHybridMemory(memDir)
			if err != nil {
				return err
			}
			registry := cliutils.BuildToolRegistry(flagWorkspace)
			modelClient := llm.NewClient(cfg.OllamaEndpoint, cfg.Model)
			agent := server.AgentFactory(modelClient, registry, memory, cfg)
			api := &server.APIServer{
				Agent:   agent,
				Context: framework.NewContext(),
				Logger:  log.New(os.Stdout, "api ", log.LstdFlags),
			}
			cmd.Printf("Starting API server on %s using model %s\n", addr, cfg.Model)
			return api.Serve(addr)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", envOrDefault("AGENT_SERVER_ADDR", ":8080"), "address for HTTP API server")
	cmd.Flags().StringVar(&memDir, "memory", filepath.Join(flagWorkspace, ".memory"), "Memory storage directory")
	return cmd
}

func newTaskCmd() *cobra.Command {
	var instruction string
	var taskType string
	var contextPath string
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Execute a single agent task and print the result",
		RunE: func(cmd *cobra.Command, args []string) error {
			if instruction == "" {
				return errors.New("instruction is required")
			}
			cfg := &framework.Config{
				Model:              flagModel,
				OllamaEndpoint:     flagEndpoint,
				DefaultAgent:       "coding",
				MaxIterations:      8,
				DisableToolCalling: flagDisableTools,
			}
			memory, err := framework.NewHybridMemory(filepath.Join(flagWorkspace, ".memory"))
			if err != nil {
				return err
			}
			registry := cliutils.BuildToolRegistry(flagWorkspace)
			modelClient := llm.NewClient(cfg.OllamaEndpoint, cfg.Model)
			agent := server.AgentFactory(modelClient, registry, memory, cfg)

			ctxData := map[string]any{}
			if contextPath != "" {
				data, err := os.ReadFile(contextPath)
				if err != nil {
					return err
				}
				if err := json.Unmarshal(data, &ctxData); err != nil {
					return err
				}
			}
			task := &framework.Task{
				ID:          fmt.Sprintf("task-%d", time.Now().UnixNano()),
				Type:        framework.TaskType(taskType),
				Instruction: instruction,
				Context:     ctxData,
			}
			state := framework.NewContext()
			state.Set("task.id", task.ID)

			result, err := agent.Execute(context.Background(), task, state)
			if err != nil {
				return err
			}
			output := map[string]any{
				"node":    result.NodeID,
				"success": result.Success,
				"data":    result.Data,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(output)
		},
	}
	cmd.Flags().StringVarP(&instruction, "instruction", "i", "", "Instruction for the agent (required)")
	cmd.Flags().StringVar(&taskType, "type", string(framework.TaskTypeCodeModification), "Task type")
	cmd.Flags().StringVar(&contextPath, "context", "", "Optional JSON file with additional context")
	return cmd
}

func newWorkflowCmd() *cobra.Command {
	var storePath string
	workflowCmd := &cobra.Command{Use: "workflow", Short: "Inspect workflow snapshots"}
	workflowCmd.PersistentFlags().StringVar(&storePath, "store", filepath.Join(flagWorkspace, ".control"), "Workflow store location")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List known workflow snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := persistence.NewFileWorkflowStore(storePath)
			if err != nil {
				return err
			}
			snapshots, err := store.List(cmd.Context())
			if err != nil {
				return err
			}
			for _, snap := range snapshots {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", snap.ID, snap.Status, snap.UpdatedAt.Format(time.RFC3339))
			}
			return nil
		},
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show a workflow snapshot by ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("workflow id required")
			}
			store, err := persistence.NewFileWorkflowStore(storePath)
			if err != nil {
				return err
			}
			snap, ok, err := store.Load(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("snapshot %s not found", args[0])
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(snap)
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a workflow snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("workflow id required")
			}
			store, err := persistence.NewFileWorkflowStore(storePath)
			if err != nil {
				return err
			}
			return store.Delete(cmd.Context(), args[0])
		},
	}

	workflowCmd.AddCommand(listCmd, showCmd, deleteCmd)
	return workflowCmd
}

func newMemoryCmd() *cobra.Command {
	var baseDir string
	memoryCmd := &cobra.Command{Use: "memory", Short: "Inspect hybrid memory"}
	memoryCmd.PersistentFlags().StringVar(&baseDir, "dir", filepath.Join(flagWorkspace, ".memory"), "Memory directory")

	recallCmd := &cobra.Command{
		Use:   "recall",
		Short: "Recall a memory by scope and key",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return errors.New("usage: memory recall <scope> <key>")
			}
			store, err := framework.NewHybridMemory(baseDir)
			if err != nil {
				return err
			}
			scope := framework.MemoryScope(strings.ToLower(args[0]))
			rec, ok, err := store.Recall(cmd.Context(), args[1], scope)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("memory %s not found", args[1])
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(rec)
		},
	}

	searchCmd := &cobra.Command{
		Use:   "search",
		Short: "Search memories by query",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return errors.New("usage: memory search <scope> <query>")
			}
			store, err := framework.NewHybridMemory(baseDir)
			if err != nil {
				return err
			}
			scope := framework.MemoryScope(strings.ToLower(args[0]))
			results, err := store.Search(cmd.Context(), args[1], scope)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(results)
		},
	}

	memoryCmd.AddCommand(recallCmd, searchCmd)
	return memoryCmd
}

func newDocsCmd() *cobra.Command {
	var docsDir string
	var goldsBin string
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate static documentation using Golds",
		RunE: func(cmd *cobra.Command, args []string) error {
			if goldsBin == "" {
				gopath := os.Getenv("GOPATH")
				if gopath == "" {
					var err error
					gopath, err = os.UserHomeDir()
					if err != nil {
						return err
					}
					gopath = filepath.Join(gopath, "go")
				}
				goldsBin = filepath.Join(gopath, "bin", "golds")
			}
			if _, err := os.Stat(goldsBin); err != nil {
				return fmt.Errorf("golds binary not found (%s)", goldsBin)
			}
			cacheDir := filepath.Join(flagWorkspace, ".gocache")
			modDir := filepath.Join(flagWorkspace, ".gomodcache")
			if err := os.MkdirAll(docsDir, 0o755); err != nil {
				return err
			}
			env := append(os.Environ(), "GOCACHE="+cacheDir, "GOMODCACHE="+modDir)
			command := exec.Command(goldsBin, "-gen", "-dir="+docsDir, "./...")
			command.Dir = flagWorkspace
			command.Env = env
			command.Stdout = cmd.OutOrStdout()
			command.Stderr = cmd.ErrOrStderr()
			return command.Run()
		},
	}
	cmd.Flags().StringVar(&docsDir, "out", filepath.Join(flagWorkspace, "docs"), "Output directory for docs")
	cmd.Flags().StringVar(&goldsBin, "golds", "", "Path to golds binary (default GOPATH/bin/golds)")
	return cmd
}

func newLSPCmd() *cobra.Command {
	var root string
	var language string
	var file string
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Probe an LSP server configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			desc, ok := cliutils.LookupLSPDescriptor(language)
			if !ok {
				return fmt.Errorf("unsupported language %s", language)
			}
			client, err := desc.Factory(root)
			if err != nil {
				return err
			}
			defer func() {
				if closer, ok := client.(interface{ Close() error }); ok {
					_ = closer.Close()
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if file != "" {
				diags, err := client.GetDiagnostics(ctx, file)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Diagnostics: %v\n", diags)
			}
			symbols, err := client.SearchSymbols(ctx, "")
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "workspace/symbol error: %v\n", err)
			} else if len(symbols) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Symbol sample: %v\n", symbols[:min(5, len(symbols))])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", flagWorkspace, "Workspace root for language server")
	cmd.Flags().StringVar(&language, "lang", "go", "Language (go,rust,clangd,haskell,ts,lua,python)")
	cmd.Flags().StringVar(&file, "file", "", "Optional file to request diagnostics for")
	return cmd
}

func newInspectCmd() *cobra.Command {
	var contextPath string
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect a stored context JSON file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if contextPath == "" {
				return errors.New("context file path required")
			}
			data, err := os.ReadFile(contextPath)
			if err != nil {
				return err
			}
			var ctx framework.Context
			if err := json.Unmarshal(data, &ctx); err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(ctx)
		},
	}
	cmd.Flags().StringVar(&contextPath, "file", "", "Path to a JSON context snapshot")
	return cmd
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
