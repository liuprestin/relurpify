package coder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lexcodex/relurpify/agents/internal/parse"
	plannerpkg "github.com/lexcodex/relurpify/agents/planner"
	"github.com/lexcodex/relurpify/framework"
)

// CoderState tracks the aggregated execution state for the expert coder agent.
// It mirrors the TypedDict schema requested by the user so other packages can
// introspect or persist the same information without bespoke conversions.
type CoderState struct {
	Messages      []framework.Message    `json:"messages"`
	CodeContext   map[string]interface{} `json:"code_context"`
	Task          string                 `json:"task"`
	Plan          []framework.PlanStep   `json:"plan"`
	Errors        []string               `json:"errors"`
	FilesModified []string               `json:"files_modified"`
	NextAction    string                 `json:"next_action"`
}

// ExpertCoderAgent orchestrates planning, coding, executing, debugging, and
// reviewing loops. It routes work to specialist agents whenever the workspace
// or instruction indicates that deeper expertise (framework, performance,
// security, domain-specific) is required.
type ExpertCoderAgent struct {
	Model  framework.LanguageModel
	Tools  *framework.ToolRegistry
	Memory framework.MemoryStore
	Config *framework.Config

	planner        *plannerpkg.PlannerAgent
	general        framework.Agent
	delegates      []delegateRoute
	delegateLookup map[string]framework.Agent
	maxIterations  int
}

// delegateRoute stores metadata about a delegate agent.
type delegateRoute struct {
	Name        string
	Description string
	Agent       framework.Agent
	Match       func(*framework.Task, *framework.Context) bool
	Priority    int
}

// Initialize wires internal planner/coder delegates and specialist adapters.
func (a *ExpertCoderAgent) Initialize(cfg *framework.Config) error {
	a.Config = cfg
	if a.Tools == nil {
		a.Tools = framework.NewToolRegistry()
	}
	if cfg != nil && cfg.MaxIterations > 0 {
		a.maxIterations = cfg.MaxIterations
	} else {
		a.maxIterations = 4
	}
	if a.Model == nil {
		return fmt.Errorf("expert coder agent missing model")
	}
	a.delegateLookup = make(map[string]framework.Agent)
	a.planner = &plannerpkg.PlannerAgent{Model: a.Model, Tools: a.Tools, Memory: a.Memory}
	if err := a.planner.Initialize(cfg); err != nil {
		return err
	}
	baseCoder := &CodingAgent{Model: a.Model, Tools: a.Tools, Memory: a.Memory}
	if err := baseCoder.Initialize(cfg); err != nil {
		return err
	}
	a.general = baseCoder
	a.delegates = []delegateRoute{
		{
			Name:        "general",
			Description: "Generalist coder capable of planning, editing, and verifying code",
			Agent:       baseCoder,
			Match: func(task *framework.Task, state *framework.Context) bool {
				return true
			},
			Priority: 0,
		},
	}
	a.delegateLookup["general"] = baseCoder

	specialists := []delegateRoute{
		{
			Name:        "framework",
			Description: "Specialist focused on framework-specific conventions (React hooks, SQLAlchemy relationships, etc.)",
			Agent: &instructionAdapter{
				prefix: "You are a framework specialist. Emphasize idiomatic usage of the referenced frameworks, ensure lifecycle hooks, providers, and ORM relationships stay consistent.",
				agent:  &CodingAgent{Model: a.Model, Tools: a.Tools, Memory: a.Memory},
			},
			Match:    keywordMatcher("react", "next.js", "vue", "svelte", "sqlalchemy", "django", "rails", "spring", "flutter", "kotlin", "android", "ios", "swiftui"),
			Priority: 2,
		},
		{
			Name:        "performance",
			Description: "Specialist that prioritizes performance, correctness under load, and memory safety.",
			Agent: &instructionAdapter{
				prefix: "You are a performance engineer. Optimize for low latency, memory efficiency, and algorithmic clarity while keeping the code maintainable.",
				agent:  &CodingAgent{Model: a.Model, Tools: a.Tools, Memory: a.Memory},
			},
			Match:    keywordMatcher("performance", "latency", "optimize", "throughput", "profiling", "hot path", "slow query", "memory leak"),
			Priority: 2,
		},
		{
			Name:        "security",
			Description: "Specialist that focuses on authentication, authorization, encryption, and secure coding patterns.",
			Agent: &instructionAdapter{
				prefix: "You are a security reviewer. Prioritize secure defaults, safe data handling, and defense-in-depth for auth flows.",
				agent:  &CodingAgent{Model: a.Model, Tools: a.Tools, Memory: a.Memory},
			},
			Match:    keywordMatcher("auth", "security", "jwt", "oauth", "encryption", "sql injection", "xss", "csrf", "rbac", "secret"),
			Priority: 2,
		},
		{
			Name:        "domain",
			Description: "Domain expert consulted when metadata or instructions reference regulated industries or bespoke business logic.",
			Agent: &instructionAdapter{
				prefix: "You are a domain expert. Respect industry regulations, existing invariants, and business terminology before editing code.",
				agent:  &CodingAgent{Model: a.Model, Tools: a.Tools, Memory: a.Memory},
			},
			Match: func(task *framework.Task, state *framework.Context) bool {
				if task == nil {
					return false
				}
				if domain, ok := task.Metadata["domain"]; ok && domain != "" {
					return true
				}
				instruction := strings.ToLower(task.Instruction)
				keywords := []string{"healthcare", "fintech", "bank", "payment", "insurance", "automotive", "embedded"}
				for _, kw := range keywords {
					if strings.Contains(instruction, kw) {
						return true
					}
				}
				return false
			},
			Priority: 1,
		},
	}

	for _, specialist := range specialists {
		if specialist.Agent == nil {
			continue
		}
		if err := specialist.Agent.Initialize(cfg); err != nil {
			return err
		}
		a.delegates = append(a.delegates, specialist)
		a.delegateLookup[specialist.Name] = specialist.Agent
	}

	return nil
}

