package pattern

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	agentctx "github.com/lexcodex/relurpify/agents/contextual"
	"github.com/lexcodex/relurpify/framework"
)

// ReActAgent implements the Reason+Act pattern.
// ModeRuntimeProfile conveys high-level runtime settings to the agent.
type ModeRuntimeProfile struct {
	Name        string
	Description string
	Temperature float64
	Context     ContextPreferences
}

// ContextPreferences tune context management for a mode.
type ContextPreferences struct {
	PreferredDetailLevel agentctx.DetailLevel
	MinHistorySize       int
	CompressionThreshold float64
}

// ReActAgent implements the Reason+Act pattern.
type ReActAgent struct {
	Model               framework.LanguageModel
	Tools               *framework.ToolRegistry
	Memory              framework.MemoryStore
	Config              *framework.Config
	maxIterations       int
	budget              *framework.ContextBudget
	contextManager      *framework.ContextManager
	compressionStrategy framework.CompressionStrategy

	Mode            string
	ModeProfile     ModeRuntimeProfile
	contextStrategy agentctx.ContextStrategy
	progressive     *agentctx.ProgressiveLoader
	sharedContext   *framework.SharedContext
	summarizer      framework.Summarizer
	initialLoadDone bool
}

// Initialize wires configuration.
func (a *ReActAgent) Initialize(config *framework.Config) error {
	a.Config = config
	if config.MaxIterations <= 0 {
		a.maxIterations = 8
	} else {
		a.maxIterations = config.MaxIterations
	}
	if a.Tools == nil {
		a.Tools = framework.NewToolRegistry()
	}
	if a.budget == nil {
		a.budget = framework.NewContextBudget(8000)
	}
	a.budget.SetReservations(1000, 2000, 1000)
	if a.contextManager == nil {
		a.contextManager = framework.NewContextManager(a.budget)
	}
	if a.compressionStrategy == nil {
		a.compressionStrategy = framework.NewSimpleCompressionStrategy()
	}
	if a.Mode == "" {
		a.Mode = "code"
	}
	if a.ModeProfile.Name == "" {
		a.ModeProfile = ModeRuntimeProfile{
			Name:        a.Mode,
			Description: "Reason + Act agent",
			Temperature: 0.2,
			Context: ContextPreferences{
				PreferredDetailLevel: agentctx.DetailDetailed,
				MinHistorySize:       5,
				CompressionThreshold: 0.8,
			},
		}
	}
	if a.contextStrategy == nil {
		switch strings.ToLower(a.Mode) {
		case "debug", "ask":
			a.contextStrategy = agentctx.NewAggressiveStrategy()
		case "architect":
			a.contextStrategy = agentctx.NewConservativeStrategy()
		default:
			a.contextStrategy = agentctx.NewAdaptiveStrategy()
		}
	}
	if a.summarizer == nil {
		a.summarizer = &framework.SimpleSummarizer{}
	}
	if a.progressive == nil {
		a.progressive = agentctx.NewProgressiveLoader(a.contextManager, nil, nil, a.budget, a.summarizer)
	}
	return nil
}

// debugf logs formatted messages whenever agent debug logging is enabled.
func (a *ReActAgent) debugf(format string, args ...interface{}) {
	if a == nil || a.Config == nil || !a.Config.DebugAgent {
		return
	}
	log.Printf("[react] "+format, args...)
}

// Execute runs the task through the workflow graph.
func (a *ReActAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	graph, err := a.BuildGraph(task)
	if err != nil {
		return nil, err
	}
	a.initialLoadDone = false
	a.sharedContext = framework.NewSharedContext(state, a.budget, a.summarizer)
	if a.progressive != nil && a.contextStrategy != nil && task != nil {
		if err := a.progressive.InitialLoad(task, a.contextStrategy); err != nil {
			a.debugf("initial context load failed: %v", err)
		} else {
			a.initialLoadDone = true
		}
	}
	defer func() {
		a.sharedContext = nil
		a.initialLoadDone = false
	}()
	if cfg := a.Config; cfg != nil && cfg.Telemetry != nil {
		graph.SetTelemetry(cfg.Telemetry)
	}
	result, err := graph.Execute(ctx, state)
	return result, err
}

