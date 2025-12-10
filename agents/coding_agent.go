package agents

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/lexcodex/relurpify/framework"
)

// CodingAgent orchestrates multiple specialized modes inspired by the
// requirements document. It wraps existing planning/react agents with tailored
// tool scopes and temperatures while keeping a consistent interface for the
// runtime.
type CodingAgent struct {
	Model        framework.LanguageModel
	Tools        *framework.ToolRegistry
	Memory       framework.MemoryStore
	Config       *framework.Config
	modeProfiles map[Mode]ModeProfile

	mu        sync.Mutex
	delegates map[Mode]framework.Agent
}

// Initialize wires configuration and default mode data.
func (a *CodingAgent) Initialize(cfg *framework.Config) error {
	a.Config = cfg
	if a.Tools == nil {
		a.Tools = framework.NewToolRegistry()
	}
	
	// If the config carries a manifest spec, apply its constraints
	if cfg != nil && cfg.AgentSpec != nil {
		// Apply tool matrix filtering (disable tools not in the matrix)
		// Note: PermissionManager handles security (file access), 
		// but this matrix handles logical capability (can I even see the tool?).
		applyToolMatrix(a.Tools, cfg.AgentSpec.Tools)
		
		// If mode profiles are not yet customized, we can inject the mode from spec
		// The AgentRuntimeSpec defines one primary mode, but CodingAgent supports many.
		// We can override the 'default' mode profile with spec data.
		if a.modeProfiles == nil {
			a.modeProfiles = defaultModeProfiles()
		}
		// Update default mode profile to match spec
		defProfile := a.modeProfiles[defaultMode]
		// Map spec.Files to capabilities/restrictions if needed
		// For now, we trust the PermissionManager for file path enforcement.
		a.modeProfiles[defaultMode] = defProfile
	}

	if a.modeProfiles == nil {
		a.modeProfiles = defaultModeProfiles()
	}
	if a.delegates == nil {
		a.delegates = make(map[Mode]framework.Agent)
	}
	return nil
}

// applyToolMatrix disables tools in the registry that are explicitly turned off by the manifest.
func applyToolMatrix(registry *framework.ToolRegistry, tools framework.AgentToolMatrix) {
	// This is a coarse filter. We iterate through known tool categories.
	// If a category is disabled, we remove those tools.
	// Since tool registry doesn't index by capability, we have to inspect tools.
	// For simplicity, we just pass the allowed list logic if we had one.
	// But here we might just want to remove specific ones.
	
	// Actually, easier to just rely on PermissionManager for strictness? 
	// No, the prompt should know what tools are available.
	
	// Map AgentToolMatrix to tool names/categories
	// This mapping would need to be robust. For now, a simple heuristic:
	if !tools.FileRead {
		// Remove file reading tools
		// registry.Remove("file_read") // ToolRegistry doesn't support Remove yet?
		// We can use RestrictTo if we build the allowed list.
	}
	// Better approach: Build an allow-list based on the matrix.
	var allowed []string
	all := registry.All()
	for _, t := range all {
		keep := false
		switch {
		case strings.HasPrefix(t.Name(), "file_read"): keep = tools.FileRead
		case strings.HasPrefix(t.Name(), "file_write"): keep = tools.FileWrite
		case strings.HasPrefix(t.Name(), "file_edit"): keep = tools.FileEdit
		case strings.HasPrefix(t.Name(), "file_"): keep = tools.FileRead // fallback for list/search
		case strings.HasPrefix(t.Name(), "exec_"): keep = tools.BashExecute
		case strings.HasPrefix(t.Name(), "git_"): keep = true // Git usually allowed if installed
		case strings.HasPrefix(t.Name(), "lsp_"): keep = tools.LSPQuery
		case strings.HasPrefix(t.Name(), "semantic_"): keep = tools.SearchCodebase
		case strings.HasPrefix(t.Name(), "grep"): keep = tools.SearchCodebase
		default: keep = true // Keep generic tools
		}
		if keep {
			allowed = append(allowed, t.Name())
		}
	}
	registry.RestrictTo(allowed)
}

// Capabilities aggregates capabilities from all modes.
func (a *CodingAgent) Capabilities() []framework.Capability {
	seen := map[framework.Capability]struct{}{}
	var caps []framework.Capability
	for _, profile := range a.modeProfiles {
		for _, cap := range profile.Capabilities {
			if _, ok := seen[cap]; ok {
				continue
			}
			seen[cap] = struct{}{}
			caps = append(caps, cap)
		}
	}
	return caps
}

// BuildGraph delegates to the default mode graph.
func (a *CodingAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	delegate, err := a.delegateForMode(defaultMode)
	if err != nil {
		return nil, err
	}
	return delegate.BuildGraph(task)
}

// Execute selects the correct mode and proxies execution to the underlying
// pattern agent. The context is augmented with the mode metadata so downstream
// tooling can render diagnostics.
func (a *CodingAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	mode := a.modeFromTask(task)
	profile, ok := a.modeProfiles[mode]
	if !ok {
		profile = a.modeProfiles[defaultMode]
	}
	delegate, err := a.delegateForMode(profile.Name)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task required")
	}
	enriched := *task
	enriched.Context = cloneContext(task.Context)
	enriched.Context["mode"] = string(profile.Name)
	enriched.Context["restrictions"] = profile.Restrictions
	enriched.Instruction = a.decorateInstruction(profile, task.Instruction)
	state.Set("coding_agent.mode", profile.Name)
	result, err := delegate.Execute(ctx, &enriched, state)
	if err != nil {
		return nil, err
	}
	if final, ok := state.Get("react.final_output"); ok {
		if result.Data == nil {
			result.Data = map[string]any{}
		}
		result.Data["final_output"] = final
	}
	return result, nil
}

