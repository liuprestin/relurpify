package tui

import (
	"strings"
	"time"
)

// StreamTokenMsg represents a streamed token from the agent pipeline.
type StreamTokenMsg struct {
	Token     string
	TokenType TokenType
	Metadata  map[string]interface{}
}

// TokenType enumerates the supported streaming categories.
type TokenType string

const (
	TokenText     TokenType = "text"
	TokenThinking TokenType = "thinking"
	TokenPlan     TokenType = "plan"
	TokenChange   TokenType = "change"
)

// StreamCompleteMsg signals that streaming has finished.
type StreamCompleteMsg struct {
	Duration   time.Duration
	TokensUsed int
}

// StreamErrorMsg wraps runtime failures for display.
type StreamErrorMsg struct {
	Error error
}

// MessageBuilder accumulates streaming state until completion.
type MessageBuilder struct {
	startTime     time.Time
	text          strings.Builder
	thinking      []ThinkingStep
	currentStep   *ThinkingStep
	plan          *TaskPlan
	changes       []FileChange
	currentChange *FileChange
}

// NewMessageBuilder constructs a builder with sane defaults.
func NewMessageBuilder() *MessageBuilder {
	return &MessageBuilder{
		startTime: time.Now(),
		thinking:  []ThinkingStep{},
		changes:   []FileChange{},
	}
}

// AddToken ingests the next streaming token.
func (mb *MessageBuilder) AddToken(token StreamTokenMsg) {
	switch token.TokenType {
	case TokenText:
		mb.text.WriteString(token.Token)
	case TokenThinking:
		mb.addThinking(token)
	case TokenPlan:
		mb.addPlanTask(token)
	case TokenChange:
		mb.addChange(token)
	}
}

func (mb *MessageBuilder) addThinking(token StreamTokenMsg) {
	kind, _ := token.Metadata["kind"].(string)
	switch kind {
	case "start":
		if mb.currentStep != nil {
			mb.currentStep.EndTime = time.Now()
			mb.thinking = append(mb.thinking, *mb.currentStep)
		}
		stepType := StepAnalyzing
		if t, ok := token.Metadata["stepType"].(string); ok {
			stepType = StepType(t)
		}
		desc, _ := token.Metadata["description"].(string)
		mb.currentStep = &ThinkingStep{
			Type:        stepType,
			Description: desc,
			StartTime:   time.Now(),
			Details:     []string{},
		}
	case "detail":
		if mb.currentStep == nil {
			return
		}
		if detail, ok := token.Metadata["detail"].(string); ok {
			mb.currentStep.Details = append(mb.currentStep.Details, detail)
		}
	default:
		if mb.currentStep == nil {
			return
		}
		if token.Token != "" {
			mb.currentStep.Details = append(mb.currentStep.Details, token.Token)
		}
	}
}

func (mb *MessageBuilder) addPlanTask(token StreamTokenMsg) {
	if mb.plan == nil {
		mb.plan = &TaskPlan{Tasks: []Task{}, StartTime: time.Now()}
	}
	desc, _ := token.Metadata["description"].(string)
	status := TaskPending
	if raw, ok := token.Metadata["status"].(string); ok {
		status = TaskStatus(raw)
	}
	mb.plan.Tasks = append(mb.plan.Tasks, Task{Description: desc, Status: status})
}

func (mb *MessageBuilder) addChange(token StreamTokenMsg) {
	if token.Metadata != nil {
		if path, ok := token.Metadata["path"].(string); ok && path != "" {
			if mb.currentChange != nil {
				mb.changes = append(mb.changes, *mb.currentChange)
			}
			changeType := ChangeModify
			if raw, ok := token.Metadata["type"].(string); ok && raw != "" {
				changeType = ChangeType(raw)
			}
			mb.currentChange = &FileChange{
				Path:   path,
				Type:   changeType,
				Status: StatusPending,
			}
			return
		}
	}
	if mb.currentChange == nil {
		return
	}
	mb.currentChange.Diff += token.Token
	if strings.HasPrefix(token.Token, "+") {
		mb.currentChange.LinesAdded++
	} else if strings.HasPrefix(token.Token, "-") {
		mb.currentChange.LinesRemoved++
	}
}

// Build finalizes the streaming message into a concrete agent message.
func (mb *MessageBuilder) Build(duration time.Duration, tokensUsed int) Message {
	if mb.currentStep != nil {
		mb.currentStep.EndTime = time.Now()
		mb.thinking = append(mb.thinking, *mb.currentStep)
		mb.currentStep = nil
	}
	if mb.currentChange != nil {
		mb.changes = append(mb.changes, *mb.currentChange)
		mb.currentChange = nil
	}
	return Message{
		ID:        generateID(),
		Timestamp: mb.startTime,
		Role:      RoleAgent,
		Content: MessageContent{
			Text:     mb.text.String(),
			Thinking: append([]ThinkingStep(nil), mb.thinking...),
			Plan:     clonePlan(mb.plan),
			Changes:  cloneChanges(mb.changes),
			Expanded: map[string]bool{"thinking": true, "plan": true, "changes": false},
		},
		Metadata: MessageMetadata{Duration: duration, TokensUsed: tokensUsed},
	}
}

// BuildPartial renders the in-progress agent response.
func (mb *MessageBuilder) BuildPartial() Message {
	thinking := append([]ThinkingStep(nil), mb.thinking...)
	if mb.currentStep != nil {
		thinking = append(thinking, *mb.currentStep)
	}
	changes := cloneChanges(mb.changes)
	if mb.currentChange != nil {
		changes = append(changes, *mb.currentChange)
	}
	return Message{
		ID:        "streaming",
		Timestamp: mb.startTime,
		Role:      RoleAgent,
		Content: MessageContent{
			Text:     mb.text.String(),
			Thinking: thinking,
			Plan:     clonePlan(mb.plan),
			Changes:  changes,
			Expanded: map[string]bool{"thinking": true, "plan": true, "changes": false},
		},
	}
}

func clonePlan(plan *TaskPlan) *TaskPlan {
	if plan == nil {
		return nil
	}
	cp := *plan
	cp.Tasks = append([]Task(nil), plan.Tasks...)
	return &cp
}

func cloneChanges(changes []FileChange) []FileChange {
	if len(changes) == 0 {
		return nil
	}
	out := make([]FileChange, len(changes))
	copy(out, changes)
	return out
}