// Capabilities advertises agent abilities.
func (a *ExpertCoderAgent) Capabilities() []framework.Capability {
	return []framework.Capability{
		framework.CapabilityPlan,
		framework.CapabilityCode,
		framework.CapabilityReview,
		framework.CapabilityExecute,
		framework.CapabilityExplain,
		framework.CapabilityRefactor,
		framework.CapabilityHumanInLoop,
	}
}

// BuildGraph constructs nodes for init -> plan -> router -> coder -> executor -> debugger -> reviewer -> decision.
func (a *ExpertCoderAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	if a.Model == nil {
		return nil, fmt.Errorf("expert coder agent missing model")
	}
	graph := framework.NewGraph()
	nodes := []framework.Node{
		&expertInitNode{id: "expert_init", agent: a, task: task},
		&expertPlanNode{id: "expert_plan", agent: a, task: task},
		&expertRouterNode{id: "expert_router", agent: a, task: task},
		&expertDelegateNode{id: "expert_delegate", agent: a, task: task},
		&expertExecutorNode{id: "expert_executor", agent: a},
		&expertDebuggerNode{id: "expert_debugger", agent: a, task: task},
		&expertReviewNode{id: "expert_review", agent: a, task: task},
		&expertDecisionNode{id: "expert_decide", agent: a},
		&expertHumanNode{id: "expert_human"},
		framework.NewTerminalNode("expert_done"),
	}
	for _, node := range nodes {
		if err := graph.AddNode(node); err != nil {
			return nil, err
		}
	}
	if err := graph.SetStart("expert_init"); err != nil {
		return nil, err
	}
	edges := []struct{ from, to string }{
		{"expert_init", "expert_plan"},
		{"expert_plan", "expert_router"},
		{"expert_router", "expert_delegate"},
		{"expert_delegate", "expert_executor"},
		{"expert_executor", "expert_debugger"},
		{"expert_debugger", "expert_review"},
		{"expert_review", "expert_decide"},
		{"expert_human", "expert_done"},
	}
	for _, edge := range edges {
		if err := graph.AddEdge(edge.from, edge.to, nil, false); err != nil {
			return nil, err
		}
	}
	if err := graph.AddEdge("expert_decide", "expert_plan", func(res *framework.Result, ctx *framework.Context) bool {
		val, _ := ctx.Get("expert.replan")
		replan, _ := val.(bool)
		return replan
	}, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge("expert_decide", "expert_router", func(res *framework.Result, ctx *framework.Context) bool {
		val, _ := ctx.Get("expert.continue")
		cont, _ := val.(bool)
		return cont
	}, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge("expert_decide", "expert_human", func(res *framework.Result, ctx *framework.Context) bool {
		val, _ := ctx.Get("expert.need_human")
		wait, _ := val.(bool)
		return wait
	}, false); err != nil {
		return nil, err
	}
	if err := graph.AddEdge("expert_decide", "expert_done", func(res *framework.Result, ctx *framework.Context) bool {
		contVal, _ := ctx.Get("expert.continue")
		cont, _ := contVal.(bool)
		replanVal, _ := ctx.Get("expert.replan")
		replan, _ := replanVal.(bool)
		waitVal, _ := ctx.Get("expert.need_human")
		wait, _ := waitVal.(bool)
		return !cont && !replan && !wait
	}, false); err != nil {
		return nil, err
	}
	return graph, nil
}

// Execute runs the assembled graph.
func (a *ExpertCoderAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	graph, err := a.BuildGraph(task)
	if err != nil {
		return nil, err
	}
	return graph.Execute(ctx, state)
}

// expertInitNode ensures required tooling is present and seeds coder state.
type expertInitNode struct {
	id    string
	agent *ExpertCoderAgent
	task  *framework.Task
}

func (n *expertInitNode) ID() string               { return n.id }
func (n *expertInitNode) Type() framework.NodeType { return framework.NodeTypeSystem }

func (n *expertInitNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("initializing")
	critical := []string{"file_read", "file_write", "file_search", "exec_run_code", "exec_run_tests", "exec_run_linter", "exec_run_build", "git_diff"}
	advisory := []string{"git_status", "git_commit", "search_grep", "search_semantic", "file_list"}
	missingCritical := missingTools(n.agent.Tools, critical)
	missingAdvisory := missingTools(n.agent.Tools, advisory)
	if len(missingCritical) > 0 {
		state.Set("expert.tooling.missing", missingCritical)
		return nil, fmt.Errorf("expert tools missing critical capabilities: %s", strings.Join(missingCritical, ", "))
	}
	if len(missingAdvisory) > 0 {
		state.Set("expert.tooling.warnings", missingAdvisory)
	}
	coder := loadCoderState(state, n.task)
	coder.Task = n.task.Instruction
	coder.CodeContext["workspace"] = n.task.Context
	coder.Messages = append(coder.Messages, framework.Message{Role: "system", Content: "Initialized expert coder agent"})
	saveCoderState(state, coder)
	state.Set("expert.iteration", 0)
	state.Set("expert.errors", []string{})
	state.Set("expert.replan", false)
	state.Set("expert.continue", true)
	state.Set("expert.need_human", false)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"missing_optional": missingAdvisory}}, nil
}

