package react

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lexcodex/relurpify/framework"
)

type stubLLM struct {
	responses      []*framework.LLMResponse
	idx            int
	generateCalls  int
	withToolsCalls int
}

func (s *stubLLM) Generate(ctx context.Context, prompt string, options *framework.LLMOptions) (*framework.LLMResponse, error) {
	s.generateCalls++
	return s.nextResponse()
}

func (s *stubLLM) GenerateStream(ctx context.Context, prompt string, options *framework.LLMOptions) (<-chan string, error) {
	return nil, errors.New("not implemented")
}

func (s *stubLLM) Chat(ctx context.Context, messages []framework.Message, options *framework.LLMOptions) (*framework.LLMResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *stubLLM) ChatWithTools(ctx context.Context, messages []framework.Message, tools []framework.Tool, options *framework.LLMOptions) (*framework.LLMResponse, error) {
	s.withToolsCalls++
	return s.nextResponse()
}

func (s *stubLLM) nextResponse() (*framework.LLMResponse, error) {
	if s.idx >= len(s.responses) {
		return nil, errors.New("no response")
	}
	resp := s.responses[s.idx]
	s.idx++
	return resp, nil
}

type stubTool struct {
	name string
}

func (t stubTool) Name() string        { return t.name }
func (t stubTool) Description() string { return "stub tool" }
func (t stubTool) Category() string    { return "test" }
func (t stubTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "value", Type: "string", Required: false},
	}
}
func (t stubTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	return &framework.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"echo": args["value"],
		},
	}, nil
}
func (t stubTool) IsAvailable(ctx context.Context, state *framework.Context) bool { return true }
func (t stubTool) Permissions() framework.ToolPermissions {
	return framework.ToolPermissions{Permissions: &framework.PermissionSet{
		FileSystem: []framework.FileSystemPermission{
			{Action: framework.FileSystemRead, Path: "**"},
		},
	}}
}

func TestReActAgentExecute(t *testing.T) {
	llm := &stubLLM{
		responses: []*framework.LLMResponse{
			{Text: `{"thought":"finished","tool":"none","arguments":{},"complete":true}`},
		},
	}
	registry := framework.NewToolRegistry()
	assert.NoError(t, registry.Register(stubTool{name: "echo"}))
	agent := &ReActAgent{
		Model: llm,
		Tools: registry,
	}
	cfg := &framework.Config{Model: "test-model", MaxIterations: 2}
	assert.NoError(t, agent.Initialize(cfg))

	task := &framework.Task{ID: "task-1", Instruction: "do something"}
	state := framework.NewContext()
	state.Set("task.id", task.ID)

	think := &reactThinkNode{id: "think", agent: agent, task: task}
	act := &reactActNode{id: "act", agent: agent}
	observe := &reactObserveNode{id: "observe", agent: agent, task: task}
	terminal := framework.NewTerminalNode("done")

	graph := framework.NewGraph()
	assert.NoError(t, graph.AddNode(think))
	assert.NoError(t, graph.AddNode(act))
	assert.NoError(t, graph.AddNode(observe))
	assert.NoError(t, graph.AddNode(terminal))
	assert.NoError(t, graph.SetStart("think"))
	assert.NoError(t, graph.AddEdge("think", "act", nil, false))
	assert.NoError(t, graph.AddEdge("act", "observe", nil, false))
	assert.NoError(t, graph.AddEdge("observe", "done", nil, false))

	// run single pass (no loop) to validate node behavior
	result, err := graph.Execute(context.Background(), state)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "done", result.NodeID)

	final, ok := state.Get("react.final_output")
	assert.True(t, ok, "final output should be stored in context")
	assert.Contains(t, final.(map[string]interface{})["summary"], "Iteration")
	assert.Equal(t, 1, llm.withToolsCalls)
	assert.Equal(t, 0, llm.generateCalls)
}