// modeFromTask inspects task metadata/context to decide which mode should own
// execution. It defaults to the general coding mode when nothing is specified.
func (a *CodingAgent) modeFromTask(task *framework.Task) Mode {
	if task == nil {
		return defaultMode
	}
	if task.Metadata != nil {
		if mode, ok := task.Metadata["mode"]; ok {
			return Mode(strings.ToLower(mode))
		}
	}
	if task.Context != nil {
		if modeRaw, ok := task.Context["mode"]; ok {
			if mode, ok := modeRaw.(string); ok {
				return Mode(strings.ToLower(mode))
			}
		}
	}
	return defaultMode
}

// delegateForMode lazily instantiates the underlying agent for the requested
// mode and reuses it on subsequent calls.
func (a *CodingAgent) delegateForMode(mode Mode) (framework.Agent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if agent, ok := a.delegates[mode]; ok {
		return agent, nil
	}
	profile, ok := a.modeProfiles[mode]
	if !ok {
		return nil, fmt.Errorf("mode %s not configured", mode)
	}
	var agent framework.Agent
	switch mode {
	case ModeArchitect:
		agent = &PlannerAgent{Model: a.Model, Tools: a.scopedTools(profile.ToolScope), Memory: a.Memory}
	case ModeAsk:
		agent = &ReActAgent{
			Model:       a.Model,
			Tools:       a.scopedTools(profile.ToolScope),
			Memory:      a.Memory,
			Mode:        string(profile.Name),
			ModeProfile: convertModeRuntimeProfile(profile),
		}
	case ModeDocument:
		agent = &ReActAgent{
			Model:       a.Model,
			Tools:       a.scopedTools(profile.ToolScope),
			Memory:      a.Memory,
			Mode:        string(profile.Name),
			ModeProfile: convertModeRuntimeProfile(profile),
		}
	default:
		agent = &ReActAgent{
			Model:       a.Model,
			Tools:       a.scopedTools(profile.ToolScope),
			Memory:      a.Memory,
			Mode:        string(profile.Name),
			ModeProfile: convertModeRuntimeProfile(profile),
		}
	}
	if err := agent.Initialize(a.Config); err != nil {
		return nil, err
	}
	a.delegates[mode] = agent
	return agent, nil
}

// scopedTools clones the global registry but drops tools outside the mode's
// permission envelope.
func (a *CodingAgent) scopedTools(scope ToolScope) *framework.ToolRegistry {
	if a.Tools == nil {
		return framework.NewToolRegistry()
	}
	registry := framework.NewToolRegistry()
	for _, tool := range a.Tools.All() {
		if toolAllowed(tool, scope) {
			_ = registry.Register(tool)
		}
	}
	return registry
}

// toolAllowed checks whether the tool's declared permissions fit inside the
// mode's scope before the agent exposes it to the LLM.
func toolAllowed(tool framework.Tool, scope ToolScope) bool {
	perms := tool.Permissions()
	if perms.Permissions == nil {
		return true
	}
	for _, fs := range perms.Permissions.FileSystem {
		switch fs.Action {
		case framework.FileSystemWrite:
			if !scope.AllowWrite {
				return false
			}
		case framework.FileSystemExecute:
			if !scope.AllowExecute {
				return false
			}
		}
	}
	if len(perms.Permissions.Executables) > 0 && !scope.AllowExecute {
		return false
	}
	if len(perms.Permissions.Network) > 0 && !scope.AllowNetwork {
		return false
	}
	return true
}

// decorateInstruction wraps the user instruction with mode metadata so the LLM
// is primed with the current restrictions.
func (a *CodingAgent) decorateInstruction(profile ModeProfile, instruction string) string {
	if instruction == "" {
		return ""
	}
	builder := &strings.Builder{}
	fmt.Fprintf(builder, "[Mode: %s]\n", profile.Title)
	fmt.Fprintf(builder, "Description: %s\n", profile.Description)
	if len(profile.Restrictions) > 0 {
		fmt.Fprintf(builder, "Restrictions: %s\n", strings.Join(profile.Restrictions, "; "))
	}
	fmt.Fprintf(builder, "\n%s", instruction)
	return builder.String()
}

func convertModeRuntimeProfile(profile ModeProfile) ModeRuntimeProfile {
	contextPrefs := ContextPreferences{
		PreferredDetailLevel: profile.ContextProfile.PreferredDetailLevel,
		MinHistorySize:       profile.ContextProfile.MinHistorySize,
		CompressionThreshold: profile.ContextProfile.CompressionThreshold,
	}
	return ModeRuntimeProfile{
		Name:        string(profile.Name),
		Description: profile.Description,
		Temperature: profile.Temperature,
		Context:     contextPrefs,
	}
}

// cloneContext performs a shallow copy of the task context map to avoid
// mutating the caller's state.
func cloneContext(ctx map[string]any) map[string]any {
	if ctx == nil {
		return map[string]any{}
	}
	clone := make(map[string]any, len(ctx))
	for k, v := range ctx {
		clone[k] = v
	}
	return clone
}
