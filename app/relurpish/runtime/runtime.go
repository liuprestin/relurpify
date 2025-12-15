package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lexcodex/relurpify/agents"
	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/framework/ast"
	"github.com/lexcodex/relurpify/llm"
	"github.com/lexcodex/relurpify/server"
	"github.com/lexcodex/relurpify/tools"
)

// Runtime wires the relurpish CLI, Bubble Tea UI, and API server to the shared
// agent runtime. It centralizes tool registration, manifests, sandbox
// registration, and log management.
type Runtime struct {
	Config       Config
	Tools        *framework.ToolRegistry
	Memory       framework.MemoryStore
	Context      *framework.Context
	Agent        framework.Agent
	Model        framework.LanguageModel
	Registration *framework.AgentRegistration
	Logger       *log.Logger
	Workspace    WorkspaceConfig

	logFile io.Closer

	serverMu     sync.Mutex
	serverCancel context.CancelFunc
}

// New builds a runtime. It always returns a usable Runtime instance even when
// sandbox or manifest verification fails so that the wizard/status views can
// surface actionable diagnostics.
func New(ctx context.Context, cfg Config) (*Runtime, error) {
	if err := cfg.Normalize(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	logFile, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	logger := log.New(logFile, "relurpish ", log.LstdFlags|log.Lmicroseconds)

	memory, err := framework.NewHybridMemory(cfg.MemoryPath)
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("memory init: %w", err)
	}

	var workspaceCfg WorkspaceConfig
	var allowedTools []string
	if cfg.ConfigPath != "" {
		if loaded, err := LoadWorkspaceConfig(cfg.ConfigPath); err == nil {
			workspaceCfg = loaded
			if workspaceCfg.Model != "" {
				cfg.OllamaModel = workspaceCfg.Model
			}
			if len(workspaceCfg.Agents) > 0 {
				cfg.AgentName = workspaceCfg.Agents[0]
			}
			allowedTools = append(allowedTools, workspaceCfg.AllowedTools...)
		} else if !errors.Is(err, os.ErrNotExist) {
			logger.Printf("workspace config load failed: %v", err)
		}
	}

	registration, err := framework.RegisterAgent(ctx, framework.RuntimeConfig{
		ManifestPath: cfg.ManifestPath,
		Sandbox:      cfg.Sandbox,
		AuditLimit:   cfg.AuditLimit,
		BaseFS:       cfg.Workspace,
		HITLTimeout:  cfg.HITLTimeout,
	})
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("sandbox registration failed: %w", err)
	}
	if registration.Manifest == nil || registration.Manifest.Spec.Agent == nil {
		logFile.Close()
		return nil, fmt.Errorf("agent manifest missing spec.agent configuration")
	}
	agentSpec := registration.Manifest.Spec.Agent
	if agentSpec.Model.Name == "" {
		logFile.Close()
		return nil, fmt.Errorf("agent manifest missing spec.agent.model.name")
	}
	runner, err := framework.NewSandboxCommandRunner(registration.Manifest, registration.Runtime, cfg.Workspace)
	if err != nil {
		logFile.Close()
		return nil, err
	}
	registry, err := BuildToolRegistry(cfg.Workspace, runner, ToolRegistryOptions{
		AgentID:            registration.ID,
		PermissionManager:  registration.Permissions,
		AgentSpec:          nil,
	})
	if err != nil {
		logFile.Close()
		return nil, err
	}
	if cfg.AgentName == "" {
		cfg.AgentName = registration.Manifest.Metadata.Name
	}

	if cfg.OllamaModel == "" {
		cfg.OllamaModel = agentSpec.Model.Name
	}
	if cfg.OllamaModel == "" {
		logFile.Close()
		return nil, fmt.Errorf("ollama model not configured; update %s", cfg.ManifestPath)
	}

	// Load all agent definitions from the agents directory
	agentDefs, err := LoadAgentDefinitions(cfg.AgentsDir)
	if err != nil && !os.IsNotExist(err) {
		// Log warning but proceed with builtin agents
		logger.Printf("warning: failed to load agent definitions: %v", err)
	}

	// Setup Telemetry
	var sinks []framework.Telemetry
	sinks = append(sinks, framework.LoggerTelemetry{Logger: logger})

	if cfg.TelemetryPath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.TelemetryPath), 0o755); err == nil {
			if fileSink, err := framework.NewJSONFileTelemetry(cfg.TelemetryPath); err == nil {
				sinks = append(sinks, fileSink)
			} else {
				logger.Printf("warning: failed to init json telemetry: %v", err)
			}
		}
	}
	telemetry := framework.MultiplexTelemetry{Sinks: sinks}
	registry.UseTelemetry(telemetry)

	logLLM := false
	if agentSpec.Logging != nil && agentSpec.Logging.LLM != nil {
		logLLM = *agentSpec.Logging.LLM
	}
	modelClient := llm.NewClient(cfg.OllamaEndpoint, cfg.OllamaModel)
	modelClient.SetDebugLogging(logLLM)
	model := llm.NewInstrumentedModel(modelClient, telemetry, logLLM)

	// Create base config derived from manifest + CLI args
	agentCfg := &framework.Config{
		Name:              cfg.AgentLabel(),
		Model:             cfg.OllamaModel,
		OllamaEndpoint:    cfg.OllamaEndpoint,
		MaxIterations:     8,
		OllamaToolCalling: agentSpec.ToolCallingEnabled(),
		AgentSpec:         agentSpec, // Default to manifest spec
		Telemetry:         telemetry,
	}

	agent := instantiateAgent(cfg, model, registry, memory, agentDefs, agentCfg)

	// Enforce the effective (post-definition) tool policies before initializing.
	if agentCfg.AgentSpec != nil {
		framework.RestrictToolRegistryByMatrix(registry, agentCfg.AgentSpec.Tools)
		registry.UseAgentSpec(registration.ID, agentCfg.AgentSpec)
	}

	if err := agent.Initialize(agentCfg); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("initialize agent: %w", err)
	}
	if reflection, ok := agent.(*agents.ReflectionAgent); ok {
		if reflection.Delegate != nil {
			_ = reflection.Delegate.Initialize(agentCfg)
		}
	}
	if len(allowedTools) > 0 {
		registry.RestrictTo(allowedTools)
	}
	rt := &Runtime{
		Config:       cfg,
		Tools:        registry,
		Memory:       memory,
		Context:      framework.NewContext(),
		Agent:        agent,
		Model:        model,
		Logger:       logger,
		logFile:      logFile,
		Workspace:    workspaceCfg,
		Registration: registration,
	}
	return rt, nil
}

