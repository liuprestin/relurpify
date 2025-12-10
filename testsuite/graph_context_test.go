package testsuite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lexcodex/relurpify/framework"
)

func TestGraphCheckpointRoundTripWithSharedContext(t *testing.T) {
	base := framework.NewContext()
	base.Set("task.id", "graph-integration")
	shared := framework.NewSharedContext(base, nil, &framework.SimpleSummarizer{})
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "main.go")
	content := strings.Repeat("func hi() {}\n", 20)
	if _, err := shared.AddFile(filePath, content, "go", framework.DetailFull); err != nil {
		t.Fatalf("AddFile failed: %v", err)
	}
	for i := 0; i < 12; i++ {
		shared.AddInteraction("user", fmt.Sprintf("message %d", i), nil)
	}

	memoryStore, err := framework.NewHybridMemory(t.TempDir())
	if err != nil {
		t.Fatalf("memory init failed: %v", err)
	}
	if err := memoryStore.Remember(context.Background(), "plan", map[string]interface{}{"status": "draft"}, framework.MemoryScopeProject); err != nil {
		t.Fatalf("remember failed: %v", err)
	}

	strategy := framework.NewSimpleCompressionStrategy()
	llm := &fakeLLM{text: "Summary: done\nKey Facts: []"}
	graph := framework.NewGraph()
	worker := &recordingNode{id: "worker", run: func(state *framework.Context) {
		state.Set("resume.history", len(state.History()))
	}}
	done := framework.NewTerminalNode("done")
	if err := graph.AddNode(worker); err != nil {
		t.Fatalf("add worker: %v", err)
	}
	if err := graph.AddNode(done); err != nil {
		t.Fatalf("add terminal: %v", err)
	}
	if err := graph.AddEdge(worker.ID(), done.ID(), nil, false); err != nil {
		t.Fatalf("edge worker->done: %v", err)
	}
	if err := graph.SetStart(worker.ID()); err != nil {
		t.Fatalf("set start: %v", err)
	}

	checkpoint, err := graph.CreateCompressedCheckpoint("graph-integration", worker.ID(), shared.Context, llm, strategy)
	if err != nil {
		t.Fatalf("CreateCompressedCheckpoint error: %v", err)
	}
	if checkpoint.CompressedContext == nil {
		t.Fatal("expected compressed context to be attached")
	}
	if len(checkpoint.Context.History()) > strategy.KeepRecent() {
		t.Fatalf("expected history trimmed to %d entries, got %d", strategy.KeepRecent(), len(checkpoint.Context.History()))
	}

	result, err := graph.ResumeFromCheckpoint(context.Background(), checkpoint)
	if err != nil {
		t.Fatalf("ResumeFromCheckpoint error: %v", err)
	}
	if result == nil || result.NodeID != "done" {
		t.Fatalf("resume result mismatch: %+v", result)
	}
	if value, _ := checkpoint.Context.Get("resume.history"); value != strategy.KeepRecent() {
		t.Fatalf("expected resume history %d, got %v", strategy.KeepRecent(), value)
	}

	record, ok, err := memoryStore.Recall(context.Background(), "plan", framework.MemoryScopeProject)
	if err != nil || !ok {
		t.Fatalf("expected memory recall to succeed, err=%v", err)
	}
	if record.Value["status"] != "draft" {
		t.Fatalf("unexpected memory payload: %#v", record.Value)
	}
}

func TestSharedContextBudgetCompressionFlow(t *testing.T) {
	ctx := framework.NewContext()
	budget := framework.NewContextBudget(256)
	budget.SetReservations(0, 0, 0)
	shared := framework.NewSharedContext(ctx, budget, &framework.SimpleSummarizer{})
	filePath := filepath.Join(t.TempDir(), "large.go")
	content := strings.Repeat("func big() {}\n", 200)
	fc, err := shared.AddFile(filePath, content, "go", framework.DetailFull)
	if err != nil {
		t.Fatalf("AddFile failed: %v", err)
	}
	for i := 0; i < 40; i++ {
		shared.AddInteraction("user", strings.Repeat("step "+fmt.Sprint(i)+" ", 40), nil)
	}
	budget.UpdateUsage(shared.Context, nil)
	if budget.CheckBudget() == framework.BudgetOK {
		t.Fatal("expected budget pressure after loading context")
	}

	shared.OnBudgetWarning(0.9)
	if fc.Level != framework.DetailSummary {
		t.Fatalf("expected file downgraded to summary, got %v", fc.Level)
	}
	strategy := framework.NewSimpleCompressionStrategy()
	llm := &fakeLLM{text: "Summary: trimmed\nKey Facts: []"}
	if err := shared.Context.CompressHistory(strategy.KeepRecent(), llm, strategy); err != nil {
		t.Fatalf("CompressHistory error: %v", err)
	}
	stats := shared.Context.GetCompressionStats()
	if stats.CompressionEvents == 0 || stats.CompressedChunks == 0 {
		t.Fatal("expected compression stats to reflect activity")
	}
}

