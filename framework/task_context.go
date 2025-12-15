package framework

import "context"

type taskContextKey struct{}

// TaskContext carries high-level task metadata through contexts so telemetry
// and downstream components can correlate LLM/tool activity to a specific task.
type TaskContext struct {
	ID          string
	Type        TaskType
	Instruction string
}

// WithTaskContext attaches task metadata to the context.
func WithTaskContext(ctx context.Context, task TaskContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, taskContextKey{}, task)
}

// TaskContextFrom extracts task metadata, if present.
func TaskContextFrom(ctx context.Context) (TaskContext, bool) {
	if ctx == nil {
		return TaskContext{}, false
	}
	val := ctx.Value(taskContextKey{})
	task, ok := val.(TaskContext)
	return task, ok
}

