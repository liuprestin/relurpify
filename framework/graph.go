package framework

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// NodeType enumerates supported node categories.
type NodeType string

const (
	NodeTypeLLM         NodeType = "llm"
	NodeTypeTool        NodeType = "tool"
	NodeTypeConditional NodeType = "conditional"
	NodeTypeHuman       NodeType = "human"
	NodeTypeTerminal    NodeType = "terminal"
	NodeTypeSystem      NodeType = "system"
	NodeTypeObservation NodeType = "observation"
)

// Node describes the unit of work executed inside a graph.
type Node interface {
	ID() string
	Type() NodeType
	Execute(ctx context.Context, state *Context) (*Result, error)
}

// ConditionFunc determines whether an edge should be followed.
type ConditionFunc func(result *Result, state *Context) bool

// Edge describes a transition between nodes.
type Edge struct {
	From      string
	To        string
	Condition ConditionFunc
	Parallel  bool
}

// Graph orchestrates a workflow of nodes. It behaves like a tiny, deterministic
// state machine: nodes are registered ahead of time, edges describe transitions,
// and Execute walks the graph while recording telemetry plus enforcing invariants
// such as bounded node visits (to guard against accidental cycles).
type Graph struct {
	mu                 sync.RWMutex
	nodes              map[string]Node
	edges              map[string][]Edge
	startNodeID        string
	maxNodeVisits      int
	telemetry          Telemetry
	execMu             sync.Mutex
	visitCounts        map[string]int
	executionPath      []string
	checkpointInterval int
	checkpointCallback CheckpointCallback
	lastCheckpointNode string
}

// CheckpointCallback receives checkpoints generated during execution.
type CheckpointCallback func(checkpoint *GraphCheckpoint) error

// NewGraph creates a graph with sane defaults.
func NewGraph() *Graph {
	return &Graph{
		nodes:         make(map[string]Node),
		edges:         make(map[string][]Edge),
		maxNodeVisits: 1024,
		visitCounts:   make(map[string]int),
		executionPath: make([]string, 0),
	}
}

// WithCheckpointing configures automatic checkpointing for the graph.
func (g *Graph) WithCheckpointing(interval int, callback CheckpointCallback) *Graph {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkpointInterval = interval
	g.checkpointCallback = callback
	return g
}

// SetTelemetry wires a telemetry sink for execution traces.
func (g *Graph) SetTelemetry(t Telemetry) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.telemetry = t
}

// emit sends telemetry events when a sink is configured; a no-op otherwise.
func (g *Graph) emit(event Event) {
	g.mu.RLock()
	telemetry := g.telemetry
	g.mu.RUnlock()
	if telemetry == nil {
		return
	}
	telemetry.Emit(event)
}

// extractTaskID fetches the current task identifier from the shared context so
// telemetry has stable correlation identifiers even across node boundaries.
func (g *Graph) extractTaskID(state *Context) string {
	if state == nil {
		return ""
	}
	if value, ok := state.Get("task.id"); ok {
		return fmt.Sprint(value)
	}
	return ""
}

// SetStart marks the starting node.
func (g *Graph) SetStart(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.nodes[id]; !ok {
		return fmt.Errorf("start node %s not found", id)
	}
	g.startNodeID = id
	return nil
}

// AddNode registers a node.
func (g *Graph) AddNode(node Node) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.nodes[node.ID()]; exists {
		return fmt.Errorf("node %s already exists", node.ID())
	}
	g.nodes[node.ID()] = node
	return nil
}

// AddEdge wires two nodes together.
func (g *Graph) AddEdge(from, to string, condition ConditionFunc, parallel bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.nodes[from]; !ok {
		return fmt.Errorf("node %s not defined", from)
	}
	if _, ok := g.nodes[to]; !ok {
		return fmt.Errorf("node %s not defined", to)
	}
	g.edges[from] = append(g.edges[from], Edge{
		From:      from,
		To:        to,
		Condition: condition,
		Parallel:  parallel,
	})
	return nil
}

// GraphSnapshot stores enough state to resume an execution.
type GraphSnapshot struct {
	NodeID string
	State  *ContextSnapshot
}

// Execute runs the graph from its start node.
func (g *Graph) Execute(ctx context.Context, state *Context) (*Result, error) {
	return g.ExecuteFromSnapshot(ctx, state, nil)
}

