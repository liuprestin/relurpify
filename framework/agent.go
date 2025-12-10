package framework

import "context"

// Capability represents a high-level ability exposed by an agent.
type Capability string

const (
	CapabilityPlan        Capability = "plan"
	CapabilityCode        Capability = "code"
	CapabilityReview      Capability = "review"
	CapabilityExecute     Capability = "execute"
	CapabilityExplain     Capability = "explain"
	CapabilityRefactor    Capability = "refactor"
	CapabilityHumanInLoop Capability = "human_in_loop"
)

// TaskType describes the type of work an agent should perform.
type TaskType string

const (
	TaskTypeCodeModification TaskType = "code_modification"
	TaskTypeCodeGeneration   TaskType = "code_generation"
	TaskTypePlanning         TaskType = "planning"
	TaskTypeReview           TaskType = "review"
	TaskTypeAnalysis         TaskType = "analysis"
)

// Task encapsulates the information sent to an agent. The Context provides
// shared runtime state, while Task carries the immutable instruction plus any
// metadata gathered from the editor/LSP layer (file paths, cursor ranges, etc).
type Task struct {
	ID          string
	Type        TaskType
	Instruction string
	Context     map[string]any
	Metadata    map[string]string
}

// Plan encapsulates planning information. Planner-like agents persist their
// reasoning by filling this struct and storing it inside Context so subsequent
// nodes can execute or verify each step.
type Plan struct {
	Goal         string
	Steps        []PlanStep
	Dependencies map[int][]int
}

// PlanStep describes a single actionable step. The Tool/Params fields point to
// entries in the ToolRegistry so the planner can decide between filesystem,
// git, execution, and LSP-powered capabilities at runtime.
type PlanStep struct {
	ID           int
	Description  string
	Tool         string
	Params       map[string]interface{}
	Expected     string
	Verification string
	Status       string
}

// Config contains per-agent configuration knobs supplied by the server or CLI.
// Agents are encouraged to store the pointer passed to Initialize so they can
// reference shared defaults (model name, iteration caps, etc.) inside their
// graph-building logic.
type Config struct {
	Name               string
	DefaultAgent       string
	MaxIterations      int
	Model              string
	OllamaEndpoint     string
	LanguageServers    map[string]map[string]string
	OllamaToolCalling  bool
	DebugLLM           bool
	DebugAgent         bool
	AgentSpec          *AgentRuntimeSpec
	Telemetry          Telemetry
}

// Result captures the result of a graph or agent execution. Creating a shared
// struct keeps telemetry, persistence, and tool adapters consistent because
// they can always expect a NodeID/Success/Data triple.
type Result struct {
	NodeID  string
	Success bool
	Data    map[string]any
	Error   error
}

// Agent defines the contract for all specialized agents. BuildGraph is exposed
// so orchestrators can inspect the workflow ahead of time (for visualization or
// validation) before calling Execute.
type Agent interface {
	Initialize(config *Config) error
	Execute(ctx context.Context, task *Task, state *Context) (*Result, error)
	Capabilities() []Capability
	BuildGraph(task *Task) (*Graph, error)
}
