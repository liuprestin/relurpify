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
