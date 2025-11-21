package main

import (
	logpkg "log"
	"os"
	"strings"

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

	memory, err := framework.NewHybridMemory(".memory")
	if err != nil {
		logger.Fatalf("memory init failed: %v", err)
	}

	registry := framework.NewToolRegistry()
	for _, tool := range tools.FileOperations(".") {
		if err := registry.Register(tool); err != nil {
			logger.Fatalf("register tool %s: %v", tool.Name(), err)
		}
	}
	searchTools := []framework.Tool{
		&tools.GrepTool{BasePath: "."},
		&tools.SemanticSearchTool{BasePath: "."},
		&tools.SimilarityTool{BasePath: "."},
	}
	for _, tool := range searchTools {
		if err := registry.Register(tool); err != nil {
			logger.Fatalf("register tool %s: %v", tool.Name(), err)
		}
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