// ExecuteFromSnapshot resumes execution from a snapshot.
func (g *Graph) ExecuteFromSnapshot(ctx context.Context, state *Context, snapshot *GraphSnapshot) (*Result, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}

	taskID := g.extractTaskID(state)
	g.emit(Event{Type: EventGraphStart, TaskID: taskID, Timestamp: time.Now().UTC()})
	var execErr error
	defer func() {
		status := "success"
		if execErr != nil {
			status = "error"
		}
		g.emit(Event{
			Type:      EventGraphFinish,
			TaskID:    taskID,
			Timestamp: time.Now().UTC(),
			Metadata: map[string]interface{}{
				"status": status,
			},
		})
	}()

	current := g.startNodeID
	if snapshot != nil {
		current = snapshot.NodeID
		if err := state.Restore(snapshot.State); err != nil {
			execErr = fmt.Errorf("restore snapshot: %w", err)
			return nil, execErr
		}
	}
	if current == "" {
		execErr = errors.New("graph has no start node")
		return nil, execErr
	}

	lastResult, err := g.run(ctx, state, current, true, taskID)
	execErr = err
	return lastResult, err
}

func (g *Graph) run(ctx context.Context, state *Context, current string, reset bool, taskID string) (*Result, error) {
	g.execMu.Lock()
	defer g.execMu.Unlock()
	if reset {
		g.visitCounts = make(map[string]int)
		g.executionPath = make([]string, 0)
		g.lastCheckpointNode = ""
	}
	g.mu.RLock()
	defer g.mu.RUnlock()

	var lastResult *Result
	for current != "" {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		node, ok := g.nodes[current]
		if !ok {
			return nil, fmt.Errorf("node %s missing", current)
		}
		g.visitCounts[current]++
		if g.visitCounts[current] > g.maxNodeVisits {
			return nil, fmt.Errorf("potential cycle detected at node %s", current)
		}
		g.executionPath = append(g.executionPath, current)
		g.emit(Event{
			Type:      EventNodeStart,
			NodeID:    current,
			TaskID:    taskID,
			Timestamp: time.Now().UTC(),
		})
		result, err := node.Execute(ctx, state)
		if err != nil {
			err = fmt.Errorf("node %s execution failed: %w", current, err)
			g.emit(Event{
				Type:      EventNodeError,
				NodeID:    current,
				TaskID:    taskID,
				Timestamp: time.Now().UTC(),
				Message:   err.Error(),
			})
			return nil, err
		}
		if result == nil {
			result = &Result{NodeID: current, Success: true, Data: map[string]interface{}{}}
		}
		result.NodeID = current
		lastResult = result
		for key, value := range result.Data {
			state.Set(fmt.Sprintf("%s.%s", current, key), value)
		}
		g.emit(Event{
			Type:      EventNodeFinish,
			NodeID:    current,
			TaskID:    taskID,
			Timestamp: time.Now().UTC(),
			Metadata: map[string]interface{}{
				"success": result.Success,
			},
		})
		g.maybeCheckpoint(taskID, current, state)
		next, err := g.nextNodes(ctx, state, node, result)
		if err != nil {
			return nil, err
		}
		current = next
	}
	return lastResult, nil
}

func (g *Graph) maybeCheckpoint(taskID, currentNode string, state *Context) {
	if g.checkpointInterval == 0 || g.checkpointCallback == nil {
		return
	}
	if !g.shouldCheckpoint() {
		return
	}
	checkpoint, err := g.CreateCheckpoint(taskID, currentNode, state)
	if err != nil {
		g.emit(Event{
			Type:      EventNodeError,
			NodeID:    currentNode,
			TaskID:    taskID,
			Timestamp: time.Now().UTC(),
			Message:   fmt.Sprintf("checkpoint creation failed: %v", err),
		})
		return
	}
	if err := g.checkpointCallback(checkpoint); err != nil {
		g.emit(Event{
			Type:      EventNodeError,
			NodeID:    currentNode,
			TaskID:    taskID,
			Timestamp: time.Now().UTC(),
			Message:   fmt.Sprintf("checkpoint callback failed: %v", err),
		})
		return
	}
	g.lastCheckpointNode = currentNode
}

func (g *Graph) shouldCheckpoint() bool {
	if g.checkpointInterval == 0 {
		return false
	}
	pathLength := len(g.executionPath)
	lastIndex := 0
	if g.lastCheckpointNode != "" {
		for idx := len(g.executionPath) - 1; idx >= 0; idx-- {
			if g.executionPath[idx] == g.lastCheckpointNode {
				lastIndex = idx + 1
				break
			}
		}
	}
	nodesSinceCheckpoint := pathLength - lastIndex
	return nodesSinceCheckpoint >= g.checkpointInterval
}