// expertPlanNode produces a structured plan via the planner agent.
type expertPlanNode struct {
	id    string
	agent *ExpertCoderAgent
	task  *framework.Task
}

func (n *expertPlanNode) ID() string               { return n.id }
func (n *expertPlanNode) Type() framework.NodeType { return framework.NodeTypeSystem }

func (n *expertPlanNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("planning")
	clone := state.Clone()
	result, err := n.agent.planner.Execute(ctx, n.task, clone)
	if err != nil {
		return nil, err
	}
	state.Merge(clone)
	planVal, _ := state.Get("planner.plan")
	plan, _ := planVal.(framework.Plan)
	coder := loadCoderState(state, n.task)
	coder.Plan = plan.Steps
	coder.Messages = append(coder.Messages, framework.Message{Role: "assistant", Content: fmt.Sprintf("Generated plan with %d steps", len(plan.Steps))})
	saveCoderState(state, coder)
	return result, nil
}

// expertRouterNode picks the next delegate based on planner output and context.
type expertRouterNode struct {
	id    string
	agent *ExpertCoderAgent
	task  *framework.Task
}

func (n *expertRouterNode) ID() string               { return n.id }
func (n *expertRouterNode) Type() framework.NodeType { return framework.NodeTypeConditional }

func (n *expertRouterNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("routing")
	route := n.agent.selectDelegate(n.task, state)
	coder := loadCoderState(state, n.task)
	coder.NextAction = route.Name
	coder.Messages = append(coder.Messages, framework.Message{Role: "system", Content: fmt.Sprintf("Routing work to %s specialist", route.Name)})
	saveCoderState(state, coder)
	state.Set("expert.delegate_name", route.Name)
	state.Set("expert.delegate_description", route.Description)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"delegate": route.Name}}, nil
}

// expertDelegateNode executes the selected agent and records snapshots for rollback.
type expertDelegateNode struct {
	id    string
	agent *ExpertCoderAgent
	task  *framework.Task
}

func (n *expertDelegateNode) ID() string               { return n.id }
func (n *expertDelegateNode) Type() framework.NodeType { return framework.NodeTypeSystem }