// Close releases resources managed by runtime.
func (r *Runtime) Close() error {
	if r.logFile != nil {
		return r.logFile.Close()
	}
	return nil
}

// ToolRegistryOptions carries optional manifest/runtime policies into tool construction.
type ToolRegistryOptions struct {
	AgentID           string
	PermissionManager *framework.PermissionManager
	AgentSpec         *framework.AgentRuntimeSpec
}

// BuildToolRegistry registers builtin tools scoped to the workspace.
func BuildToolRegistry(workspace string, runner framework.CommandRunner, opts ...ToolRegistryOptions) (*framework.ToolRegistry, error) {
	if workspace == "" {
		workspace = "."
	}
	if runner == nil {
		return nil, fmt.Errorf("command runner required")
	}
	var cfg ToolRegistryOptions
	if len(opts) > 0 {
		cfg = opts[0]
	}
	registry := framework.NewToolRegistry()
	if cfg.PermissionManager != nil {
		registry.UsePermissionManager(cfg.AgentID, cfg.PermissionManager)
	}
	if cfg.AgentSpec != nil {
		registry.UseAgentSpec(cfg.AgentID, cfg.AgentSpec)
	}
	register := func(tool framework.Tool) error {
		if err := registry.Register(tool); err != nil {
			return err
		}
		return nil
	}
	for _, tool := range tools.FileOperations(workspace) {
		if err := register(tool); err != nil {
			return nil, err
		}
	}
	for _, tool := range []framework.Tool{
		&tools.GrepTool{BasePath: workspace},
		&tools.SimilarityTool{BasePath: workspace},
		&tools.SemanticSearchTool{BasePath: workspace},
	} {
		if err := register(tool); err != nil {
			return nil, err
		}
	}
	for _, tool := range []framework.Tool{
		&tools.GitCommandTool{RepoPath: workspace, Command: "diff", Runner: runner},
		&tools.GitCommandTool{RepoPath: workspace, Command: "history", Runner: runner},
		&tools.GitCommandTool{RepoPath: workspace, Command: "branch", Runner: runner},
		&tools.GitCommandTool{RepoPath: workspace, Command: "commit", Runner: runner},
		&tools.GitCommandTool{RepoPath: workspace, Command: "blame", Runner: runner},
	} {
		if err := register(tool); err != nil {
			return nil, err
		}
	}
	for _, tool := range []framework.Tool{
		&tools.RunTestsTool{Command: []string{"go", "test", "./..."}, Workdir: workspace, Timeout: 10 * time.Minute, Runner: runner},
		&tools.RunLinterTool{Command: []string{"golangci-lint", "run"}, Workdir: workspace, Timeout: 5 * time.Minute, Runner: runner},
		&tools.RunBuildTool{Command: []string{"go", "build", "./..."}, Workdir: workspace, Timeout: 10 * time.Minute, Runner: runner},
		&tools.ExecuteCodeTool{Command: []string{"bash", "-c"}, Workdir: workspace, Timeout: 1 * time.Minute, Runner: runner},
	} {
		if err := register(tool); err != nil {
			return nil, err
		}
	}
	for _, tool := range tools.CommandLineTools(workspace, runner) {
		if err := register(tool); err != nil {
			return nil, err
		}
	}
	indexDir := filepath.Join(workspace, "relurpify_cfg", "memory", "ast_index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return nil, err
	}
	store, err := ast.NewSQLiteStore(filepath.Join(indexDir, "index.db"))
	if err != nil {
		return nil, err
	}
	manager := ast.NewIndexManager(store, ast.IndexConfig{
		WorkspacePath:   workspace,
		ParallelWorkers: 4,
	})
	if cfg.PermissionManager != nil {
		manager.SetPathFilter(func(path string, isDir bool) bool {
			action := framework.FileSystemRead
			if isDir {
				action = framework.FileSystemList
			}
			return cfg.PermissionManager.CheckFileAccess(context.Background(), cfg.AgentID, action, path) == nil
		})
	}
	tools.AttachASTSymbolProvider(manager, registry)
	if err := register(tools.NewASTTool(manager)); err != nil {
		return nil, err
	}
	go manager.IndexWorkspace()
	return registry, nil
}

