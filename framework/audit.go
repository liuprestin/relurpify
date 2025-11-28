package framework

import (
	"context"
	"errors"
	"sync"
	"time"
)

// AuditAction categorizes records for downstream processing.
type AuditAction string

const (
	AuditActionFileAccess AuditAction = "file_access"
	AuditActionExec       AuditAction = "exec"
	AuditActionNetwork    AuditAction = "network"
	AuditActionCapability AuditAction = "capability"
	AuditActionIPC        AuditAction = "ipc"
	AuditActionTool       AuditAction = "tool"
	AuditActionRequest    AuditAction = "permission_request"
)

// AuditRecord captures a single trace event.
type AuditRecord struct {
	Timestamp   time.Time              `json:"timestamp"`
	AgentID     string                 `json:"agent_id"`
	Action      string                 `json:"action"`
	Type        string                 `json:"type"`
	Permission  string                 `json:"permission"`
	Result      string                 `json:"result"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	User        string                 `json:"user,omitempty"`
	Correlation string                 `json:"correlation_id,omitempty"`
}

// AuditLogger defines the logging backend.
type AuditLogger interface {
	Log(ctx context.Context, record AuditRecord) error
	Query(ctx context.Context, filter AuditQuery) ([]AuditRecord, error)
}

// AuditQuery filters audit entries.
type AuditQuery struct {
	AgentID    string
	Action     string
	Type       string
	TimeStart  time.Time
	TimeEnd    time.Time
	Permission string
	Result     string
}

// InMemoryAuditLogger appends logs to a bounded buffer.
type InMemoryAuditLogger struct {
	mu     sync.RWMutex
	buffer []AuditRecord
	limit  int
}

// NewInMemoryAuditLogger builds a default logger.
func NewInMemoryAuditLogger(limit int) *InMemoryAuditLogger {
	if limit == 0 {
		limit = 2048
	}
	return &InMemoryAuditLogger{
		buffer: make([]AuditRecord, 0, limit),
		limit:  limit,
	}
}

// Log appends the record to the buffer.
func (l *InMemoryAuditLogger) Log(_ context.Context, record AuditRecord) error {
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buffer) == l.limit {
		l.buffer = l.buffer[1:]
	}
	l.buffer = append(l.buffer, record)
	return nil
}

// Query filters based on the supplied query.
func (l *InMemoryAuditLogger) Query(_ context.Context, filter AuditQuery) ([]AuditRecord, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var result []AuditRecord
	for _, record := range l.buffer {
		if filter.AgentID != "" && record.AgentID != filter.AgentID {
			continue
		}
		if filter.Type != "" && record.Type != filter.Type {
			continue
		}
		if filter.Action != "" && record.Action != filter.Action {
			continue
		}
		if !filter.TimeStart.IsZero() && record.Timestamp.Before(filter.TimeStart) {
			continue
		}
		if !filter.TimeEnd.IsZero() && record.Timestamp.After(filter.TimeEnd) {
			continue
		}
		if filter.Permission != "" && record.Permission != filter.Permission {
			continue
		}
		if filter.Result != "" && record.Result != filter.Result {
			continue
		}
		result = append(result, record)
	}
	return result, nil
}

// AuditStore exposes a read API for servers or dashboards.
type AuditStore struct {
	logger AuditLogger
}

// NewAuditStore builds the store.
func NewAuditStore(logger AuditLogger) *AuditStore {
	return &AuditStore{logger: logger}
}

// Query proxies the request.
func (s *AuditStore) Query(ctx context.Context, filter AuditQuery) ([]AuditRecord, error) {
	if s.logger == nil {
		return nil, errors.New("audit logger missing")
	}
	return s.logger.Query(ctx, filter)
}