func (n *expertDelegateNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("coding")
	nameVal, _ := state.Get("expert.delegate_name")
	delegateName, _ := nameVal.(string)
	delegate := n.agent.delegateLookup[delegateName]
	if delegate == nil {
		delegate = n.agent.general
	}
	snapshot := state.Snapshot()
	state.Set("expert.last_snapshot", snapshot)
	child := state.Clone()
	result, err := delegate.Execute(ctx, n.task, child)
	if err != nil {
		coder := loadCoderState(state, n.task)
		coder.Errors = append(coder.Errors, err.Error())
		coder.Messages = append(coder.Messages, framework.Message{Role: "assistant", Content: fmt.Sprintf("Delegate %s failed: %s", delegateName, err.Error())})
		saveCoderState(state, coder)
		state.Set("expert.errors", coder.Errors)
		return nil, err
	}
	state.Merge(child)
	coder := loadCoderState(state, n.task)
	files := extractFileList(result.Data["files"])
	if len(files) > 0 {
		coder.FilesModified = uniqueStrings(append(coder.FilesModified, files...))
	}
	coder.Messages = append(coder.Messages, framework.Message{Role: "assistant", Content: fmt.Sprintf("Delegate %s completed", delegateName)})
	saveCoderState(state, coder)
	state.Set("expert.last_result", result)
	state.Set("expert.errors", coder.Errors)
	return result, nil
}

// expertExecutorNode enforces syntax/lint/test gates.
type expertExecutorNode struct {
	id    string
	agent *ExpertCoderAgent
}

func (n *expertExecutorNode) ID() string               { return n.id }
func (n *expertExecutorNode) Type() framework.NodeType { return framework.NodeTypeTool }

func (n *expertExecutorNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("executing")
	checks := map[string]interface{}{}
	var errors []string
	for _, toolName := range []string{"exec_run_build", "exec_run_linter", "exec_run_tests"} {
		tool, ok := n.agent.Tools.Get(toolName)
		if !ok {
			checks[toolName] = "skipped"
			continue
		}
		res, err := tool.Execute(ctx, state, map[string]interface{}{})
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s failed: %s", toolName, err.Error()))
			checks[toolName] = err.Error()
			continue
		}
		checks[toolName] = res.Data
		if !res.Success {
			checks[toolName] = res.Data
			if res.Error != "" {
				errors = append(errors, fmt.Sprintf("%s reported error: %s", toolName, res.Error))
			}
		}
	}
	coder := loadCoderState(state, nil)
	if len(errors) > 0 {
		coder.Errors = uniqueStrings(append(coder.Errors, errors...))
	}
	saveCoderState(state, coder)
	state.Set("expert.executor", checks)
	state.Set("expert.errors", coder.Errors)
	success := len(errors) == 0
	return &framework.Result{NodeID: n.id, Success: success, Data: checks}, nil
}

// expertDebuggerNode analyzes failures, optionally requesting replans or rollbacks.
type expertDebuggerNode struct {
	id    string
	agent *ExpertCoderAgent
	task  *framework.Task
}

func (n *expertDebuggerNode) ID() string               { return n.id }
func (n *expertDebuggerNode) Type() framework.NodeType { return framework.NodeTypeObservation }

func (n *expertDebuggerNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("debugging")
	coder := loadCoderState(state, n.task)
	if len(coder.Errors) == 0 {
		state.Set("expert.replan", false)
		return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"errors": []string{}}}, nil
	}
	planSummary := summarizePlan(coder.Plan)
	prompt := fmt.Sprintf(`You are a debugging controller. Task: %s
Plan: %s
Errors: %s
Respond JSON {"replan":bool,"next_action":"code|debug|review","suggestions":[],"rollback":bool}`, n.task.Instruction, planSummary, strings.Join(coder.Errors, "; "))
	resp, err := n.agent.Model.Generate(ctx, prompt, &framework.LLMOptions{
		Model:       n.agent.modelName(),
		Temperature: 0.1,
		MaxTokens:   400,
	})
	if err != nil {
		return nil, err
	}
	advice, err := parseDebuggerAdvice(resp.Text)
	if err != nil {
		return nil, err
	}
	if advice.Rollback {
		snapVal, _ := state.Get("expert.last_snapshot")
		if snap, ok := snapVal.(*framework.ContextSnapshot); ok && snap != nil {
			_ = state.Restore(snap)
			coder = loadCoderState(state, n.task)
		}
	}
	coder.Messages = append(coder.Messages, framework.Message{Role: "assistant", Content: strings.Join(advice.Suggestions, "\n")})
	coder.NextAction = advice.NextAction
	saveCoderState(state, coder)
	state.Set("expert.replan", advice.Replan)
	state.Set("expert.errors", coder.Errors)
	state.Set("expert.debug_suggestions", advice.Suggestions)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"advice": advice}}, nil
}

