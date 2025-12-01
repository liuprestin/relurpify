package agents

import (
	"context"
	"fmt"

	"github.com/lexcodex/relurpify/framework"
)

// ManualCodingAgent is a lightweight agent used in Architect/Ask/Security
// contexts when read-only inspection is required. It emits guidance without
// mutating the workspace.
type ManualCodingAgent struct {
	Mode   ModeProfile
	Tools  *framework.ToolRegistry
	Model  framework.LanguageModel
	Config *framework.Config
}

// Initialize satisfies the Agent interface.
func (a *ManualCodingAgent) Initialize(cfg *framework.Config) error {
	a.Config = cfg
	if a.Tools == nil {
		a.Tools = framework.NewToolRegistry()
	}
	return nil
}

// Capabilities returns the current mode capabilities.
func (a *ManualCodingAgent) Capabilities() []framework.Capability {
	return a.Mode.Capabilities
}

// BuildGraph returns a trivial graph so the CLI can visualize execution.
func (a *ManualCodingAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	graph := framework.NewGraph()
	done := framework.NewTerminalNode("manual_done")
	_ = graph.AddNode(done)
	_ = graph.SetStart(done.ID())
	return graph, nil
}

// Execute just records the instruction and returns metadata.
func (a *ManualCodingAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	state.Set("manual.mode", a.Mode.Name)
	state.AddInteraction("system", fmt.Sprintf("Operating in %s mode. Instruction: %s", a.Mode.Title, task.Instruction), nil)
	return &framework.Result{
		NodeID:  "manual_done",
		Success: true,
		Data: map[string]any{
			"mode":        a.Mode.Name,
			"restrictions": a.Mode.Restrictions,
			"instruction":  task.Instruction,
		},
	}, nil
}