func TestGraphParallelExecution(t *testing.T) {
	graph := framework.NewGraph()

	// Start Node
	start := &recordingNode{id: "start"}
	// Branch A: sets "a"=1
	branchA := &recordingNode{id: "branchA", run: func(state *framework.Context) {
		state.Set("val.a", 1)
	}}
	// Branch B: sets "b"=2
	branchB := &recordingNode{id: "branchB", run: func(state *framework.Context) {
		state.Set("val.b", 2)
	}}
	// Merge Node (implicitly happens when branches join)
	end := framework.NewTerminalNode("end")

	graph.AddNode(start)
	graph.AddNode(branchA)
	graph.AddNode(branchB)
	graph.AddNode(end)

	graph.SetStart("start")

	// Split: Start -> A (Parallel), Start -> B (Parallel)
	graph.AddEdge("start", "branchA", nil, true)
	graph.AddEdge("start", "branchB", nil, true)

	// Join: A -> End, B -> End
	// Note: The framework supports merging context from parallel branches.
	// When multiple branches converge to a node, that node is executed once for each incoming edge *conceptually*,
	// OR the framework waits.
	// Looking at graph.go: "Launch parallel branches... merging their updates... wg.Wait()".
	// The current implementation executes parallel edges and merges results back to the PARENT context.
	// It does NOT support a true "Join" node that waits for all predecessors.
	// Instead, the parallel branches are executed, and then the parent flow continues.
	// Wait, graph.go nextNodes() logic:
	// "Launch parallel branches... wg.Wait() ... if len(serialEdges) == 0 { return "", nil }"
	// This means parallel branches are "fork-join" at the edge level.
	// So if 'start' has parallel edges to A and B, it will run A and B to completion (assuming they are sub-chains or leaves).
	// If A and B are simple nodes that don't point anywhere, they finish, and then we continue?
	// Actually, if 'start' has serial edges too, it would take them.
	// But A and B are separate paths.
	//
	// Let's re-read graph.go:
	// for _, edge := range parallelEdges { go func() { ... executeBranch(edge.To, branchCtx) ... merge } }
	// The executeBranch runs the subgraph starting at edge.To.
	// So A and B must terminate or converge.
	// If A points to End, and B points to End.
	// Branch A execution: Run A -> Run End.
	// Branch B execution: Run B -> Run End.
	// Both will merge "End" modifications back.
	//
	// To verify state merge, we just need A and B to set variables.
	
	graph.AddEdge("branchA", "end", nil, false)
	graph.AddEdge("branchB", "end", nil, false)

	ctx := framework.NewContext()
	_, err := graph.Execute(context.Background(), ctx)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	valA, _ := ctx.Get("val.a")
	valB, _ := ctx.Get("val.b")

	if valA != 1 {
		t.Errorf("expected val.a=1, got %v", valA)
	}
	if valB != 2 {
		t.Errorf("expected val.b=2, got %v", valB)
	}
}

type recordingNode struct {
	id  string
	run func(*framework.Context)
}

func (n *recordingNode) ID() string               { return n.id }
func (n *recordingNode) Type() framework.NodeType { return framework.NodeTypeSystem }
func (n *recordingNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	if n.run != nil {
		n.run(state)
	}
	return &framework.Result{NodeID: n.id, Success: true}, nil
}

type fakeLLM struct {
	text string
}

func (f *fakeLLM) Generate(ctx context.Context, prompt string, options *framework.LLMOptions) (*framework.LLMResponse, error) {
	return &framework.LLMResponse{Text: f.text}, nil
}
func (f *fakeLLM) GenerateStream(ctx context.Context, prompt string, options *framework.LLMOptions) (<-chan string, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeLLM) Chat(ctx context.Context, messages []framework.Message, options *framework.LLMOptions) (*framework.LLMResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeLLM) ChatWithTools(ctx context.Context, messages []framework.Message, tools []framework.Tool, options *framework.LLMOptions) (*framework.LLMResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
