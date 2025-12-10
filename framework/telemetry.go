package framework

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

// EventType categorizes telemetry events.
type EventType string

const (
	EventGraphStart   EventType = "graph_start"
	EventGraphFinish  EventType = "graph_finish"
	EventNodeStart    EventType = "node_start"
	EventNodeFinish   EventType = "node_finish"
	EventNodeError    EventType = "node_error"
	EventAgentStart   EventType = "agent_start"
	EventAgentFinish  EventType = "agent_finish"
	EventToolCall     EventType = "tool_call"
	EventToolResult   EventType = "tool_result"
	EventStateChange  EventType = "state_change"
)

// Event captures structured telemetry data.
type Event struct {
	Type      EventType              `json:"type"`
	NodeID    string                 `json:"node_id,omitempty"`
	TaskID    string                 `json:"task_id,omitempty"`
	Message   string                 `json:"message,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Telemetry captures execution traces emitted by the graph runtime. Production
// deployments can implement OpenTelemetry exporters here, while tests typically
// swap in lightweight loggers.
type Telemetry interface {
	Emit(event Event)
}

// MultiplexTelemetry broadcasts events to multiple sinks.
type MultiplexTelemetry struct {
	Sinks []Telemetry
}

// Emit forwards the event to all registered sinks.
func (m MultiplexTelemetry) Emit(event Event) {
	for _, s := range m.Sinks {
		s.Emit(event)
	}
}

// JSONFileTelemetry writes events as newline-delimited JSON to a file.
// This allows external tools to tail and process the stream in real-time.
type JSONFileTelemetry struct {
	path string
	file *os.File
	enc  *json.Encoder
	mu   sync.Mutex
}

// NewJSONFileTelemetry opens (or creates) the log file.
func NewJSONFileTelemetry(path string) (*JSONFileTelemetry, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &JSONFileTelemetry{
		path: path,
		file: f,
		enc:  json.NewEncoder(f),
	}, nil
}

// Emit writes the JSON record.
func (j *JSONFileTelemetry) Emit(event Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.enc != nil {
		_ = j.enc.Encode(event)
	}
}

// Close releases the file handle.
func (j *JSONFileTelemetry) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file != nil {
		return j.file.Close()
	}
	return nil
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