// Capabilities describes what the agent can do.
func (a *ReActAgent) Capabilities() []framework.Capability {
	return []framework.Capability{
		framework.CapabilityPlan,
		framework.CapabilityCode,
		framework.CapabilityExplain,
	}
}

// BuildGraph constructs the ReAct workflow.
func (a *ReActAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	if a.Model == nil {
		return nil, fmt.Errorf("react agent missing language model")
	}
	graph := framework.NewGraph()
	think := &reactThinkNode{
		id:    "react_think",
		agent: a,
		task:  task,
	}
	act := &reactActNode{
		id:    "react_act",
		agent: a,
	}
	observe := &reactObserveNode{
		id:    "react_observe",
		agent: a,
		task:  task,
	}
	terminal := framework.NewTerminalNode("react_done")

	for _, node := range []framework.Node{think, act, observe, terminal} {
		if err := graph.AddNode(node); err != nil {
			return nil, err
		}
	}
	if err := graph.SetStart(think.ID()); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(think.ID(), act.ID(), nil, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(act.ID(), observe.ID(), nil, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(observe.ID(), think.ID(), func(result *framework.Result, ctx *framework.Context) bool {
		done, _ := ctx.Get("react.done")
		return done == false || done == nil
	}, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(observe.ID(), terminal.ID(), func(result *framework.Result, ctx *framework.Context) bool {
		done, _ := ctx.Get("react.done")
		return done == true
	}, false); err != nil {
		return nil, err
	}
	return graph, nil
}

func (a *ReActAgent) enforceBudget(state *framework.Context) {
	if a.budget == nil {
		return
	}
	var tools []framework.Tool
	if a.Tools != nil {
		tools = a.Tools.All()
	}
	a.budget.UpdateUsage(state, tools)
	budgetState := a.budget.CheckBudget()
	if budgetState >= framework.BudgetNeedsCompression && a.Model != nil {
		compressed := false
		if a.sharedContext != nil && a.contextStrategy != nil && a.compressionStrategy != nil {
			if a.contextStrategy.ShouldCompress(a.sharedContext) {
				keep := a.ModeProfile.Context.MinHistorySize
				if keep <= 0 {
					keep = a.compressionStrategy.KeepRecent()
				}
				if keep <= 0 {
					keep = 5
				}
				if err := a.sharedContext.CompressHistory(keep, a.Model, a.compressionStrategy); err != nil {
					a.debugf("shared context compression failed: %v", err)
				} else {
					compressed = true
				}
			}
		}
		if !compressed && a.compressionStrategy != nil {
			if err := state.CompressHistory(a.compressionStrategy.KeepRecent(), a.Model, a.compressionStrategy); err != nil {
				a.debugf("compression failed: %v", err)
			} else {
				compressed = true
			}
		}
		if compressed {
			a.budget.UpdateUsage(state, tools)
		}
	}
	if budgetState == framework.BudgetCritical && a.contextManager != nil {
		targetTokens := a.budget.AvailableForContext / 4
		if targetTokens == 0 {
			targetTokens = 1
		}
		if err := a.contextManager.MakeSpace(targetTokens); err != nil {
			a.debugf("context pruning failed: %v", err)
		}
	}
}

func (a *ReActAgent) recordLatestInteraction(state *framework.Context) {
	if a.contextManager == nil {
		return
	}
	interaction, ok := state.LatestInteraction()
	if !ok {
		return
	}
	item := &framework.InteractionContextItem{
		Interaction: interaction,
		Relevance:   1.0,
		PriorityVal: 1,
	}
	if err := a.contextManager.AddItem(item); err != nil {
		a.debugf("context item add failed: %v", err)
	}
}

func (a *ReActAgent) manageContextSignals(state *framework.Context) {
	if a.contextStrategy == nil {
		return
	}
	lastResult := a.getLastResult(state)
	if a.sharedContext != nil && a.progressive != nil && a.contextStrategy.ShouldExpandContext(a.sharedContext, lastResult) {
		a.expandContextFromResult(lastResult)
	}
	if a.detectUncertainty(state) {
		a.handleUncertainty(state)
	}
}

func (a *ReActAgent) expandContextFromResult(result *framework.Result) {
	if result == nil || result.Data == nil || a.progressive == nil {
		return
	}
	if file, ok := result.Data["file"].(string); ok && file != "" {
		_ = a.progressive.DrillDown(file)
		return
	}
	if focus, ok := result.Data["focus_area"].(string); ok && focus != "" {
		_ = a.progressive.LoadRelatedFiles(focus, 1)
	}
}

func (a *ReActAgent) detectUncertainty(state *framework.Context) bool {
	if state == nil {
		return false
	}
	history := state.History()
	if len(history) == 0 {
		return false
	}
	last := history[len(history)-1]
	content := strings.ToLower(last.Content)
	markers := []string{
		"not sure", "unclear", "need more information",
		"cannot determine", "insufficient context", "missing information",
	}
	for _, marker := range markers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

func (a *ReActAgent) handleUncertainty(state *framework.Context) {
	if state == nil || a.progressive == nil {
		return
	}
	history := state.History()
	if len(history) == 0 {
		return
	}
	last := history[len(history)-1]
	for _, file := range agentctx.ExtractFileReferences(last.Content) {
		_ = a.progressive.ExpandContext(file, agentctx.DetailDetailed)
	}
	if len(agentctx.ExtractSymbolReferences(last.Content)) > 0 {
		request := &agentctx.ContextRequest{
			ASTQueries: []agentctx.ASTQuery{
				{Type: agentctx.ASTQueryListSymbols},
			},
		}
		_ = a.progressive.ExecuteContextRequest(request, "symbol_lookup")
	}
}

func (a *ReActAgent) getLastResult(state *framework.Context) *framework.Result {
	if state == nil {
		return nil
	}
	val, ok := state.Get("react.last_result")
	if !ok {
		return nil
	}
	if res, ok := val.(*framework.Result); ok {
		return res
	}
	return nil
}

// --- ReAct Graph nodes ---

type reactThinkNode struct {
	id    string
	agent *ReActAgent
	task  *framework.Task
}

// ID returns the think node identifier.
func (n *reactThinkNode) ID() string { return n.id }

// Type marks the think step as an observation node.
func (n *reactThinkNode) Type() framework.NodeType { return framework.NodeTypeObservation }

// Execute drives the “think” portion of the ReAct loop and either emits a tool
// call or final answer instructions.
func (n *reactThinkNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("planning")
	n.agent.enforceBudget(state)
	n.agent.manageContextSignals(state)
	var resp *framework.LLMResponse
	var err error
	tools := n.agent.Tools.All()
	useToolCalling := len(tools) > 0 && (n.agent.Config == nil || n.agent.Config.OllamaToolCalling)
	if useToolCalling {
		messages := n.ensureMessages(state, tools)
		resp, err = n.agent.Model.ChatWithTools(ctx, messages, tools, &framework.LLMOptions{
			Model:       n.agent.Config.Model,
			Temperature: 0.1,
			MaxTokens:   512,
		})
		if err == nil {
			messages = append(messages, framework.Message{
				Role:      "assistant",
				Content:   resp.Text,
				ToolCalls: resp.ToolCalls,
			})
			saveReactMessages(state, messages)
		}
	} else {
		prompt := n.buildPrompt(state)
		resp, err = n.agent.Model.Generate(ctx, prompt, &framework.LLMOptions{
			Model:       n.agent.Config.Model,
			Temperature: 0.1,
			MaxTokens:   512,
		})
	}
	if err != nil {
		return nil, err
	}
	state.AddInteraction("assistant", resp.Text, map[string]interface{}{"node": n.id})
	n.agent.recordLatestInteraction(state)
	var decision decisionPayload
	if len(resp.ToolCalls) > 0 {
		decision = decisionPayload{
			Thought:   resp.Text,
			Tool:      resp.ToolCalls[0].Name,
			Arguments: resp.ToolCalls[0].Args,
			Complete:  false,
		}
		state.Set("react.tool_calls", resp.ToolCalls)
	} else if useToolCalling {
		parsed, err := parseDecision(resp.Text)
		if err == nil && (parsed.Tool != "" || parsed.Complete) {
			decision = parsed
		} else {
			decision = decisionPayload{Thought: resp.Text, Complete: true}
		}
		state.Set("react.tool_calls", []framework.ToolCall{})
	} else {
		parsed, err := parseDecision(resp.Text)
		
		// Fallback: Check if the framework helper finds distinct tool calls (e.g. in markdown blocks)
		// even if the single-object parser failed or found nothing.
		detectedCalls := framework.ParseToolCallsFromText(resp.Text)
		
		if len(detectedCalls) > 0 {
			// Found tools via text parsing
			state.Set("react.tool_calls", detectedCalls)
			
			// Use thought from parsed if available, else full text
			thought := parsed.Thought
			if thought == "" {
				thought = resp.Text
			}
			decision = decisionPayload{
				Thought:   thought,
				Complete:  false,
				Timestamp: time.Now().UTC(),
			}
		} else {
			if err != nil {
				decision = decisionPayload{Thought: resp.Text, Complete: true}
			} else {
				decision = parsed
			}
			state.Set("react.tool_calls", []framework.ToolCall{})
		}
	}
	state.Set("react.decision", decision)
	n.agent.debugf("%s decision=%+v tool_calls=%d", n.id, decision, len(resp.ToolCalls))
	return &framework.Result{
		NodeID:  n.id,
		Success: true,
		Data: map[string]interface{}{
			"decision": decision,
		},
	}, nil
}

// buildPrompt returns a textual prompt when tool-calling chat APIs are not
// available.
func (n *reactThinkNode) buildPrompt(state *framework.Context) string {
	var hasLSP, hasAST bool
	for _, tool := range n.agent.Tools.All() {
		if strings.HasPrefix(tool.Name(), "lsp_") {
			hasLSP = true
		}
		if strings.HasPrefix(tool.Name(), "ast_") {
			hasAST = true
		}
	}
	var last string
	if res, ok := state.Get("react.last_tool_result"); ok {
		last = fmt.Sprint(res)
	}

	toolSection := framework.RenderToolsToPrompt(n.agent.Tools.All())

	var guidance strings.Builder
	if hasLSP || hasAST {
		guidance.WriteString("\nCode Analysis:\n")
		if hasLSP {
			guidance.WriteString("- Prefer LSP tools for precise navigation.\n")
		}
		if hasAST {
			guidance.WriteString("- Prefer AST tools for structure queries.\n")
		}
	}
	if val, ok := n.task.Context["plan"]; ok {
		if planJSON, err := json.MarshalIndent(val, "", "  "); err == nil {
			guidance.WriteString("\nPlan:\n")
			guidance.Write(planJSON)
			guidance.WriteRune('\n')
		}
	}

	return fmt.Sprintf(`You are a ReAct agent tasked with "%s".
%s
%s
Recent tool results: %s
Provide your response as a JSON object with "thought" and "tool"/"arguments" fields (or "complete": true).`, n.task.Instruction, toolSection, guidance.String(), last)
}

// ensureMessages seeds the chat history when tool calling is enabled so each
// iteration builds on prior reasoning.
func (n *reactThinkNode) ensureMessages(state *framework.Context, tools []framework.Tool) []framework.Message {
	messages := getReactMessages(state)
	if len(messages) > 0 {
		return messages
	}
	systemPrompt := n.buildSystemPrompt(tools)
	userPrompt := fmt.Sprintf("Task: %s", n.task.Instruction)
	messages = []framework.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	saveReactMessages(state, messages)
	return messages
}

// buildSystemPrompt summarizes tool descriptions for the chat-based workflow.
func (n *reactThinkNode) buildSystemPrompt(tools []framework.Tool) string {
	var lines []string
	var hasLSP, hasAST bool
	for _, tool := range tools {
		lines = append(lines, fmt.Sprintf("- %s: %s", tool.Name(), tool.Description()))
		if strings.HasPrefix(tool.Name(), "lsp_") {
			hasLSP = true
		}
		if strings.HasPrefix(tool.Name(), "ast_") {
			hasAST = true
		}
	}

	var guidance strings.Builder
	if hasLSP || hasAST {
		guidance.WriteString("\n\n### Code Analysis Capabilities\n")
		if hasLSP {
			guidance.WriteString("- Use 'lsp_*' tools to find definitions, references, and type information accurately.\n")
		}
		if hasAST {
			guidance.WriteString("- Use 'ast_*' tools to query the codebase structure (symbols, dependencies) efficiently.\n")
		}
		guidance.WriteString("- Always analyze the code context (definitions/refs) BEFORE attempting edits.\n")
	}

	// Inject Plan if available from Coordinator
	if val, ok := n.task.Context["plan"]; ok {
		// Attempt to marshal plan to JSON for the prompt
		if planJSON, err := json.MarshalIndent(val, "", "  "); err == nil {
			guidance.WriteString("\n\n### Execution Plan\nFollow this plan:\n")
			guidance.Write(planJSON)
			guidance.WriteRune('\n')
		}
	}

	return fmt.Sprintf(`You are a ReAct agent. Think carefully, call tools when required, and finish with a concise summary.
Available tools:
%s%s
When you call a tool, wait for its response before continuing. When the work is complete, provide the final answer as plain text.`, strings.Join(lines, "\n"), guidance.String())
}

type reactActNode struct {
	id    string
	agent *ReActAgent
}

// ID returns the node identifier for the “act” step.
func (n *reactActNode) ID() string { return n.id }

// Type labels the node as a tool execution step.
func (n *reactActNode) Type() framework.NodeType { return framework.NodeTypeTool }

// Execute runs any pending tool calls or directly invokes the requested tool
// referenced in the latest decision payload.
func (n *reactActNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("executing")
	if pending, ok := state.Get("react.tool_calls"); ok {
		if n.agent.Config != nil && !n.agent.Config.OllamaToolCalling {
			state.Set("react.tool_calls", []framework.ToolCall{})
		} else if calls, ok := pending.([]framework.ToolCall); ok && len(calls) > 0 {
			results := make(map[string]interface{})
			toolErrors := make([]string, 0)
			overallSuccess := true
			for _, call := range calls {
				tool, ok := n.agent.Tools.Get(call.Name)
				if !ok {
					return nil, fmt.Errorf("unknown tool %s", call.Name)
				}
				n.agent.debugf("%s executing tool=%s args=%v", n.id, call.Name, call.Args)
				res, err := tool.Execute(ctx, state, call.Args)
				if err != nil {
					return nil, err
				}
				if res != nil {
					results[call.Name] = map[string]interface{}{
						"success": res.Success,
						"data":    res.Data,
						"error":   res.Error,
					}
					appendToolMessage(state, call, res)
					n.agent.debugf("%s tool=%s result=%v", n.id, call.Name, res.Data)
					if !res.Success {
						overallSuccess = false
						if res.Error != "" {
							toolErrors = append(toolErrors, fmt.Sprintf("%s: %s", call.Name, res.Error))
						} else {
							toolErrors = append(toolErrors, fmt.Sprintf("%s failed", call.Name))
						}
					}
				}
			}
			state.Set("react.last_tool_result", results)
			state.Set("react.tool_calls", []framework.ToolCall{})
			result := &framework.Result{NodeID: n.id, Success: overallSuccess, Data: results}
			if len(toolErrors) > 0 {
				result.Error = fmt.Errorf("%s", strings.Join(toolErrors, "; "))
			}
			state.Set("react.last_result", result)
			return result, nil
		}
	}
	val, ok := state.Get("react.decision")
	if !ok {
		return nil, fmt.Errorf("missing decision from think step")
	}
	decision := val.(decisionPayload)
	toolName := strings.TrimSpace(decision.Tool)
	if decision.Complete || toolName == "" || strings.EqualFold(toolName, "none") {
		state.Set("react.last_tool_result", map[string]interface{}{})
		result := &framework.Result{NodeID: n.id, Success: true}
		state.Set("react.last_result", result)
		return result, nil
	}
	tool, ok := n.agent.Tools.Get(toolName)
	if !ok {
		lower := strings.ToLower(toolName)
		if lower == "" || strings.Contains(lower, "none") {
			state.Set("react.last_tool_result", map[string]interface{}{})
			result := &framework.Result{NodeID: n.id, Success: true}
			state.Set("react.last_result", result)
			return result, nil
		}
		return nil, fmt.Errorf("unknown tool %s", toolName)
	}
	res, err := tool.Execute(ctx, state, decision.Arguments)
	if err != nil {
		return nil, err
	}
	state.Set("react.last_tool_result", res.Data)
	n.agent.debugf("%s tool=%s result=%v", n.id, decision.Tool, res.Data)
	result := &framework.Result{
		NodeID:  n.id,
		Success: res.Success,
		Data:    res.Data,
		Error:   parseError(res.Error),
	}
	state.Set("react.last_result", result)
	return result, nil
}

type reactObserveNode struct {
	id    string
	agent *ReActAgent
	task  *framework.Task
}

// ID returns the node identifier for the observe step.
func (n *reactObserveNode) ID() string { return n.id }

// Type marks the step as an observation/validation pass.
func (n *reactObserveNode) Type() framework.NodeType { return framework.NodeTypeObservation }

// Execute captures tool output, tracks loop iterations, and determines whether
// the ReAct loop should continue.
func (n *reactObserveNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("validating")
	iterVal, _ := state.Get("react.iteration")
	iter, _ := iterVal.(int)
	iter++
	state.Set("react.iteration", iter)
	decisionVal, _ := state.Get("react.decision")
	decision, _ := decisionVal.(decisionPayload)
	lastRes, _ := state.Get("react.last_tool_result")
	lastMap, _ := lastRes.(map[string]interface{})
	var diagnostic strings.Builder
	diagnostic.WriteString(fmt.Sprintf("Iteration %d observation.\n", iter))
	if decision.Thought != "" {
		diagnostic.WriteString("Thought: " + decision.Thought + "\n")
	}
	if len(lastMap) > 0 {
		diagnostic.WriteString("Tool Result: ")
		diagnostic.WriteString(fmt.Sprint(lastMap))
		diagnostic.WriteRune('\n')
	}
	completed := decision.Complete || iter >= n.agent.maxIterations
	if res, ok := state.Get("react.tool_calls"); ok {
		if calls, ok := res.([]framework.ToolCall); ok && len(calls) > 0 {
			completed = false
		}
	}
	state.Set("react.done", completed)

	if n.agent.Memory != nil {
		_ = n.agent.Memory.Remember(ctx, NewUUID(), map[string]interface{}{
			"task":      n.task.Instruction,
			"iteration": iter,
			"decision":  decision,
		}, framework.MemoryScopeSession)
	}

	if completed {
		state.Set("react.final_output", map[string]interface{}{
			"summary": diagnostic.String(),
			"result":  lastMap,
		})
	}
	n.agent.debugf("%s completed=%v diagnostic=%s", n.id, completed, diagnostic.String())
	result := &framework.Result{
		NodeID:  n.id,
		Success: true,
		Data: map[string]interface{}{
			"diagnostic": diagnostic.String(),
			"complete":   completed,
		},
	}
	state.Set("react.last_result", result)
	return result, nil
}

// decisionPayload models the JSON output of the think step.
type decisionPayload struct {
	Thought   string                 `json:"thought"`
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments"`
	Complete  bool                   `json:"complete"`
	Reason    string                 `json:"reason"`
	Timestamp time.Time              `json:"timestamp"`
}

// parseDecision extracts the model's JSON payload (or falls back to the raw
// text) and normalizes it into the decisionPayload struct.
func parseDecision(raw string) (decisionPayload, error) {
	var payload decisionPayload
	snippet := ExtractJSON(raw)
	if snippet == "{}" {
		payload.Thought = strings.TrimSpace(raw)
		payload.Complete = true
		payload.Timestamp = time.Now().UTC()
		return payload, nil
	}
	var generic map[string]interface{}
	if err := json.Unmarshal([]byte(snippet), &generic); err != nil {
		return payload, err
	}
	if thought, ok := generic["thought"].(string); ok && thought != "" {
		payload.Thought = thought
	} else if payload.Thought == "" {
		payload.Thought = strings.TrimSpace(raw)
	}
	if tool, ok := generic["tool"].(string); ok {
		payload.Tool = tool
	} else if name, ok := generic["name"].(string); ok {
		payload.Tool = name
	}
	if args, ok := generic["arguments"]; ok {
		payload.Arguments = normalizeArguments(args)
	}
	if payload.Arguments == nil {
		payload.Arguments = map[string]interface{}{}
	}
	if complete, ok := generic["complete"].(bool); ok {
		payload.Complete = complete
	}
	if reason, ok := generic["reason"].(string); ok {
		payload.Reason = reason
	}
	payload.Timestamp = time.Now().UTC()
	return payload, nil
}

// normalizeArguments coerces stringified JSON arguments into maps so tools
// always receive structured input.
func normalizeArguments(value interface{}) map[string]interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return v
	case string:
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(v), &obj); err == nil {
			return obj
		}
		return map[string]interface{}{"value": v}
	default:
		return map[string]interface{}{}
	}
}

// parseError converts an error message string into an error value.
func parseError(err string) error {
	if err == "" {
		return nil
	}
	return errors.New(err)
}

const reactMessagesKey = "react.messages"

// getReactMessages reads a copy of the stored chat transcript.
func getReactMessages(state *framework.Context) []framework.Message {
	raw, ok := state.Get(reactMessagesKey)
	if !ok {
		return nil
	}
	messages, ok := raw.([]framework.Message)
	if !ok || len(messages) == 0 {
		return nil
	}
	copyMessages := make([]framework.Message, len(messages))
	copy(copyMessages, messages)
	return copyMessages
}

// saveReactMessages overwrites the stored transcript with a defensive copy.
func saveReactMessages(state *framework.Context, messages []framework.Message) {
	if len(messages) == 0 {
		state.Set(reactMessagesKey, []framework.Message{})
		return
	}
	copyMessages := make([]framework.Message, len(messages))
	copy(copyMessages, messages)
	state.Set(reactMessagesKey, copyMessages)
}

// appendToolMessage records tool responses in the transcript so the LLM can
// observe prior results when tool calling is used.
func appendToolMessage(state *framework.Context, call framework.ToolCall, res *framework.ToolResult) {
	messages := getReactMessages(state)
	if len(messages) == 0 || res == nil {
		return
	}
	payload := map[string]interface{}{
		"success": res.Success,
	}
	if len(res.Data) > 0 {
		payload["data"] = res.Data
	}
	if res.Error != "" {
		payload["error"] = res.Error
	}
	if len(res.Metadata) > 0 {
		payload["metadata"] = res.Metadata
	}
	encoded, err := json.Marshal(payload)
	content := string(encoded)
	if err != nil {
		content = fmt.Sprintf("success=%t data=%v error=%s", res.Success, res.Data, res.Error)
	}
	messages = append(messages, framework.Message{
		Role:       "tool",
		Name:       call.Name,
		Content:    content,
		ToolCallID: call.ID,
	})
	saveReactMessages(state, messages)
}
