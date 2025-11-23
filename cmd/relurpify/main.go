package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/cmd/internal/cliutils"
	"github.com/lexcodex/relurpify/cmd/internal/setup"
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

	root.AddCommand(newServeCmd(), newTaskCmd(), newWorkflowCmd(), newMemoryCmd(), newDocsCmd(), newLSPCmd(), newInspectCmd(), newSetupCmd(), newShellCmd())
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

func newShellCmd() *cobra.Command {
	var configPath string
	var forceDetect bool
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Interactive agent shell with autodetected tooling",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := ensureShellConfig(configPath, forceDetect)
			if err != nil {
				return err
			}
			return runShell(cmd, configPath, cfg)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", setup.DefaultConfigPath(flagWorkspace), "Config file to load/save")
	cmd.Flags().BoolVar(&forceDetect, "detect", false, "Re-run detection before starting the shell")
	return cmd
}

func ensureShellConfig(path string, force bool) (*setup.Config, error) {
	prev, err := loadSetupConfig(path)
	if err != nil {
		return nil, err
	}
	if prev == nil || force {
		cfg, err := setup.Detect(flagWorkspace, flagEndpoint, flagModel, prev)
		if err != nil {
			return nil, err
		}
		if err := setup.SaveConfig(path, cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	return prev, nil
}

func runShell(cmd *cobra.Command, configPath string, cfg *setup.Config) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Type 'help' for a command list. Current environment:")
	describeConfig(cmd, cfg)
	scanner := bufio.NewScanner(cmd.InOrStdin())
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	for {
		fmt.Fprint(out, "relurpify> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		verb, rest := splitCommand(line)
		switch verb {
		case "help":
			printShellHelp(cmd)
		case "exit", "quit":
			return nil
		case "status":
			describeConfig(cmd, cfg)
		case "models":
			listModels(cmd, cfg)
		case "use":
			if rest == "" {
				cmd.Println("usage: use <model>")
				continue
			}
			if !cfg.SetSelectedModel(rest) {
				cmd.Printf("model %s not in detected list\n", rest)
				continue
			}
			cfg.LastUpdated = time.Now()
			if err := setup.SaveConfig(configPath, cfg); err != nil {
				return err
			}
			cmd.Printf("Default model switched to %s\n", rest)
		case "lsps":
			listLSPServers(cmd, cfg)
		case "detect":
			var err error
			cfg, err = refreshShellConfig(configPath, cfg)
			if err != nil {
				return err
			}
			describeConfig(cmd, cfg)
		case "task":
			if rest == "" {
				cmd.Println("usage: task <instruction>")
				continue
			}
			if err := shellRunTask(cmd, cfg, framework.TaskTypeCodeModification, rest); err != nil {
				cmd.Printf("task error: %v\n", err)
			}
		case "analyze":
			if rest == "" {
				cmd.Println("usage: analyze <instruction>")
				continue
			}
			if err := shellRunTask(cmd, cfg, framework.TaskTypeAnalysis, rest); err != nil {
				cmd.Printf("analyze error: %v\n", err)
			}
		case "apply":
			file, lang, instr, err := parseApplyArgs(rest)
			if err != nil {
				cmd.Println(err)
				continue
			}
			if err := shellRunApply(cmd, cfg, file, lang, instr); err != nil {
				cmd.Printf("apply error: %v\n", err)
			}
		default:
			cmd.Printf("unknown command: %s\n", verb)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func refreshShellConfig(path string, prev *setup.Config) (*setup.Config, error) {
	cfg, err := setup.Detect(flagWorkspace, flagEndpoint, flagModel, prev)
	if err != nil {
		return nil, err
	}
	if err := setup.SaveConfig(path, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func splitCommand(line string) (string, string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", ""
	}
	idx := strings.IndexAny(trimmed, " \t")
	if idx == -1 {
		return strings.ToLower(trimmed), ""
	}
	return strings.ToLower(trimmed[:idx]), strings.TrimSpace(trimmed[idx+1:])
}

func printShellHelp(cmd *cobra.Command) {
	cmd.Println("Commands:")
	cmd.Println("  help                 Show this message")
	cmd.Println("  status               Show the current autodetected environment")
	cmd.Println("  detect               Re-run environment detection")
	cmd.Println("  models               List Ollama models")
	cmd.Println("  use <model>          Switch the default model")
	cmd.Println("  lsps                 List LSP availability")
	cmd.Println("  task <instruction>   Run a coding task via the agent stack")
	cmd.Println("  analyze <instruction>Run an analysis-style task")
	cmd.Println("  apply [lang=<id>] <file> :: <instruction>  Apply changes to a file")
	cmd.Println("  exit|quit            Leave the shell")
}

func listModels(cmd *cobra.Command, cfg *setup.Config) {
	if len(cfg.Ollama.AvailableModels) == 0 {
		cmd.Println("No models detected.")
		if cfg.Ollama.LastError != "" {
			cmd.Printf("Last error: %s\n", cfg.Ollama.LastError)
		}
		if cfg.Ollama.SelectedModel != "" {
			cmd.Printf("Current selection: %s\n", cfg.Ollama.SelectedModel)
		}
		return
	}
	for _, model := range cfg.Ollama.AvailableModels {
		marker := " "
		if model == cfg.Ollama.SelectedModel {
			marker = "*"
		}
		cmd.Printf("%s %s\n", marker, model)
	}
}

func listLSPServers(cmd *cobra.Command, cfg *setup.Config) {
	for _, server := range cfg.LSPServers {
		status := "missing"
		if server.Available {
			status = "available"
		}
		cmd.Printf("%s - extensions: %s, status: %s", server.Language, strings.Join(server.Extensions, ","), status)
		if server.CommandPath != "" {
			cmd.Printf(" (%s)", server.CommandPath)
		}
		if server.WorkspaceMatches > 0 {
			cmd.Printf(" files=%d", server.WorkspaceMatches)
		}
		cmd.Println()
	}
}

func shellRunTask(cmd *cobra.Command, cfg *setup.Config, taskType framework.TaskType, instruction string) error {
	workspace := workspaceFromConfig(cfg)
	agentCfg := buildFrameworkConfig(cfg)
	memory, err := framework.NewHybridMemory(filepath.Join(workspace, ".memory"))
	if err != nil {
		return err
	}
	registry := cliutils.BuildToolRegistry(workspace)
	modelClient := llm.NewClient(agentCfg.OllamaEndpoint, agentCfg.Model)
	agent := server.AgentFactory(modelClient, registry, memory, agentCfg)
	task := &framework.Task{
		ID:          fmt.Sprintf("shell-%d", time.Now().UnixNano()),
		Type:        taskType,
		Instruction: instruction,
		Context:     map[string]any{},
	}
	state := framework.NewContext()
	state.Set("task.id", task.ID)
	result, err := agent.Execute(context.Background(), task, state)
	if err != nil {
		return err
	}
	return renderShellResult(cmd, state, result)
}

func shellRunApply(cmd *cobra.Command, cfg *setup.Config, filePath, language, instruction string) error {
	workspace := workspaceFromConfig(cfg)
	absFile := filePath
	if !filepath.IsAbs(absFile) {
		absFile = filepath.Join(workspace, filePath)
	}
	data, err := os.ReadFile(absFile)
	if err != nil {
		return err
	}
	agentCfg := buildFrameworkConfig(cfg)
	memory, err := framework.NewHybridMemory(filepath.Join(workspace, ".memory"))
	if err != nil {
		return err
	}
	registry := cliutils.BuildToolRegistry(workspace)
	langKey := language
	if langKey == "" {
		langKey = cliutils.InferLanguageByExtension(absFile)
	}
	langKey = canonicalLangKey(langKey)
	var cleanup func()
	if langKey != "" && shouldUseLSP(cfg, langKey) {
		proxy, closer, err := cliutils.NewProxyForLanguage(langKey, workspace)
		if err != nil {
			return err
		}
		if proxy != nil {
			cliutils.RegisterLSPTools(registry, proxy)
			cleanup = closer
		}
	}
	if cleanup != nil {
		defer cleanup()
	}
	modelClient := llm.NewClient(agentCfg.OllamaEndpoint, agentCfg.Model)
	agent := server.AgentFactory(modelClient, registry, memory, agentCfg)
	ctxMap := map[string]any{
		"file":  absFile,
		"files": []string{absFile},
		"code":  string(data),
	}
	if langKey != "" {
		ctxMap["language"] = langKey
	}
	task := &framework.Task{
		ID:          fmt.Sprintf("apply-%d", time.Now().UnixNano()),
		Type:        framework.TaskTypeCodeModification,
		Instruction: fmt.Sprintf("%s (target: %s)", instruction, absFile),
		Context:     ctxMap,
	}
	state := framework.NewContext()
	state.Set("task.id", task.ID)
	state.Set("active.file", absFile)
	state.Set("active.uri", absFile)
	result, err := agent.Execute(context.Background(), task, state)
	if err != nil {
		return err
	}
	return renderShellResult(cmd, state, result)
}

func shouldUseLSP(cfg *setup.Config, lang string) bool {
	if cfg == nil {
		return true
	}
	for _, server := range cfg.LSPServers {
		if server.Language == lang || server.ID == lang {
			return server.Available
		}
	}
	return true
}

func canonicalLangKey(lang string) string {
	if lang == "" {
		return ""
	}
	if desc, ok := cliutils.LookupLSPDescriptor(lang); ok && desc.ID != "" {
		return desc.ID
	}
	return strings.ToLower(lang)
}

func renderShellResult(cmd *cobra.Command, state *framework.Context, result *framework.Result) error {
	cmd.Printf("Agent node: %s success=%v\n", result.NodeID, result.Success)
	if final, ok := state.Get("react.final_output"); ok {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		cmd.Println("Final output:")
		_ = enc.Encode(final)
	}
	if tests, ok := state.Get("coder.tests"); ok {
		cmd.Printf("Test results: %v\n", tests)
	}
	if result.Data != nil {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		cmd.Println("Result data:")
		_ = enc.Encode(result.Data)
	}
	return nil
}

func workspaceFromConfig(cfg *setup.Config) string {
	if cfg != nil && cfg.Workspace != "" {
		return cfg.Workspace
	}
	return flagWorkspace
}

func buildFrameworkConfig(cfg *setup.Config) *framework.Config {
	model := flagModel
	if cfg != nil {
		if cfg.Ollama.SelectedModel != "" {
			model = cfg.Ollama.SelectedModel
		}
	}
	endpoint := flagEndpoint
	if cfg != nil && cfg.Ollama.Endpoint != "" {
		endpoint = cfg.Ollama.Endpoint
	}
	return &framework.Config{
		Model:              model,
		OllamaEndpoint:     endpoint,
		DefaultAgent:       "coding",
		MaxIterations:      8,
		DisableToolCalling: flagDisableTools,
	}
}

func parseApplyArgs(input string) (string, string, string, error) {
	rest := strings.TrimSpace(input)
	if rest == "" {
		return "", "", "", errors.New("usage: apply [lang=<id>] <file> :: <instruction>")
	}
	language := ""
	lower := strings.ToLower(rest)
	if strings.HasPrefix(lower, "lang=") {
		rest = rest[5:]
		idx := strings.IndexAny(rest, " \t")
		if idx == -1 {
			return "", "", "", errors.New("file path required after lang option")
		}
		language = strings.TrimSpace(rest[:idx])
		rest = strings.TrimSpace(rest[idx+1:])
	}
	parts := strings.SplitN(rest, "::", 2)
	if len(parts) != 2 {
		return "", "", "", errors.New("usage: apply [lang=<id>] <file> :: <instruction>")
	}
	file := strings.TrimSpace(parts[0])
	instruction := strings.TrimSpace(parts[1])
	if file == "" || instruction == "" {
		return "", "", "", errors.New("usage: apply [lang=<id>] <file> :: <instruction>")
	}
	return file, language, instruction, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