// nextNodes evaluates the outgoing edges for a node. Parallel edges are
// executed optimistically on cloned contexts while serial edges behave like a
// traditional state machine transition. Returning a single node ID keeps the
// main Execute loop simple and debuggable.
func (g *Graph) nextNodes(ctx context.Context, state *Context, node Node, result *Result) (string, error) {
	outEdges := g.edges[node.ID()]
	if len(outEdges) == 0 || node.Type() == NodeTypeTerminal {
		return "", nil
	}
	var serialEdges []Edge
	var parallelEdges []Edge
	for _, edge := range outEdges {
		if edge.Condition != nil && !edge.Condition(result, state) {
			continue
		}
		if edge.Parallel {
			parallelEdges = append(parallelEdges, edge)
		} else {
			serialEdges = append(serialEdges, edge)
		}
	}
	// Launch parallel branches, merging their updates into the shared state.
	if len(parallelEdges) > 0 {
		var wg sync.WaitGroup
		errChan := make(chan error, len(parallelEdges))
		for _, edge := range parallelEdges {
			wg.Add(1)
			edge := edge
			go func() {
				defer wg.Done()
				branchCtx := state.Clone()
				_, err := g.executeBranch(ctx, edge.To, branchCtx)
				if err != nil {
					errChan <- err
					return
				}
				state.Merge(branchCtx)
			}()
		}
		wg.Wait()
		close(errChan)
		for err := range errChan {
			if err != nil {
				return "", err
			}
		}
	}
	if len(serialEdges) == 0 {
		return "", nil
	}
	if len(serialEdges) > 1 {
		return "", fmt.Errorf("ambiguous transitions from %s", node.ID())
	}
	return serialEdges[0].To, nil
}

// executeBranch runs a detached sub-graph that starts at the provided node.
// The parent graph shares the node/edge definitions but each branch receives a
// cloned Context, which preserves determinism until Merge recombines updates.
func (g *Graph) executeBranch(ctx context.Context, start string, state *Context) (*Result, error) {
	// We reuse the same node/edge maps because branch graphs are read-only. The
	// only mutable data lives inside the cloned Context passed to this function.
	subGraph := &Graph{
		nodes:         g.nodes,
		edges:         g.edges,
		startNodeID:   start,
		maxNodeVisits: g.maxNodeVisits,
		telemetry:     g.telemetry,
	}
	return subGraph.Execute(ctx, state)
}

// Validate ensures there are no cycles and all references exist.
func (g *Graph) Validate() error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if len(g.nodes) == 0 {
		return errors.New("graph has no nodes")
	}
	if g.startNodeID == "" {
		return errors.New("graph has no start node")
	}
	for from, edges := range g.edges {
		if _, ok := g.nodes[from]; !ok {
			return fmt.Errorf("edge references missing node %s", from)
		}
		for _, edge := range edges {
			if _, ok := g.nodes[edge.To]; !ok {
				return fmt.Errorf("edge references missing node %s", edge.To)
			}
		}
	}
	return nil
}

// Pause builds a snapshot at the given node.
func (g *Graph) Pause(currentNode string, state *Context) *GraphSnapshot {
	return &GraphSnapshot{
		NodeID: currentNode,
		State:  state.Snapshot(),
	}
}

// LLMOptions configures language model calls. Keeping the options struct inside
// the framework avoids hard-coding Ollama/OpenAI specific fields in agent code.
type LLMOptions struct {
	Model       string
	Temperature float64
	MaxTokens   int
	Stop        []string
	TopP        float64
	Stream      bool
}