// expertReviewNode enforces quality gates via the shared model.
type expertReviewNode struct {
	id    string
	agent *ExpertCoderAgent
	task  *framework.Task
}

func (n *expertReviewNode) ID() string               { return n.id }
func (n *expertReviewNode) Type() framework.NodeType { return framework.NodeTypeObservation }

func (n *expertReviewNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("reviewing")
	coder := loadCoderState(state, n.task)
	executorVal, _ := state.Get("expert.executor")
	prompt := fmt.Sprintf(`Review the latest changes for task "%s".
Plan summary: %s
Files touched: %v
Execution gates: %+v
Produce JSON {"issues":[{"severity":"high|medium|low","description":"...","suggestion":"..."}],"approve":bool,"require_human":bool,"notes":""}`, n.task.Instruction, summarizePlan(coder.Plan), coder.FilesModified, executorVal)
	resp, err := n.agent.Model.Generate(ctx, prompt, &framework.LLMOptions{
		Model:       n.agent.modelName(),
		Temperature: 0.2,
		MaxTokens:   500,
	})
	if err != nil {
		return nil, err
	}
	review, err := parseExpertReview(resp.Text)
	if err != nil {
		return nil, err
	}
	state.Set("expert.review", review)
	coder.Messages = append(coder.Messages, framework.Message{Role: "assistant", Content: fmt.Sprintf("Review approve=%v", review.Approve)})
	saveCoderState(state, coder)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"review": review}}, nil
}

// expertDecisionNode decides whether to continue looping, replan, or hand off to a human.
type expertDecisionNode struct {
	id    string
	agent *ExpertCoderAgent
}

func (n *expertDecisionNode) ID() string               { return n.id }
func (n *expertDecisionNode) Type() framework.NodeType { return framework.NodeTypeConditional }

func (n *expertDecisionNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	iterVal, _ := state.Get("expert.iteration")
	iter, _ := iterVal.(int)
	iter++
	state.Set("expert.iteration", iter)
	coder := loadCoderState(state, nil)
	reviewVal, _ := state.Get("expert.review")
	review, _ := reviewVal.(expertReviewPayload)
	replanVal, _ := state.Get("expert.replan")
	replan, _ := replanVal.(bool)
	pendingErrors := len(coder.Errors) > 0
	approve := review.Approve
	continueLoop := (pendingErrors || !approve || replan) && iter < n.agent.maxIterations && !review.RequireHuman
	needHuman := review.RequireHuman || (iter >= n.agent.maxIterations && (pendingErrors || !approve))
	state.Set("expert.continue", continueLoop)
	state.Set("expert.need_human", needHuman)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{
		"iteration":     iter,
		"continue":      continueLoop,
		"need_human":    needHuman,
		"pendingErrors": pendingErrors,
	}}, nil
}

// expertHumanNode signals that a human should review or approve next actions.
type expertHumanNode struct {
	id string
}

func (n *expertHumanNode) ID() string               { return n.id }
func (n *expertHumanNode) Type() framework.NodeType { return framework.NodeTypeHuman }

func (n *expertHumanNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("human_review")
	state.Set("expert.awaiting_human", true)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"message": "Awaiting human input"}}, nil
}

// Helper functions ----------------------------------------------------------

func (a *ExpertCoderAgent) selectDelegate(task *framework.Task, state *framework.Context) delegateRoute {
	for _, route := range a.delegates {
		if route.Name == "general" {
			continue
		}
		if route.Match != nil && route.Match(task, state) {
			return route
		}
	}
	return a.delegates[0]
}

func (a *ExpertCoderAgent) modelName() string {
	if a != nil && a.Config != nil && a.Config.Model != "" {
		return a.Config.Model
	}
	return ""
}

func loadCoderState(state *framework.Context, task *framework.Task) CoderState {
	if state == nil {
		return CoderState{Messages: []framework.Message{}, CodeContext: map[string]interface{}{}, Plan: []framework.PlanStep{}}
	}
	val, ok := state.Get("expert.state")
	if ok {
		if cs, ok := val.(CoderState); ok {
			if cs.CodeContext == nil {
				cs.CodeContext = map[string]interface{}{}
			}
			return cs
		}
	}
	cs := CoderState{
		Messages:      []framework.Message{},
		CodeContext:   map[string]interface{}{},
		Plan:          []framework.PlanStep{},
		Errors:        []string{},
		FilesModified: []string{},
	}
	if task != nil {
		cs.Task = task.Instruction
	}
	return cs
}