// LoadAgentDefinitions scans the directory for YAML files and parses them.
func LoadAgentDefinitions(dir string) (map[string]*framework.AgentDefinition, error) {
	defs := make(map[string]*framework.AgentDefinition)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml")) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		def, err := framework.LoadAgentDefinition(path)
		if err != nil {
			if errors.Is(err, framework.ErrNotAgentDefinition) {
				continue
			}
			return nil, fmt.Errorf("load %s: %w", entry.Name(), err)
		}
		if def.Name == "" {
			def.Name = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}
		defs[def.Name] = def
	}
	return defs, nil
}

// instantiateAgent picks the concrete agent implementation for the CLI preset.
func instantiateAgent(cfg Config, model framework.LanguageModel, registry *framework.ToolRegistry, memory framework.MemoryStore, defs map[string]*framework.AgentDefinition, agentCfg *framework.Config) framework.Agent {
	// Check file-based definitions first
	if def, ok := defs[cfg.AgentName]; ok {
		// Update config with the definition's spec
		agentCfg.AgentSpec = &def.Spec
		agentCfg.OllamaToolCalling = def.Spec.ToolCallingEnabled()
		if def.Spec.Model.Name != "" {
			agentCfg.Model = def.Spec.Model.Name
		}

		// Use the Implementation field to pick struct
		switch def.Spec.Implementation {
		case "planner":
			return &agents.PlannerAgent{Model: model, Tools: registry, Memory: memory}
		case "react":
			return &agents.ReActAgent{Model: model, Tools: registry, Memory: memory}
		case "eternal":
			return &agents.EternalAgent{Model: model}
		// TODO: Add support for creating agents directly from 'def' struct fields (system prompt, etc)
		// For now we map them to existing Go structs.
		default:
			// Fallback to ReAct if unspecified but defined
			return &agents.ReActAgent{Model: model, Tools: registry, Memory: memory, Mode: string(def.Spec.Mode)}
		}
	}

	switch cfg.AgentLabel() {
	case "planner":
		return &agents.PlannerAgent{Model: model, Tools: registry, Memory: memory}
	case "react":
		return &agents.ReActAgent{Model: model, Tools: registry, Memory: memory}
	case "reflection":
		return &agents.ReflectionAgent{
			Reviewer: model,
			Delegate: &agents.CodingAgent{Model: model, Tools: registry, Memory: memory},
		}
	case "expert":
		return &agents.ExpertCoderAgent{Model: model, Tools: registry, Memory: memory}
	default:
		return &agents.CodingAgent{Model: model, Tools: registry, Memory: memory}
	}
}