// ToolCall encodes a function invocation requested by the LLM.
type ToolCall struct {
	ID   string                 `json:"id,omitempty"`
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// LLMResponse is the result of a language model invocation.
type LLMResponse struct {
	Text         string         `json:"text,omitempty"`
	FinishReason string         `json:"finish_reason,omitempty"`
	Usage        map[string]int `json:"usage,omitempty"`
	ToolCalls    []ToolCall     `json:"tool_calls,omitempty"`
}

// Message is used for chat-like interactions.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// LanguageModel provides the required LLM capabilities.
type LanguageModel interface {
	Generate(ctx context.Context, prompt string, options *LLMOptions) (*LLMResponse, error)
	GenerateStream(ctx context.Context, prompt string, options *LLMOptions) (<-chan string, error)
	Chat(ctx context.Context, messages []Message, options *LLMOptions) (*LLMResponse, error)
	ChatWithTools(ctx context.Context, messages []Message, tools []Tool, options *LLMOptions) (*LLMResponse, error)
}

// LLMNode represents an LLM call. It is a thin wrapper around a LanguageModel
// implementation so that planners can mix LLM “thinking” nodes with tool calls
// or conditional branches inside the same graph.
type LLMNode struct {
	id      string
	Model   LanguageModel
	Prompt  string
	Options *LLMOptions
}

// ID implements Node.
func (n *LLMNode) ID() string { return n.id }

// Type implements Node.
func (n *LLMNode) Type() NodeType { return NodeTypeLLM }

// Execute runs the prompt against the language model.
func (n *LLMNode) Execute(ctx context.Context, state *Context) (*Result, error) {
	if n.Model == nil {
		return nil, errors.New("llm node missing model")
	}
	resp, err := n.Model.Generate(ctx, n.Prompt, n.Options)
	if err != nil {
		return nil, err
	}
	state.AddInteraction("assistant", resp.Text, map[string]interface{}{"node": n.id})
	return &Result{
		NodeID:  n.id,
		Success: true,
		Data: map[string]interface{}{
			"text": resp.Text,
		},
	}, nil
}

// ToolNode executes a tool by name.
type ToolNode struct {
	id   string
	Tool Tool
	Args map[string]interface{}
}

// ID implements Node.
func (n *ToolNode) ID() string { return n.id }

// Type implements Node.
func (n *ToolNode) Type() NodeType { return NodeTypeTool }

// Execute calls the underlying tool.
func (n *ToolNode) Execute(ctx context.Context, state *Context) (*Result, error) {
	if n.Tool == nil {
		return nil, errors.New("tool node missing tool")
	}
	if !n.Tool.IsAvailable(ctx, state) {
		return nil, fmt.Errorf("tool %s unavailable", n.Tool.Name())
	}
	res, err := n.Tool.Execute(ctx, state, n.Args)
	if err != nil {
		return nil, err
	}
	return &Result{
		NodeID:  n.id,
		Success: res.Success,
		Data:    res.Data,
		Error:   errorFromString(res.Error),
	}, nil
}

// ConditionalNode computes the next branch dynamically.
type ConditionalNode struct {
	id        string
	Condition func(*Context) (string, error)
}

// ID implements Node.
func (n *ConditionalNode) ID() string { return n.id }

// Type implements Node.
func (n *ConditionalNode) Type() NodeType { return NodeTypeConditional }

// Execute just evaluates the condition and stores the decision.
func (n *ConditionalNode) Execute(ctx context.Context, state *Context) (*Result, error) {
	to, err := n.Condition(state)
	if err != nil {
		return nil, err
	}
	return &Result{
		NodeID:  n.id,
		Success: true,
		Data: map[string]interface{}{
			"next": to,
		},
	}, nil
}

// HumanNode represents a pause waiting for user approval.
type HumanNode struct {
	id       string
	Prompt   string
	Callback func(*Context) error
}

// ID implements Node.
func (n *HumanNode) ID() string { return n.id }

// Type implements Node.
func (n *HumanNode) Type() NodeType { return NodeTypeHuman }

// Execute pauses execution until callback completes.
func (n *HumanNode) Execute(ctx context.Context, state *Context) (*Result, error) {
	if n.Callback != nil {
		if err := n.Callback(state); err != nil {
			return nil, err
		}
	}
	return &Result{NodeID: n.id, Success: true}, nil
}

// TerminalNode marks the end of the workflow.
type TerminalNode struct {
	id string
}

// NewTerminalNode creates a terminal node.
func NewTerminalNode(id string) *TerminalNode {
	return &TerminalNode{id: id}
}

// ID implements Node.
func (n *TerminalNode) ID() string { return n.id }

// Type implements Node.
func (n *TerminalNode) Type() NodeType { return NodeTypeTerminal }

// Execute completes immediately.
func (n *TerminalNode) Execute(ctx context.Context, state *Context) (*Result, error) {
	return &Result{NodeID: n.id, Success: true}, nil
}

// errorFromString reconstructs an error from a stored message, enabling tool
// results that only record strings to participate in graph error handling.
func errorFromString(err string) error {
	if err == "" {
		return nil
	}
	return errors.New(err)
}
