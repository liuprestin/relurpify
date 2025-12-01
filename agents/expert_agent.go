package agents

import (
	"context"
	"fmt"

	"github.com/lexcodex/relurpify/framework"
)

// ExpertCoderAgent chains the architect planner with the coding delegate,
// mirroring the pipeline pattern from the specification.
type ExpertCoderAgent struct {
	Model  framework.LanguageModel
	Tools  *framework.ToolRegistry
	Memory framework.MemoryStore
	Config *framework.Config

	planner *PlannerAgent
	coder   *CodingAgent
}

// Initialize configures the planner and coding delegates.
func (a *ExpertCoderAgent) Initialize(cfg *framework.Config) error {
	a.Config = cfg
	if a.Tools == nil {
		a.Tools = framework.NewToolRegistry()
	}
	a.planner = &PlannerAgent{Model: a.Model, Tools: a.Tools, Memory: a.Memory}
	if err := a.planner.Initialize(cfg); err != nil {
		return err
	}
	a.coder = &CodingAgent{Model: a.Model, Tools: a.Tools, Memory: a.Memory}
	return a.coder.Initialize(cfg)
}

// Capabilities merges planning and coding skills.
func (a *ExpertCoderAgent) Capabilities() []framework.Capability {
	return []framework.Capability{
		framework.CapabilityPlan,
		framework.CapabilityCode,
		framework.CapabilityReview,
		framework.CapabilityExplain,
	}
}

// BuildGraph constructs a pipeline graph.
func (a *ExpertCoderAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	graph := framework.NewGraph()
	plan := &expertPlanNode{id: "expert_plan", agent: a, task: task}
	implement := &expertImplementNode{id: "expert_implement", agent: a, task: task}
	done := framework.NewTerminalNode("expert_done")
	if err := graph.AddNode(plan); err != nil {
		return nil, err
	}
	if err := graph.AddNode(implement); err != nil {
		return nil, err
	}
	if err := graph.AddNode(done); err != nil {
		return nil, err
	}
	if err := graph.SetStart(plan.ID()); err != nil {
		return nil, err
	}
	_ = graph.AddEdge(plan.ID(), implement.ID(), nil, false)
	_ = graph.AddEdge(implement.ID(), done.ID(), nil, false)
	return graph, nil
}

// Execute runs plan then coding mode.
func (a *ExpertCoderAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	graph, err := a.BuildGraph(task)
	if err != nil {
		return nil, err
	}
	return graph.Execute(ctx, state)
}

type expertPlanNode struct {
	id    string
	agent *ExpertCoderAgent
	task  *framework.Task
}

func (n *expertPlanNode) ID() string               { return n.id }
func (n *expertPlanNode) Type() framework.NodeType { return framework.NodeTypeSystem }

func (n *expertPlanNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("planning")
	result, err := n.agent.planner.Execute(ctx, n.task, state)
	if err != nil {
		return nil, err
	}
	state.Set("expert.plan", result.Data)
	return &framework.Result{
		NodeID:  n.id,
		Success: true,
		Data:    result.Data,
	}, nil
}

type expertImplementNode struct {
	id    string
	agent *ExpertCoderAgent
	task  *framework.Task
}

func (n *expertImplementNode) ID() string               { return n.id }
func (n *expertImplementNode) Type() framework.NodeType { return framework.NodeTypeSystem }

func (n *expertImplementNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("executing")
	task := *n.task
	if task.Context == nil {
		task.Context = map[string]any{}
	}
	task.Context["mode"] = string(ModeCode)
	task.Context["plan"] = mustGet(state, "expert.plan")
	result, err := n.agent.coder.Execute(ctx, &task, state)
	if err != nil {
		return nil, err
	}
	return &framework.Result{NodeID: n.id, Success: true, Data: result.Data}, nil
}

func mustGet(ctx *framework.Context, key string) interface{} {
	value, ok := ctx.Get(key)
	if !ok {
		panic(fmt.Sprintf("missing context key %s", key))
	}
	return value
}
