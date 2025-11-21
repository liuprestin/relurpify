package agents

import (
	"context"
	"fmt"

	"github.com/lexcodex/relurpify/framework"
)

// CodingAgent focuses on code-aware tasks.
type CodingAgent struct {
	Model      framework.LanguageModel
	Tools      *framework.ToolRegistry
	Memory     framework.MemoryStore
	Config     *framework.Config
	reactAgent *ReActAgent
}

// Initialize configures dependencies.
func (a *CodingAgent) Initialize(cfg *framework.Config) error {
	a.Config = cfg
	if a.Tools == nil {
		a.Tools = framework.NewToolRegistry()
	}
	a.reactAgent = &ReActAgent{
		Model:  a.Model,
		Tools:  a.Tools,
		Memory: a.Memory,
		Config: cfg,
	}
	return a.reactAgent.Initialize(cfg)
}

// Capabilities reports available features.
func (a *CodingAgent) Capabilities() []framework.Capability {
	return []framework.Capability{
		framework.CapabilityCode,
		framework.CapabilityRefactor,
		framework.CapabilityExplain,
		framework.CapabilityExecute,
	}
}

// Execute runs the coding workflow.
func (a *CodingAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	graph, err := a.BuildGraph(task)
	if err != nil {
		return nil, err
	}
	return graph.Execute(ctx, state)
}

// BuildGraph builds analysis -> coding -> validation flow.
func (a *CodingAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	if a.Model == nil {
		return nil, fmt.Errorf("coding agent missing model")
	}
	graph := framework.NewGraph()
	analyze := &codingAnalyzeNode{id: "coder_analyze", agent: a, task: task}
	modify := &codingModifyNode{id: "coder_modify", agent: a, task: task}
	validate := &codingValidateNode{id: "coder_validate", agent: a}
	done := framework.NewTerminalNode("coder_done")

	for _, node := range []framework.Node{analyze, modify, validate, done} {
		if err := graph.AddNode(node); err != nil {
			return nil, err
		}
	}
	if err := graph.SetStart(analyze.ID()); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(analyze.ID(), modify.ID(), nil, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(modify.ID(), validate.ID(), nil, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(validate.ID(), done.ID(), nil, false); err != nil {
		return nil, err
	}
	return graph, nil
}

type codingAnalyzeNode struct {
	id    string
	agent *CodingAgent
	task  *framework.Task
}

func (n *codingAnalyzeNode) ID() string               { return n.id }
func (n *codingAnalyzeNode) Type() framework.NodeType { return framework.NodeTypeObservation }

func (n *codingAnalyzeNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("analysis")
	prompt := fmt.Sprintf(`You are analyzing Go code for task "%s".
List the relevant files, risks, and plan as JSON {\"plan\":[], \"files\":[], \"risks\":[]}.`, n.task.Instruction)
	resp, err := n.agent.Model.Generate(ctx, prompt, &framework.LLMOptions{
		Model:       n.agent.Config.Model,
		MaxTokens:   400,
		Temperature: 0.1,
	})
	if err != nil {
		return nil, err
	}
	state.AddInteraction("assistant", resp.Text, map[string]interface{}{"node": n.id})
	state.Set("coder.analysis", resp.Text)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"analysis": resp.Text}}, nil
}

type codingModifyNode struct {
	id    string
	agent *CodingAgent
	task  *framework.Task
}

func (n *codingModifyNode) ID() string               { return n.id }
func (n *codingModifyNode) Type() framework.NodeType { return framework.NodeTypeSystem }

func (n *codingModifyNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("coding")
	return n.agent.reactAgent.Execute(ctx, n.task, state)
}

type codingValidateNode struct {
	id    string
	agent *CodingAgent
}

func (n *codingValidateNode) ID() string               { return n.id }
func (n *codingValidateNode) Type() framework.NodeType { return framework.NodeTypeObservation }

func (n *codingValidateNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("validating")
	tool, ok := n.agent.Tools.Get("exec_run_tests")
	if !ok {
		state.Set("coder.tests", "skipped")
		return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"tests": "skipped"}}, nil
	}
	res, err := tool.Execute(ctx, state, map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	state.Set("coder.tests", res.Data)
	return &framework.Result{NodeID: n.id, Success: res.Success, Data: res.Data, Error: toolErr(res.Error)}, nil
}

func toolErr(err string) error {
	if err == "" {
		return nil
	}
	return fmt.Errorf(err)
}
