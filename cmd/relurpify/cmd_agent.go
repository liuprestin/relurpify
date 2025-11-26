package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/cmd/internal/cliutils"
	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/llm"
	"github.com/lexcodex/relurpify/server"
)

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
