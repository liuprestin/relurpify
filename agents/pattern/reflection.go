package pattern

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lexcodex/relurpify/framework"
)

// ReflectionAgent reviews outputs and triggers revisions when needed.
type ReflectionAgent struct {
	Reviewer      framework.LanguageModel
	Delegate      framework.Agent
	Config        *framework.Config
	maxIterations int
}

// Initialize configures the reviewer.
func (a *ReflectionAgent) Initialize(cfg *framework.Config) error {
	a.Config = cfg
	if cfg.MaxIterations <= 0 {
		a.maxIterations = 3
	} else {
		a.maxIterations = cfg.MaxIterations
	}
	return nil
}

// Execute runs the review workflow.
func (a *ReflectionAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	graph, err := a.BuildGraph(task)
	if err != nil {
		return nil, err
	}
	return graph.Execute(ctx, state)
}

// Capabilities returns capabilities.
func (a *ReflectionAgent) Capabilities() []framework.Capability {
	return []framework.Capability{framework.CapabilityReview}
}

// BuildGraph builds the review workflow.
func (a *ReflectionAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	if a.Delegate == nil {
		return nil, fmt.Errorf("reflection agent missing delegate")
	}
	if a.Reviewer == nil {
		return nil, fmt.Errorf("reflection agent missing reviewer model")
	}
	graph := framework.NewGraph()
	run := &reflectionDelegateNode{id: "reflection_execute", agent: a, task: task}
	review := &reflectionReviewNode{id: "reflection_review", agent: a, task: task}
	decision := &reflectionDecisionNode{id: "reflection_decide", agent: a}
	done := framework.NewTerminalNode("reflection_done")
	for _, node := range []framework.Node{run, review, decision, done} {
		if err := graph.AddNode(node); err != nil {
			return nil, err
		}
	}
	if err := graph.SetStart(run.ID()); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(run.ID(), review.ID(), nil, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(review.ID(), decision.ID(), nil, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(decision.ID(), run.ID(), func(res *framework.Result, ctx *framework.Context) bool {
		revise, _ := ctx.Get("reflection.revise")
		return revise == true
	}, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(decision.ID(), done.ID(), func(res *framework.Result, ctx *framework.Context) bool {
		revise, _ := ctx.Get("reflection.revise")
		return revise != true
	}, false); err != nil {
		return nil, err
	}
	return graph, nil
}

type reflectionDelegateNode struct {
	id    string
	agent *ReflectionAgent
	task  *framework.Task
}

// ID returns the graph identifier for the delegate execution step.
func (n *reflectionDelegateNode) ID() string { return n.id }

// Type indicates this node executes system steps rather than tools.
func (n *reflectionDelegateNode) Type() framework.NodeType {
	return framework.NodeTypeSystem
}

// Execute runs the delegate agent while isolating state mutations until the
// child run succeeds.
func (n *reflectionDelegateNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("executing")
	child := state.Clone()
	result, err := n.agent.Delegate.Execute(ctx, n.task, child)
	if err != nil {
		return nil, err
	}
	state.Merge(child)
	state.Set("reflection.last_result", result)
	return result, nil
}

type reflectionReviewNode struct {
	id    string
	agent *ReflectionAgent
	task  *framework.Task
}

// ID returns the review node identifier.
func (n *reflectionReviewNode) ID() string { return n.id }

// Type marks the node as an observation step since it inspects output.
func (n *reflectionReviewNode) Type() framework.NodeType {
	return framework.NodeTypeObservation
}

// Execute asks the reviewer model to evaluate the last result and captures the
// structured feedback in the shared state.
func (n *reflectionReviewNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	resultVal, _ := state.Get("reflection.last_result")
	lastResult, _ := resultVal.(*framework.Result)
	prompt := fmt.Sprintf(`Review the following result for task "%s".
Consider correctness, completeness, quality, security, performance.
Respond JSON {"issues":[{"severity":"high|medium|low","description":"...","suggestion":"..."}],"approve":bool}
Result: %+v`, n.task.Instruction, lastResult)
	resp, err := n.agent.Reviewer.Generate(ctx, prompt, &framework.LLMOptions{
		Model:       n.agent.Config.Model,
		Temperature: 0.2,
		MaxTokens:   600,
	})
	if err != nil {
		return nil, err
	}
	review, err := parseReview(resp.Text)
	if err != nil {
		return nil, err
	}
	state.Set("reflection.review", review)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"review": review}}, nil
}

type reflectionDecisionNode struct {
	id    string
	agent *ReflectionAgent
}

// ID returns the decision node identifier.
func (n *reflectionDecisionNode) ID() string { return n.id }

// Type declares the node as a conditional branch in the graph.
func (n *reflectionDecisionNode) Type() framework.NodeType {
	return framework.NodeTypeConditional
}

// Execute inspects review feedback and decides if another delegate iteration
// should run.
func (n *reflectionDecisionNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	reviewVal, _ := state.Get("reflection.review")
	review, _ := reviewVal.(reviewPayload)
	iterVal, _ := state.Get("reflection.iteration")
	iter, _ := iterVal.(int)
	iter++
	state.Set("reflection.iteration", iter)
	revise := !review.Approve && iter < n.agent.maxIterations
	state.Set("reflection.revise", revise)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"revise": revise}}, nil
}

type reviewPayload struct {
	Issues []struct {
		Severity    string `json:"severity"`
		Description string `json:"description"`
		Suggestion  string `json:"suggestion"`
	} `json:"issues"`
	Approve bool `json:"approve"`
}

// parseReview decodes the reviewer JSON into a strongly typed payload.
func parseReview(raw string) (reviewPayload, error) {
	var payload reviewPayload
	if err := json.Unmarshal([]byte(ExtractJSON(raw)), &payload); err != nil {
		return payload, err
	}
	return payload, nil
}
