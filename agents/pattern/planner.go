package pattern

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lexcodex/relurpify/framework"
)

// PlannerAgent builds a plan before executing. It is intentionally explicit:
// first ask the LLM for a structured plan, then execute tool-backed steps,
// finally verify + summarize. The separation mirrors how human operators would
// tackle unfamiliar tasks and serves as reference implementation for creating
// new multi-step agents.
type PlannerAgent struct {
	Model  framework.LanguageModel
	Tools  *framework.ToolRegistry
	Memory framework.MemoryStore
	Config *framework.Config
}

// Initialize configures the agent.
func (a *PlannerAgent) Initialize(cfg *framework.Config) error {
	a.Config = cfg
	if a.Tools == nil {
		a.Tools = framework.NewToolRegistry()
	}
	return nil
}

// Execute runs the planner workflow.
func (a *PlannerAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	graph, err := a.BuildGraph(task)
	if err != nil {
		return nil, err
	}
	return graph.Execute(ctx, state)
}

// Capabilities enumerates features.
func (a *PlannerAgent) Capabilities() []framework.Capability {
	return []framework.Capability{
		framework.CapabilityPlan,
		framework.CapabilityExecute,
	}
}

// BuildGraph builds planning pipeline with explicit plan→execute→verify stages.
// Returning a Graph instead of hiding the workflow inside Execute keeps the
// system debuggable and allows other packages to analyze the structure.
func (a *PlannerAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	if a.Model == nil {
		return nil, fmt.Errorf("planner agent missing model")
	}
	graph := framework.NewGraph()
	planNode := &plannerPlanNode{id: "planner_plan", agent: a, task: task}
	execNode := &plannerExecuteNode{id: "planner_execute", agent: a}
	verifyNode := &plannerVerifyNode{id: "planner_verify", agent: a, task: task}
	done := framework.NewTerminalNode("planner_done")

	for _, node := range []framework.Node{planNode, execNode, verifyNode, done} {
		if err := graph.AddNode(node); err != nil {
			return nil, err
		}
	}
	if err := graph.SetStart(planNode.ID()); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(planNode.ID(), execNode.ID(), nil, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(execNode.ID(), verifyNode.ID(), nil, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(verifyNode.ID(), done.ID(), nil, false); err != nil {
		return nil, err
	}
	return graph, nil
}

type plannerPlanNode struct {
	id    string
	agent *PlannerAgent
	task  *framework.Task
}

func (n *plannerPlanNode) ID() string               { return n.id }
func (n *plannerPlanNode) Type() framework.NodeType { return framework.NodeTypeSystem }

// Execute prompts the LLM for a machine-readable plan. The JSON schema is small
// enough that contributors can tweak it without retraining anything.
func (n *plannerPlanNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("planning")
	prompt := fmt.Sprintf(`You are a planning agent. Break this task into steps with dependencies.
Task: %s
Return valid JSON Plan struct with fields goal, steps (array of {id, description, tool, params, expected, verification}).
`, n.task.Instruction)
	resp, err := n.agent.Model.Generate(ctx, prompt, &framework.LLMOptions{
		Model:       n.agent.Config.Model,
		Temperature: 0.2,
		MaxTokens:   800,
	})
	if err != nil {
		return nil, err
	}
	state.AddInteraction("assistant", resp.Text, map[string]interface{}{"node": n.id})
	plan, err := parsePlan(resp.Text)
	if err != nil {
		return nil, err
	}
	state.Set("planner.plan", plan)
	if n.agent.Memory != nil {
		_ = n.agent.Memory.Remember(ctx, NewUUID(), map[string]interface{}{
			"type": "plan",
			"plan": plan,
		}, framework.MemoryScopeSession)
	}
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"plan": plan}}, nil
}

type plannerExecuteNode struct {
	id    string
	agent *PlannerAgent
}

func (n *plannerExecuteNode) ID() string               { return n.id }
func (n *plannerExecuteNode) Type() framework.NodeType { return framework.NodeTypeTool }

// Execute iterates the generated plan and calls the requested tool for each
// actionable step. Empty tool names are skipped, which keeps the agent tolerant
// to “reasoning only” steps the LLM might propose.
func (n *plannerExecuteNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("executing")
	value, ok := state.Get("planner.plan")
	if !ok {
		return nil, fmt.Errorf("plan not available")
	}
	plan, _ := value.(framework.Plan)
	var stepResults []map[string]interface{}
	for _, step := range plan.Steps {
		if step.Tool == "" {
			continue
		}
		tool, ok := n.agent.Tools.Get(step.Tool)
		if !ok {
			return nil, fmt.Errorf("tool %s not registered", step.Tool)
		}
		result, err := tool.Execute(ctx, state, step.Params)
		if err != nil {
			return nil, err
		}
		stepResults = append(stepResults, map[string]interface{}{
			"id":     step.ID,
			"output": result.Data,
		})
		state.Set(fmt.Sprintf("planner.step.%d", step.ID), result.Data)
	}
	state.Set("planner.results", stepResults)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"results": stepResults}}, nil
}

type plannerVerifyNode struct {
	id    string
	agent *PlannerAgent
	task  *framework.Task
}

func (n *plannerVerifyNode) ID() string               { return n.id }
func (n *plannerVerifyNode) Type() framework.NodeType { return framework.NodeTypeObservation }

// Execute packages the observed tool outputs into a short summary so downstream
// systems (CLI, LSP, tests) can display human-friendly “what just happened”
// messages without parsing the entire state map.
func (n *plannerVerifyNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("validating")
	results, _ := state.Get("planner.results")
	planVal, _ := state.Get("planner.plan")
	plan, _ := planVal.(framework.Plan)
	summary := fmt.Sprintf("Executed plan for task '%s' with %d steps.", n.task.Instruction, len(plan.Steps))
	state.Set("planner.summary", summary)
	if n.agent.Memory != nil {
		_ = n.agent.Memory.Remember(ctx, NewUUID(), map[string]interface{}{
			"type":    "verification",
			"summary": summary,
			"results": results,
		}, framework.MemoryScopeSession)
	}
	return &framework.Result{
		NodeID:  n.id,
		Success: true,
		Data: map[string]interface{}{
			"summary": summary,
		},
	}, nil
}

// parsePlan pulls the JSON payload out of the model response. The helper keeps
// PlannerAgent.Execute easy to read and doubles as a seam for unit tests.
func parsePlan(raw string) (framework.Plan, error) {
	var plan framework.Plan
	if err := json.Unmarshal([]byte(ExtractJSON(raw)), &plan); err != nil {
		return plan, err
	}
	if plan.Dependencies == nil {
		plan.Dependencies = make(map[int][]int)
	}
	return plan, nil
}