// RunTask executes a task against the configured agent while preserving shared
// context state for future status screens.
func (r *Runtime) RunTask(ctx context.Context, task *framework.Task) (*framework.Result, error) {
	if task == nil {
		return nil, errors.New("task required")
	}
	state := r.Context.Clone()
	state.Set("task.id", task.ID)
	state.Set("task.type", string(task.Type))
	state.Set("task.instruction", task.Instruction)
	if task.Context != nil {
		if source, ok := task.Context["source"]; ok {
			state.Set("task.source", fmt.Sprint(source))
		}
	}
	res, err := r.Agent.Execute(ctx, task, state)
	if err == nil {
		r.Context.Merge(state)
	}
	return res, err
}

// ExecuteInstruction convenience helper.
func (r *Runtime) ExecuteInstruction(ctx context.Context, instruction string, taskType framework.TaskType, metadata map[string]any) (*framework.Result, error) {
	if taskType == "" {
		taskType = framework.TaskTypeCodeModification
	}

	metaStrings := make(map[string]string)
	if metadata != nil {
		for k, v := range metadata {
			if s, ok := v.(string); ok {
				metaStrings[k] = s
			}
		}
	}

	task := &framework.Task{
		ID:          fmt.Sprintf("chat-%d", time.Now().UnixNano()),
		Instruction: instruction,
		Type:        taskType,
		Context:     metadata,
		Metadata:    metaStrings,
	}
	return r.RunTask(ctx, task)
}

// StartServer launches the HTTP API server. The returned stop function shuts
// the server down using the provided context.
func (r *Runtime) StartServer(ctx context.Context, addr string) (func(context.Context) error, error) {
	r.serverMu.Lock()
	defer r.serverMu.Unlock()
	if r.serverCancel != nil {
		return nil, errors.New("server already running")
	}
	if addr == "" {
		addr = r.Config.ServerAddr
	}
	api := &server.APIServer{Agent: r.Agent, Context: r.Context, Logger: r.Logger}
	serverCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- api.ServeContext(serverCtx, addr)
	}()
	r.serverCancel = cancel
	stopFn := func(shutdownCtx context.Context) error {
		r.serverMu.Lock()
		if r.serverCancel == nil {
			r.serverMu.Unlock()
			return nil
		}
		r.serverCancel()
		r.serverCancel = nil
		r.serverMu.Unlock()
		select {
		case err := <-errCh:
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		case <-shutdownCtx.Done():
			return shutdownCtx.Err()
		}
	}
	return stopFn, nil
}

// ServerRunning reports whether the HTTP server is active.
func (r *Runtime) ServerRunning() bool {
	r.serverMu.Lock()
	defer r.serverMu.Unlock()
	return r.serverCancel != nil
}

// PendingHITL exposes outstanding permission requests.
func (r *Runtime) PendingHITL() []*framework.PermissionRequest {
	if r.Registration == nil || r.Registration.HITL == nil {
		return nil
	}
	return r.Registration.HITL.PendingRequests()
}

// SubscribeHITL streams HITL lifecycle events (requested/resolved/expired).
// The returned cancel function can be called to unsubscribe.
func (r *Runtime) SubscribeHITL() (<-chan framework.HITLEvent, func()) {
	if r == nil || r.Registration == nil || r.Registration.HITL == nil {
		ch := make(chan framework.HITLEvent)
		close(ch)
		return ch, func() {}
	}
	return r.Registration.HITL.Subscribe(32)
}

// ApproveHITL approves a pending request with the supplied scope.
func (r *Runtime) ApproveHITL(requestID, approver string, scope framework.GrantScope, duration time.Duration) error {
	if r.Registration == nil || r.Registration.HITL == nil {
		return errors.New("hitl broker unavailable")
	}
	if scope == "" {
		scope = framework.GrantScopeOneTime
	}
	decision := framework.PermissionDecision{
		RequestID:  requestID,
		Approved:   true,
		ApprovedBy: approver,
		Scope:      scope,
		ExpiresAt:  time.Now().Add(duration),
	}
	return r.Registration.HITL.Approve(decision)
}

// DenyHITL rejects a pending request.
func (r *Runtime) DenyHITL(requestID, reason string) error {
	if r.Registration == nil || r.Registration.HITL == nil {
		return errors.New("hitl broker unavailable")
	}
	return r.Registration.HITL.Deny(requestID, reason)
}
