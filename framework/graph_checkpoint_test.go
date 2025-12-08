package framework

import (
	"context"
	"testing"
)

type simpleTestNode struct {
	id string
}

func (n *simpleTestNode) ID() string     { return n.id }
func (n *simpleTestNode) Type() NodeType { return NodeTypeSystem }
func (n *simpleTestNode) Execute(ctx context.Context, state *Context) (*Result, error) {
	state.Set(n.id+".visited", true)
	return &Result{NodeID: n.id, Success: true}, nil
}

func TestGraphCreateCheckpoint(t *testing.T) {
	graph := NewGraph()
	node := &simpleTestNode{id: "step"}
	end := NewTerminalNode("done")
	if err := graph.AddNode(node); err != nil {
		t.Fatalf("add node: %v", err)
	}
	if err := graph.AddNode(end); err != nil {
		t.Fatalf("add node: %v", err)
	}
	if err := graph.AddEdge(node.ID(), end.ID(), nil, false); err != nil {
		t.Fatalf("add edge: %v", err)
	}
	if err := graph.SetStart(node.ID()); err != nil {
		t.Fatalf("set start: %v", err)
	}
	state := NewContext()
	state.Set("task.id", "task-ckpt")
	graph.visitCounts["step"] = 1
	graph.executionPath = []string{"step"}
	checkpoint, err := graph.CreateCheckpoint("task-ckpt", end.ID(), state)
	if err != nil {
		t.Fatalf("CreateCheckpoint error: %v", err)
	}
	if checkpoint.Context == state {
		t.Fatal("expected checkpoint to clone the context")
	}
	if checkpoint.GraphHash == "" {
		t.Fatal("expected graph hash to be populated")
	}
}

func TestGraphResumeFromCheckpoint(t *testing.T) {
	graph := NewGraph()
	node := &simpleTestNode{id: "work"}
	done := NewTerminalNode("done")
	if err := graph.AddNode(node); err != nil {
		t.Fatalf("add node: %v", err)
	}
	if err := graph.AddNode(done); err != nil {
		t.Fatalf("add node: %v", err)
	}
	if err := graph.AddEdge(node.ID(), done.ID(), nil, false); err != nil {
		t.Fatalf("add edge: %v", err)
	}
	if err := graph.SetStart(node.ID()); err != nil {
		t.Fatalf("set start: %v", err)
	}
	state := NewContext()
	state.Set("task.id", "resume-task")
	checkpoint := &GraphCheckpoint{
		CheckpointID:  "ckpt",
		TaskID:        "resume-task",
		CurrentNodeID: node.ID(),
		VisitCounts:   map[string]int{},
		ExecutionPath: []string{},
		Context:       state,
		GraphHash:     graph.computeHash(),
		Metadata:      map[string]interface{}{},
	}
	result, err := graph.ResumeFromCheckpoint(context.Background(), checkpoint)
	if err != nil {
		t.Fatalf("ResumeFromCheckpoint error: %v", err)
	}
	if result == nil || result.NodeID != "done" {
		t.Fatalf("expected resume to finish at terminal node, got %+v", result)
	}
}

func TestGraphCreateCompressedCheckpoint(t *testing.T) {
	graph := NewGraph()
	graph.visitCounts["node"] = 1
	graph.executionPath = []string{"node"}
	ctx := NewContext()
	for i := 0; i < 6; i++ {
		ctx.AddInteraction("user", "history entry", nil)
	}
	comp := &stubCompressionStrategy{
		compressed: &CompressedContext{
			Summary:          "summary",
			KeyFacts:         []KeyFact{{Type: "decision", Content: "fact"}},
			OriginalTokens:   40,
			CompressedTokens: 10,
		},
		should: true,
	}
	llm := &stubLLM{text: "Summary: s\nKey Facts: []"}
	checkpoint, err := graph.CreateCompressedCheckpoint("task", "node", ctx, llm, comp)
	if err != nil {
		t.Fatalf("CreateCompressedCheckpoint error: %v", err)
	}
	if checkpoint.CompressedContext == nil {
		t.Fatal("expected compressed context to be attached")
	}
	if len(checkpoint.Context.history) > 5 {
		t.Fatal("expected checkpoint context history to be trimmed")
	}
}
