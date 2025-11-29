package react

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lexcodex/relurpify/agents/internal/common"
	"github.com/lexcodex/relurpify/agents/internal/parse"
	"github.com/lexcodex/relurpify/framework"
)

// ReActAgent implements the Reason+Act pattern.
type ReActAgent struct {
	Model         framework.LanguageModel
	Tools         *framework.ToolRegistry
	Memory        framework.MemoryStore
	Config        *framework.Config
	maxIterations int
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
	return nil
}

// Execute runs the task through the workflow graph.
func (a *ReActAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	graph, err := a.BuildGraph(task)
	if err != nil {
		return nil, err
	}
	return graph.Execute(ctx, state)
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

// --- ReAct Graph nodes ---

type reactThinkNode struct {
	id    string
	agent *ReActAgent
	task  *framework.Task
}

func (n *reactThinkNode) ID() string               { return n.id }
func (n *reactThinkNode) Type() framework.NodeType { return framework.NodeTypeObservation }

func (n *reactThinkNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("planning")
	var resp *framework.LLMResponse
	var err error
	tools := n.agent.Tools.All()
	useToolCalling := len(tools) > 0 && (n.agent.Config == nil || !n.agent.Config.DisableToolCalling)
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
		decision = decisionPayload{Thought: resp.Text, Complete: true}
		state.Set("react.tool_calls", []framework.ToolCall{})
	} else {
		parsed, err := parseDecision(resp.Text)
		if err != nil {
			decision = decisionPayload{Thought: resp.Text, Complete: true}
		} else {
			decision = parsed
		}
		state.Set("react.tool_calls", []framework.ToolCall{})
	}
	state.Set("react.decision", decision)
	return &framework.Result{
		NodeID:  n.id,
		Success: true,
		Data: map[string]interface{}{
			"decision": decision,
		},
	}, nil
}

func (n *reactThinkNode) buildPrompt(state *framework.Context) string {
	var tools []string
	for _, tool := range n.agent.Tools.All() {
		toolLine := fmt.Sprintf("%s: %s", tool.Name(), tool.Description())
		tools = append(tools, toolLine)
	}
	var last string
	if res, ok := state.Get("react.last_tool_result"); ok {
		last = fmt.Sprint(res)
	}
	return fmt.Sprintf(`You are a ReAct agent tasked with "%s".
You can call functions using the provided tools. If you need a tool, emit a tool call. If you are done, provide the final answer text without tool calls or return the JSON object: {"thought": "...", "tool": "tool_name or none", "arguments": {...}, "complete": bool}
Available tools:
%s
Recent tool results: %s`, n.task.Instruction, strings.Join(tools, "\n"), last)
}

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

func (n *reactThinkNode) buildSystemPrompt(tools []framework.Tool) string {
	var lines []string
	for _, tool := range tools {
		lines = append(lines, fmt.Sprintf("- %s: %s", tool.Name(), tool.Description()))
	}
	return fmt.Sprintf(`You are a ReAct agent. Think carefully, call tools when required, and finish with a concise summary.
Available tools:
%s
When you call a tool, wait for its response before continuing. When the work is complete, provide the final answer as plain text.`, strings.Join(lines, "\n"))
}

type reactActNode struct {
	id    string
	agent *ReActAgent
}

func (n *reactActNode) ID() string               { return n.id }
func (n *reactActNode) Type() framework.NodeType { return framework.NodeTypeTool }

func (n *reactActNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("executing")
	if pending, ok := state.Get("react.tool_calls"); ok {
		if calls, ok := pending.([]framework.ToolCall); ok && len(calls) > 0 {
			results := make(map[string]interface{})
			for _, call := range calls {
				tool, ok := n.agent.Tools.Get(call.Name)
				if !ok {
					return nil, fmt.Errorf("unknown tool %s", call.Name)
				}
				res, err := tool.Execute(ctx, state, call.Args)
				if err != nil {
					return nil, err
				}
				if res != nil {
					results[call.Name] = res.Data
					appendToolMessage(state, call, res)
				}
			}
			state.Set("react.last_tool_result", results)
			state.Set("react.tool_calls", []framework.ToolCall{})
			return &framework.Result{NodeID: n.id, Success: true, Data: results}, nil
		}
	}
	val, ok := state.Get("react.decision")
	if !ok {
		return nil, fmt.Errorf("missing decision from think step")
	}
	decision := val.(decisionPayload)
	if decision.Complete || decision.Tool == "" || decision.Tool == "none" {
		state.Set("react.last_tool_result", map[string]interface{}{})
		return &framework.Result{NodeID: n.id, Success: true}, nil
	}
	tool, ok := n.agent.Tools.Get(decision.Tool)
	if !ok {
		return nil, fmt.Errorf("unknown tool %s", decision.Tool)
	}
	res, err := tool.Execute(ctx, state, decision.Arguments)
	if err != nil {
		return nil, err
	}
	state.Set("react.last_tool_result", res.Data)
	return &framework.Result{
		NodeID:  n.id,
		Success: res.Success,
		Data:    res.Data,
		Error:   parseError(res.Error),
	}, nil
}

type reactObserveNode struct {
	id    string
	agent *ReActAgent
	task  *framework.Task
}

func (n *reactObserveNode) ID() string               { return n.id }
func (n *reactObserveNode) Type() framework.NodeType { return framework.NodeTypeObservation }

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
		_ = n.agent.Memory.Remember(ctx, common.NewUUID(), map[string]interface{}{
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
	return &framework.Result{
		NodeID:  n.id,
		Success: true,
		Data: map[string]interface{}{
			"diagnostic": diagnostic.String(),
			"complete":   completed,
		},
	}, nil
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

func parseDecision(raw string) (decisionPayload, error) {
	var payload decisionPayload
	if err := json.Unmarshal([]byte(parse.ExtractJSON(raw)), &payload); err != nil {
		return payload, err
	}
	payload.Timestamp = time.Now().UTC()
	return payload, nil
}

func parseError(err string) error {
	if err == "" {
		return nil
	}
	return errors.New(err)
}

const reactMessagesKey = "react.messages"

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

func saveReactMessages(state *framework.Context, messages []framework.Message) {
	if len(messages) == 0 {
		state.Set(reactMessagesKey, []framework.Message{})
		return
	}
	copyMessages := make([]framework.Message, len(messages))
	copy(copyMessages, messages)
	state.Set(reactMessagesKey, copyMessages)
}

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
