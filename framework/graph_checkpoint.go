package framework

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// GraphCheckpoint captures graph execution state for resumable workflows.
type GraphCheckpoint struct {
	CheckpointID      string                 `json:"checkpoint_id"`
	TaskID            string                 `json:"task_id"`
	CreatedAt         time.Time              `json:"created_at"`
	CurrentNodeID     string                 `json:"current_node_id"`
	VisitCounts       map[string]int         `json:"visit_counts"`
	ExecutionPath     []string               `json:"execution_path"`
	Context           *Context               `json:"context"`
	CompressedContext *CompressedContext     `json:"compressed_context,omitempty"`
	GraphHash         string                 `json:"graph_hash"`
	Metadata          map[string]interface{} `json:"metadata"`
}

// CreateCheckpoint captures the current execution state for later resumption.
func (g *Graph) CreateCheckpoint(taskID, currentNodeID string, ctx *Context) (*GraphCheckpoint, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil context")
	}
	ctxClone := ctx.Clone()
	checkpoint := &GraphCheckpoint{
		CheckpointID:  generateCheckpointID(),
		TaskID:        taskID,
		CreatedAt:     time.Now().UTC(),
		CurrentNodeID: currentNodeID,
		VisitCounts:   g.copyVisitCounts(),
		ExecutionPath: g.copyExecutionPath(),
		Context:       ctxClone,
		GraphHash:     g.computeHash(),
		Metadata:      make(map[string]interface{}),
	}
	if telemetry, ok := g.telemetry.(CheckpointTelemetry); ok {
		telemetry.OnCheckpointCreated(taskID, checkpoint.CheckpointID, currentNodeID)
	}
	return checkpoint, nil
}

// CreateCompressedCheckpoint captures a checkpoint while compressing history.
func (g *Graph) CreateCompressedCheckpoint(taskID, currentNodeID string, ctx *Context, llm LanguageModel, strategy CompressionStrategy) (*GraphCheckpoint, error) {
	checkpoint, err := g.CreateCheckpoint(taskID, currentNodeID, ctx)
	if err != nil {
		return nil, err
	}
	if strategy == nil || llm == nil {
		return checkpoint, nil
	}
	if !strategy.ShouldCompress(ctx, nil) {
		return checkpoint, nil
	}
	ctx.mu.RLock()
	historyCopy := append([]Interaction(nil), ctx.history...)
	ctx.mu.RUnlock()
	if len(historyCopy) == 0 {
		return checkpoint, nil
	}
	compressed, err := strategy.Compress(historyCopy, llm)
	if err != nil {
		return nil, fmt.Errorf("failed to compress checkpoint: %w", err)
	}
	checkpoint.CompressedContext = compressed
	if len(checkpoint.Context.history) > 5 {
		start := len(checkpoint.Context.history) - 5
		checkpoint.Context.history = append([]Interaction(nil), checkpoint.Context.history[start:]...)
	}
	return checkpoint, nil
}

// ResumeFromCheckpoint validates and resumes from the provided checkpoint.
func (g *Graph) ResumeFromCheckpoint(ctx context.Context, checkpoint *GraphCheckpoint) (*Result, error) {
	if checkpoint == nil {
		return nil, fmt.Errorf("nil checkpoint")
	}
	if g.computeHash() != checkpoint.GraphHash {
		return nil, fmt.Errorf("graph definition has changed since checkpoint")
	}
	state := checkpoint.Context
	if state == nil {
		state = NewContext()
	}
	if checkpoint.CompressedContext != nil {
		state.mu.Lock()
		state.compressedHistory = append(state.compressedHistory, *checkpoint.CompressedContext)
		state.mu.Unlock()
	}
	g.execMu.Lock()
	g.visitCounts = make(map[string]int)
	for node, count := range checkpoint.VisitCounts {
		g.visitCounts[node] = count
	}
	g.executionPath = append([]string(nil), checkpoint.ExecutionPath...)
	g.lastCheckpointNode = checkpoint.CurrentNodeID
	g.execMu.Unlock()
	if telemetry, ok := g.telemetry.(CheckpointTelemetry); ok {
		telemetry.OnCheckpointRestored(checkpoint.TaskID, checkpoint.CheckpointID)
		telemetry.OnGraphResume(checkpoint.TaskID, checkpoint.CheckpointID, checkpoint.CurrentNodeID)
	}
	return g.run(ctx, state, checkpoint.CurrentNodeID, false, checkpoint.TaskID)
}

func generateCheckpointID() string {
	return fmt.Sprintf("ckpt_%d", time.Now().UnixNano())
}

func (g *Graph) copyVisitCounts() map[string]int {
	copyMap := make(map[string]int, len(g.visitCounts))
	for k, v := range g.visitCounts {
		copyMap[k] = v
	}
	return copyMap
}

func (g *Graph) copyExecutionPath() []string {
	return append([]string(nil), g.executionPath...)
}

func (g *Graph) computeHash() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	nodeIDs := make([]string, 0, len(g.nodes))
	for id := range g.nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	var sb strings.Builder
	for _, id := range nodeIDs {
		sb.WriteString(id)
	}
	edgeKeys := make([]string, 0, len(g.edges))
	for id := range g.edges {
		edgeKeys = append(edgeKeys, id)
	}
	sort.Strings(edgeKeys)
	for _, from := range edgeKeys {
		for _, edge := range g.edges[from] {
			sb.WriteString(edge.From)
			sb.WriteString(edge.To)
		}
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}
