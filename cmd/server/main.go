package main

import (
	"context"
	logpkg "log"
	"os"
	"path/filepath"
	"strings"

	"github.com/lexcodex/relurpify/cmd/internal/cliutils"
	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/llm"
	"github.com/lexcodex/relurpify/server"
	"github.com/lexcodex/relurpify/tools"
)

func main() {
	logger := logpkg.Default()

	cfg := &framework.Config{
		Model:              envOrDefault("OLLAMA_MODEL", "codellama"),
		OllamaEndpoint:     envOrDefault("OLLAMA_ENDPOINT", "http://localhost:11434"),
		MaxIterations:      8,
		DefaultAgent:       "coding",
		DisableToolCalling: envBool("DISABLE_LLM_TOOLS"),
	}

	workspace := envOrDefault("RELURPIFY_WORKSPACE", ".")
	memory, err := framework.NewHybridMemory(filepath.Join(workspace, ".memory"))
	if err != nil {
		logger.Fatalf("memory init failed: %v", err)
	}

	registry := framework.NewToolRegistry()
	for _, tool := range tools.FileOperations(workspace) {
		if err := registry.Register(tool); err != nil {
			logger.Fatalf("register tool %s: %v", tool.Name(), err)
		}
	}
	searchTools := []framework.Tool{
		&tools.GrepTool{BasePath: workspace},
		&tools.SemanticSearchTool{BasePath: workspace},
		&tools.SimilarityTool{BasePath: workspace},
	}
	for _, tool := range searchTools {
		if err := registry.Register(tool); err != nil {
			logger.Fatalf("register tool %s: %v", tool.Name(), err)
		}
	}

	manifestPath := envOrDefault("RELURPIFY_MANIFEST", filepath.Join(workspace, "agent.manifest.yaml"))
	if _, err := cliutils.BootstrapRuntime(context.Background(), workspace, manifestPath, registry); err != nil {
		logger.Fatalf("security bootstrap failed: %v", err)
	}

	modelClient := llm.NewClient(cfg.OllamaEndpoint, cfg.Model)
	agent := server.AgentFactory(modelClient, registry, memory, cfg)

	api := &server.APIServer{
		Agent:   agent,
		Context: framework.NewContext(),
		Logger:  logger,
	}

	addr := envOrDefault("AGENT_SERVER_ADDR", ":8080")
	logger.Printf("Starting agentic API server on %s (model=%s)\n", addr, cfg.Model)
	logger.Fatal(api.Serve(addr))
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string) bool {
	val := os.Getenv(key)
	switch strings.ToLower(val) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
