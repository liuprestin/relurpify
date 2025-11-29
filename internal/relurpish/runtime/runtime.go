package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lexcodex/relurpify/agents"
	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/llm"
	"github.com/lexcodex/relurpify/server"
	"github.com/lexcodex/relurpify/tools"
)

// Runtime wires the relurpish CLI, Bubble Tea UI, and API server to the shared
// agent runtime. It centralizes tool registration, manifests, sandbox
// registration, and log management.
type Runtime struct {
	Config          Config
	Tools           *framework.ToolRegistry
	Memory          framework.MemoryStore
	Context         *framework.Context
	Agent           framework.Agent
	Model           framework.LanguageModel
	Registration    *framework.AgentRegistration
	RegistrationErr error
	Logger          *log.Logger
	Workspace       WorkspaceConfig

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
	logger := log.New(io.MultiWriter(os.Stdout, logFile), "relurpish ", log.LstdFlags|log.Lmicroseconds)

	registry, err := buildToolRegistry(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	memory, err := framework.NewHybridMemory(cfg.MemoryPath)
	if err != nil {
		return nil, fmt.Errorf("memory init: %w", err)
	}

	workspaceCfg, err := LoadWorkspaceConfig(cfg.ConfigPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Printf("workspace config load failed: %v", err)
		}
		workspaceCfg = WorkspaceConfig{}
	}
	if workspaceCfg.Model != "" {
		cfg.OllamaModel = workspaceCfg.Model
	}
	if len(workspaceCfg.Agents) > 0 {
		cfg.AgentName = workspaceCfg.Agents[0]
	}

	model := llm.NewClient(cfg.OllamaEndpoint, cfg.OllamaModel)
	agent := instantiateAgent(cfg, model, registry, memory)
	agentCfg := &framework.Config{
		Name:           cfg.AgentLabel(),
		Model:          cfg.OllamaModel,
		OllamaEndpoint: cfg.OllamaEndpoint,
		MaxIterations:  8,
	}
	if err := agent.Initialize(agentCfg); err != nil {
		return nil, fmt.Errorf("initialize agent: %w", err)
	}
	if reflection, ok := agent.(*agents.ReflectionAgent); ok {
		if reflection.Delegate != nil {
			_ = reflection.Delegate.Initialize(agentCfg)
		}
	}

	if len(workspaceCfg.AllowedTools) > 0 {
		registry.RestrictTo(workspaceCfg.AllowedTools)
	}

	rt := &Runtime{
		Config:    cfg,
		Tools:     registry,
		Memory:    memory,
		Context:   framework.NewContext(),
		Agent:     agent,
		Model:     model,
		Logger:    logger,
		logFile:   logFile,
		Workspace: workspaceCfg,
	}

	registration, regErr := framework.RegisterAgent(ctx, framework.RuntimeConfig{
		ManifestPath: cfg.ManifestPath,
		Sandbox:      cfg.Sandbox,
		AuditLimit:   cfg.AuditLimit,
		BaseFS:       cfg.Workspace,
		HITLTimeout:  cfg.HITLTimeout,
	})
	if regErr != nil {
		rt.RegistrationErr = regErr
		logger.Printf("permission manager unavailable: %v", regErr)
	} else {
		rt.Registration = registration
		if registration.Permissions != nil {
			registry.UsePermissionManager(registration.ID, registration.Permissions)
		}
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

// buildToolRegistry registers builtin tools scoped to the workspace.
func buildToolRegistry(workspace string) (*framework.ToolRegistry, error) {
	if workspace == "" {
		workspace = "."
	}
	registry := framework.NewToolRegistry()
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
		&tools.GitCommandTool{RepoPath: workspace, Command: "diff"},
		&tools.GitCommandTool{RepoPath: workspace, Command: "history"},
		&tools.GitCommandTool{RepoPath: workspace, Command: "branch"},
		&tools.GitCommandTool{RepoPath: workspace, Command: "commit"},
		&tools.GitCommandTool{RepoPath: workspace, Command: "blame"},
	} {
		if err := register(tool); err != nil {
			return nil, err
		}
	}
	for _, tool := range []framework.Tool{
		&tools.RunTestsTool{Command: []string{"go", "test", "./..."}, Workdir: workspace, Timeout: 10 * time.Minute},
		&tools.RunLinterTool{Command: []string{"golangci-lint", "run"}, Workdir: workspace, Timeout: 5 * time.Minute},
		&tools.RunBuildTool{Command: []string{"go", "build", "./..."}, Workdir: workspace, Timeout: 10 * time.Minute},
		&tools.ExecuteCodeTool{Command: []string{"bash", "-lc"}, Workdir: workspace, Timeout: 1 * time.Minute},
	} {
		if err := register(tool); err != nil {
			return nil, err
		}
	}
	for _, tool := range tools.CommandLineTools(workspace) {
		if err := register(tool); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func instantiateAgent(cfg Config, model framework.LanguageModel, registry *framework.ToolRegistry, memory framework.MemoryStore) framework.Agent {
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
	case "manual":
		return &agents.ManualCodingAgent{Model: model, Tools: registry}
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
	task := &framework.Task{
		ID:          fmt.Sprintf("chat-%d", time.Now().UnixNano()),
		Instruction: instruction,
		Type:        taskType,
		Context:     metadata,
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
