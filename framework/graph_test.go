package framework

import (
	"context"
	"errors"
	"testing"
)

type testNode struct {
	id   string
	kind NodeType
	run  func(context.Context, *Context) (*Result, error)
}

// ID returns the configured node identifier for testing dispatch logic.
func (n testNode) ID() string { return n.id }

// Type reports the explicit type or defaults to a tool node for the tests.
func (n testNode) Type() NodeType {
	if n.kind == "" {
		return NodeTypeTool
	}
	return n.kind
}

// Execute runs the injected function or returns a successful result when none
// is provided so the graph tests can focus on traversal mechanics.
func (n testNode) Execute(ctx context.Context, state *Context) (*Result, error) {
	if n.run != nil {
		return n.run(ctx, state)
	}
	return &Result{NodeID: n.id, Success: true, Data: map[string]interface{}{}}, nil
}

// TestGraphExecuteLinear ensures a simple three-node graph runs to completion
// and returns the terminal node when no branches exist.
func TestGraphExecuteLinear(t *testing.T) {
	graph := NewGraph()
	ctx := NewContext()
	ctx.Set("task.id", "test")

	n1 := testNode{id: "n1"}
	n2 := testNode{id: "n2"}
	n3 := testNode{id: "n3", kind: NodeTypeTerminal}

	if err := graph.AddNode(n1); err != nil {
		t.Fatalf("add node n1: %v", err)
	}
	if err := graph.AddNode(n2); err != nil {
		t.Fatalf("add node n2: %v", err)
	}
	if err := graph.AddNode(n3); err != nil {
		t.Fatalf("add node n3: %v", err)
	}
	if err := graph.SetStart("n1"); err != nil {
		t.Fatalf("set start: %v", err)
	}
	if err := graph.AddEdge("n1", "n2", nil, false); err != nil {
		t.Fatalf("edge n1->n2: %v", err)
	}
	if err := graph.AddEdge("n2", "n3", nil, false); err != nil {
		t.Fatalf("edge n2->n3: %v", err)
	}

	result, err := graph.Execute(context.Background(), ctx)
	if err != nil {
		t.Fatalf("execute graph: %v", err)
	}
	if result == nil || result.NodeID != "n3" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// TestGraphMissingNode confirms AddEdge refuses connections to unknown nodes,
// preventing runtime panics later in execution.
func TestGraphMissingNode(t *testing.T) {
	graph := NewGraph()
	n1 := testNode{id: "n1"}
	n2 := testNode{id: "n2"}
	if err := graph.AddNode(n1); err != nil {
		t.Fatalf("add node n1: %v", err)
	}
	if err := graph.AddNode(n2); err != nil {
		t.Fatalf("add node n2: %v", err)
	}
	if err := graph.SetStart("n1"); err != nil {
		t.Fatalf("set start: %v", err)
	}
	if err := graph.AddEdge("n1", "n2", nil, false); err != nil {
		t.Fatalf("edge n1->n2: %v", err)
	}
	if err := graph.AddEdge("n2", "missing", nil, false); err == nil {
		t.Fatalf("expected error for missing node")
	}
}

// TestGraphAllowsCycles verifies the engine can handle loops as long as node
// visit counts stay under the configured limit.
func TestGraphAllowsCycles(t *testing.T) {
	graph := NewGraph()
	ctx := NewContext()
	counter := testNode{
		id: "counter",
		run: func(ctx context.Context, state *Context) (*Result, error) {
			val, _ := state.Get("count")
			next := 1
			if v, ok := val.(int); ok {
				next = v + 1
			}
			state.Set("count", next)
			return &Result{
				NodeID:  "counter",
				Success: true,
				Data: map[string]interface{}{
					"count": next,
				},
			}, nil
		},
	}
	term := testNode{id: "done", kind: NodeTypeTerminal}
	if err := graph.AddNode(counter); err != nil {
		t.Fatalf("add counter: %v", err)
	}
	if err := graph.AddNode(term); err != nil {
		t.Fatalf("add term: %v", err)
	}
	if err := graph.SetStart("counter"); err != nil {
		t.Fatalf("set start: %v", err)
	}
	if err := graph.AddEdge("counter", "counter", func(result *Result, state *Context) bool {
		count, _ := result.Data["count"].(int)
		return count < 3
	}, false); err != nil {
		t.Fatalf("loop edge: %v", err)
	}
	if err := graph.AddEdge("counter", "done", func(result *Result, state *Context) bool {
		count, _ := result.Data["count"].(int)
		return count >= 3
	}, false); err != nil {
		t.Fatalf("exit edge: %v", err)
	}
	result, err := graph.Execute(context.Background(), ctx)
	if err != nil {
		t.Fatalf("execute graph: %v", err)
	}
	if result.NodeID != "done" {
		t.Fatalf("expected terminal node, got %s", result.NodeID)
	}
}

// TestGraphMaxNodeVisits ensures runaway cycles trigger a defensive error once
// a node exceeds its allowed visit count.
func TestGraphMaxNodeVisits(t *testing.T) {
	graph := NewGraph()
	graph.maxNodeVisits = 2
	loop := testNode{id: "loop"}
	if err := graph.AddNode(loop); err != nil {
		t.Fatalf("add loop: %v", err)
	}
	if err := graph.SetStart("loop"); err != nil {
		t.Fatalf("set start: %v", err)
	}
	if err := graph.AddEdge("loop", "loop", nil, false); err != nil {
		t.Fatalf("add loop edge: %v", err)
	}
	_, err := graph.Execute(context.Background(), NewContext())
	if err == nil {
		t.Fatalf("expected error due to exceeding max node visits")
	}
	if err.Error() != "potential cycle detected at node loop" {
		t.Fatalf("edge n2->n1: %v", err)
	}
}

// TestGraphNodeError validates errors returned by a node bubble up to the
// caller so orchestration layers can surface the failure.
func TestGraphNodeError(t *testing.T) {
	graph := NewGraph()
	errNode := testNode{
		id: "err",
		run: func(ctx context.Context, state *Context) (*Result, error) {
			return nil, errors.New("boom")
		},
	}
	if err := graph.AddNode(errNode); err != nil {
		t.Fatalf("add node: %v", err)
	}
	if err := graph.SetStart("err"); err != nil {
		t.Fatalf("set start: %v", err)
	}
	_, err := graph.Execute(context.Background(), NewContext())
	if err == nil {
		t.Fatalf("expected error from err node")
	}
}