func saveCoderState(state *framework.Context, coder CoderState) {
	if state != nil {
		state.Set("expert.state", coder)
	}
}

func missingTools(registry *framework.ToolRegistry, names []string) []string {
	var missing []string
	for _, name := range names {
		if _, ok := registry.Get(name); !ok {
			missing = append(missing, name)
		}
	}
	return missing
}

func keywordMatcher(keywords ...string) func(*framework.Task, *framework.Context) bool {
	lowered := make([]string, len(keywords))
	for i, kw := range keywords {
		lowered[i] = strings.ToLower(kw)
	}
	return func(task *framework.Task, state *framework.Context) bool {
		if task != nil {
			text := strings.ToLower(task.Instruction)
			for _, kw := range lowered {
				if strings.Contains(text, kw) {
					return true
				}
			}
			for _, value := range task.Metadata {
				if strings.Contains(strings.ToLower(value), "security") {
					return true
				}
			}
		}
		if state != nil {
			planVal, _ := state.Get("planner.plan")
			if plan, ok := planVal.(framework.Plan); ok {
				for _, step := range plan.Steps {
					desc := strings.ToLower(step.Description)
					for _, kw := range lowered {
						if strings.Contains(desc, kw) {
							return true
						}
					}
				}
			}
		}
		return false
	}
}

func extractFileList(value interface{}) []string {
	var files []string
	switch v := value.(type) {
	case []string:
		files = append(files, v...)
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				files = append(files, s)
			}
		}
	case string:
		files = append(files, v)
	}
	return uniqueStrings(files)
}

func summarizePlan(steps []framework.PlanStep) string {
	if len(steps) == 0 {
		return "(no plan)"
	}
	var b strings.Builder
	for _, step := range steps {
		b.WriteString(fmt.Sprintf("%d:%s;", step.ID, step.Description))
	}
	return b.String()
}

type debuggerAdvice struct {
	Replan      bool     `json:"replan"`
	NextAction  string   `json:"next_action"`
	Suggestions []string `json:"suggestions"`
	Rollback    bool     `json:"rollback"`
}

func parseDebuggerAdvice(raw string) (debuggerAdvice, error) {
	var advice debuggerAdvice
	if err := json.Unmarshal([]byte(parse.ExtractJSON(raw)), &advice); err != nil {
		return advice, err
	}
	return advice, nil
}

type expertReviewPayload struct {
	Issues []struct {
		Severity    string `json:"severity"`
		Description string `json:"description"`
		Suggestion  string `json:"suggestion"`
	} `json:"issues"`
	Approve      bool   `json:"approve"`
	RequireHuman bool   `json:"require_human"`
	Notes        string `json:"notes"`
}

func parseExpertReview(raw string) (expertReviewPayload, error) {
	var payload expertReviewPayload
	if err := json.Unmarshal([]byte(parse.ExtractJSON(raw)), &payload); err != nil {
		return payload, err
	}
	return payload, nil
}

// instructionAdapter prepends role-specific instructions before delegating to
// another agent. It keeps the specialist composition light-weight while
// reusing the proven CodingAgent logic.
type instructionAdapter struct {
	prefix string
	agent  framework.Agent
}

func (a *instructionAdapter) Initialize(cfg *framework.Config) error {
	return a.agent.Initialize(cfg)
}

func (a *instructionAdapter) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	return a.agent.Execute(ctx, decorateTask(task, a.prefix), state)
}

func (a *instructionAdapter) Capabilities() []framework.Capability {
	return a.agent.Capabilities()
}

func (a *instructionAdapter) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	return a.agent.BuildGraph(decorateTask(task, a.prefix))
}

func decorateTask(task *framework.Task, prefix string) *framework.Task {
	if task == nil {
		return task
	}
	clone := *task
	clone.Instruction = fmt.Sprintf("%s\n\nOriginal task: %s", prefix, task.Instruction)
	if task.Context != nil {
		ctxCopy := make(map[string]any, len(task.Context))
		for k, v := range task.Context {
			ctxCopy[k] = v
		}
		clone.Context = ctxCopy
	}
	if task.Metadata != nil {
		metaCopy := make(map[string]string, len(task.Metadata))
		for k, v := range task.Metadata {
			metaCopy[k] = v
		}
		clone.Metadata = metaCopy
	}
	return &clone
}
