package framework

import (
	"log"
	"time"
)

// EventType categorizes telemetry events.
type EventType string

const (
	EventGraphStart  EventType = "graph_start"
	EventGraphFinish EventType = "graph_finish"
	EventNodeStart   EventType = "node_start"
	EventNodeFinish  EventType = "node_finish"
	EventNodeError   EventType = "node_error"
)

// Event captures structured telemetry data.
type Event struct {
	Type      EventType
	NodeID    string
	TaskID    string
	Message   string
	Timestamp time.Time
	Metadata  map[string]interface{}
}

// Telemetry captures execution traces emitted by the graph runtime. Production
// deployments can implement OpenTelemetry exporters here, while tests typically
// swap in lightweight loggers.
type Telemetry interface {
	Emit(event Event)
}

// ContextTelemetry extends telemetry with context-management specific signals.
type ContextTelemetry interface {
	OnContextCompression(taskID string, stats CompressionStats)
	OnContextPruning(taskID string, itemsRemoved int, tokensFreed int)
	OnBudgetExceeded(taskID string, attempted int, available int)
}

// CheckpointTelemetry extends telemetry with checkpoint lifecycle events.
type CheckpointTelemetry interface {
	OnCheckpointCreated(taskID string, checkpointID string, nodeID string)
	OnCheckpointRestored(taskID string, checkpointID string)
	OnGraphResume(taskID string, checkpointID string, nodeID string)
}

// LoggerTelemetry emits events via the standard logger. It is intentionally
// tiny yet immensely helpful while debugging workflows locally because every
// node transition becomes visible without extra tooling.
type LoggerTelemetry struct {
	Logger *log.Logger
}

// Emit logs the event.
func (t LoggerTelemetry) Emit(event Event) {
	logger := t.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("[%s] node=%s task=%s meta=%v msg=%s\n", event.Type, event.NodeID, event.TaskID, event.Metadata, event.Message)
}

func (t LoggerTelemetry) OnContextCompression(taskID string, stats CompressionStats) {
	logger := t.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("[context_compression] task=%s stats=%+v\n", taskID, stats)
}

func (t LoggerTelemetry) OnContextPruning(taskID string, itemsRemoved int, tokensFreed int) {
	logger := t.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("[context_pruning] task=%s removed=%d tokens=%d\n", taskID, itemsRemoved, tokensFreed)
}

func (t LoggerTelemetry) OnBudgetExceeded(taskID string, attempted int, available int) {
	logger := t.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("[budget_exceeded] task=%s attempted=%d available=%d\n", taskID, attempted, available)
}

func (t LoggerTelemetry) OnCheckpointCreated(taskID string, checkpointID string, nodeID string) {
	logger := t.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("[checkpoint_created] task=%s checkpoint=%s node=%s\n", taskID, checkpointID, nodeID)
}

func (t LoggerTelemetry) OnCheckpointRestored(taskID string, checkpointID string) {
	logger := t.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("[checkpoint_restored] task=%s checkpoint=%s\n", taskID, checkpointID)
}

func (t LoggerTelemetry) OnGraphResume(taskID string, checkpointID string, nodeID string) {
	logger := t.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("[graph_resume] task=%s checkpoint=%s node=%s\n", taskID, checkpointID, nodeID)
}