func TestReActAgentToolCallingDisabled(t *testing.T) {
	llm := &stubLLM{
		responses: []*framework.LLMResponse{
			{Text: `{"thought":"finished","tool":"none","arguments":{},"complete":true}`},
		},
	}
	registry := framework.NewToolRegistry()
	assert.NoError(t, registry.Register(stubTool{name: "echo"}))
	agent := &ReActAgent{
		Model: llm,
		Tools: registry,
	}
	cfg := &framework.Config{Model: "test-model", MaxIterations: 2, DisableToolCalling: true}
	assert.NoError(t, agent.Initialize(cfg))

	task := &framework.Task{ID: "task-2", Instruction: "do something"}
	state := framework.NewContext()
	state.Set("task.id", task.ID)

	think := &reactThinkNode{id: "think", agent: agent, task: task}
	act := &reactActNode{id: "act", agent: agent}
	observe := &reactObserveNode{id: "observe", agent: agent, task: task}
	terminal := framework.NewTerminalNode("done")

	graph := framework.NewGraph()
	assert.NoError(t, graph.AddNode(think))
	assert.NoError(t, graph.AddNode(act))
	assert.NoError(t, graph.AddNode(observe))
	assert.NoError(t, graph.AddNode(terminal))
	assert.NoError(t, graph.SetStart("think"))
	assert.NoError(t, graph.AddEdge("think", "act", nil, false))
	assert.NoError(t, graph.AddEdge("act", "observe", nil, false))
	assert.NoError(t, graph.AddEdge("observe", "done", nil, false))

	result, err := graph.Execute(context.Background(), state)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "done", result.NodeID)
	assert.Equal(t, 0, llm.withToolsCalls)
	assert.Equal(t, 1, llm.generateCalls)
}

func TestReActAgentToolCalling(t *testing.T) {
	llm := &stubLLM{
		responses: []*framework.LLMResponse{
			{Text: "", ToolCalls: []framework.ToolCall{{Name: "echo", Args: map[string]interface{}{"value": "hi"}}}},
			{Text: "all done"},
		},
	}
	registry := framework.NewToolRegistry()
	assert.NoError(t, registry.Register(stubTool{name: "echo"}))
	agent := &ReActAgent{
		Model: llm,
		Tools: registry,
	}
	cfg := &framework.Config{Model: "test-model", MaxIterations: 3}
	assert.NoError(t, agent.Initialize(cfg))

	task := &framework.Task{ID: "task-2", Instruction: "use tool"}
	state := framework.NewContext()
	state.Set("task.id", task.ID)

	think := &reactThinkNode{id: "think", agent: agent, task: task}
	act := &reactActNode{id: "act", agent: agent}
	observe := &reactObserveNode{id: "observe", agent: agent, task: task}
	terminal := framework.NewTerminalNode("done")

	graph := framework.NewGraph()
	assert.NoError(t, graph.AddNode(think))
	assert.NoError(t, graph.AddNode(act))
	assert.NoError(t, graph.AddNode(observe))
	assert.NoError(t, graph.AddNode(terminal))
	assert.NoError(t, graph.SetStart("think"))
	assert.NoError(t, graph.AddEdge("think", "act", nil, false))
	assert.NoError(t, graph.AddEdge("act", "observe", nil, false))
	assert.NoError(t, graph.AddEdge("observe", "done", nil, false))

	result, err := graph.Execute(context.Background(), state)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "done", result.NodeID)
	assert.Equal(t, 1, llm.withToolsCalls)

	lastToolRes, ok := state.Get("react.last_tool_result")
	assert.True(t, ok)
	assert.Contains(t, fmt.Sprint(lastToolRes.(map[string]interface{})["echo"]), "hi")

	messagesVal, ok := state.Get("react.messages")
	assert.True(t, ok)
	messages, ok := messagesVal.([]framework.Message)
	assert.True(t, ok)
	var toolMessages int
	for _, msg := range messages {
		if msg.Role == "tool" {
			toolMessages++
			assert.Equal(t, "echo", msg.Name)
			assert.Contains(t, msg.Content, "success")
		}
	}
	assert.Equal(t, 1, toolMessages)
}
