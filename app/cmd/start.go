package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/agents"
	"github.com/lexcodex/relurpify/app/relurpish/runtime"
	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/llm"
)

func newStartCmd() *cobra.Command {
	var mode string
	var agentName string
	var instruction string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a coding agent session",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws := ensureWorkspace()
			reg, err := buildRegistry(ws)
			if err != nil {
				return err
			}
			if agentName == "" {
				agentName = selectDefaultAgent(reg)
			}
			manifest, ok := reg.Get(agentName)
			if !ok {
				return fmt.Errorf("agent %s not found", agentName)
			}
			spec := manifest.Spec.Agent
			if spec == nil {
				return fmt.Errorf("agent %s missing spec.agent section", manifest.Metadata.Name)
			}
			if mode == "" {
				if spec.Mode != "" {
					mode = string(spec.Mode)
				} else {
					mode = string(agents.ModeCode)
				}
			}
			logLLM := false
			logAgent := false
			if globalCfg != nil {
				logLLM = globalCfg.Logging.LLM
				logAgent = globalCfg.Logging.Agent
			}
			if spec.Logging != nil {
				if spec.Logging.LLM != nil {
					logLLM = *spec.Logging.LLM
				}
				if spec.Logging.Agent != nil {
					logAgent = *spec.Logging.Agent
				}
			}
			if instruction == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Agent %s ready in %s mode. Provide --instruction to execute a task.\n", agentName, mode)
				return nil
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Dry run: %s in %s mode with instruction: %s\n", agentName, mode, instruction)
				return nil
			}
			modelName := spec.Model.Name
			if modelName == "" {
				modelName = defaultModelName()
			}
			model := llm.NewClient(defaultEndpoint(), modelName)
			model.SetDebugLogging(logLLM)
			tools, err := runtime.BuildToolRegistry(ws)
			if err != nil {
				return err
			}
			memoryPath := filepath.Join(ws, ".relurpish", "memory")
			memory, err := framework.NewHybridMemory(memoryPath)
			if err != nil {
				return err
			}
			agent := &agents.CodingAgent{
				Model:  model,
				Tools:  tools,
				Memory: memory,
			}
			cfg := &framework.Config{
				Name:              agentName,
				Model:             modelName,
				OllamaEndpoint:    defaultEndpoint(),
				MaxIterations:     8,
				OllamaToolCalling: spec.ToolCallingEnabled(),
				DebugLLM:          logLLM,
				DebugAgent:        logAgent,
			}
			if err := agent.Initialize(cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			task := &framework.Task{
				ID:          fmt.Sprintf("cli-%d", time.Now().UnixNano()),
				Instruction: instruction,
				Type:        framework.TaskTypeCodeGeneration,
				Context: map[string]any{
					"mode": mode,
				},
			}
			result, err := agent.Execute(ctx, task, framework.NewContext())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Agent complete (node=%s): %+v\n", result.NodeID, result.Data)
			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", string(agents.ModeCode), "Execution mode (code, architect, ask, debug, security, docs)")
	cmd.Flags().StringVar(&agentName, "agent", "", "Agent name from manifest registry")
	cmd.Flags().StringVar(&instruction, "instruction", "", "Instruction to execute")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate configuration without executing")
	return cmd
}

func selectDefaultAgent(reg *agents.Registry) string {
	list := reg.List()
	if len(list) == 0 {
		return "coding"
	}
	return list[0].Name
}

func defaultModelName() string {
	if globalCfg != nil && globalCfg.DefaultModel.Name != "" {
		return globalCfg.DefaultModel.Name
	}
	return "codellama:13b"
}

func defaultEndpoint() string {
	if val := os.Getenv("OLLAMA_HOST"); val != "" {
		return val
	}
	return "http://localhost:11434"
}
