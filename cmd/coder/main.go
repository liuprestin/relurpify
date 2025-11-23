package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/cmd/internal/cliutils"
	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/llm"
	"github.com/lexcodex/relurpify/server"
)

var (
	coderModel        string
	coderEndpoint     string
	coderWorkspace    string
	coderDisableTools bool
)

func main() {
	root := newCoderRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newCoderRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "relurpify-coder",
		Short: "Cursor-like CLI for running coding agents",
	}
	root.PersistentFlags().StringVar(&coderModel, "model", envOrDefault("OLLAMA_MODEL", "codellama"), "Default Ollama model")
	root.PersistentFlags().StringVar(&coderEndpoint, "ollama", envOrDefault("OLLAMA_ENDPOINT", "http://localhost:11434"), "Ollama endpoint")
	root.PersistentFlags().StringVar(&coderWorkspace, "workspace", ".", "Workspace root")
	root.PersistentFlags().BoolVar(&coderDisableTools, "disable-tools", false, "Disable tool-calling prompts (use when model lacks function calling)")

	root.AddCommand(newApplyCmd())
	return root
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func newApplyCmd() *cobra.Command {
	var instruction string
	var filePath string
	var language string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply an instruction to a file via the coding agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if instruction == "" {
				return errors.New("instruction is required")
			}
			if filePath == "" {
				return errors.New("file is required")
			}
			absFile := filePath
			if !filepath.IsAbs(absFile) {
				absFile = filepath.Join(coderWorkspace, filePath)
			}
			data, err := os.ReadFile(absFile)
			if err != nil {
				return err
			}

			cfg := &framework.Config{
				Model:              coderModel,
				OllamaEndpoint:     coderEndpoint,
				DefaultAgent:       "coding",
				MaxIterations:      8,
				DisableToolCalling: coderDisableTools,
			}
			memory, err := framework.NewHybridMemory(filepath.Join(coderWorkspace, ".memory"))
			if err != nil {
				return err
			}
			registry := cliutils.BuildToolRegistry(coderWorkspace)

			langKey := language
			if langKey == "" {
				langKey = cliutils.InferLanguageByExtension(absFile)
			}
			var cleanup func()
			if langKey != "" {
				proxy, closer, err := cliutils.NewProxyForLanguage(langKey, coderWorkspace)
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

			modelClient := llm.NewClient(coderEndpoint, coderModel)
			agent := server.AgentFactory(modelClient, registry, memory, cfg)

			ctxMap := map[string]any{
				"file":  absFile,
				"files": []string{absFile},
				"code":  string(data),
			}
			if langKey != "" {
				ctxMap["language"] = langKey
			}
			task := &framework.Task{
				ID:          fmt.Sprintf("code-%d", time.Now().UnixNano()),
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
			return renderResult(cmd, state, result)
		},
	}
	cmd.Flags().StringVarP(&instruction, "instruction", "i", "", "Instruction to apply (required)")
	cmd.Flags().StringVarP(&filePath, "file", "f", "", "File to operate on (required)")
	cmd.Flags().StringVar(&language, "lang", "", "Language override (auto-detected from file extension)")
	return cmd
}

func renderResult(cmd *cobra.Command, state *framework.Context, result *framework.Result) error {
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
