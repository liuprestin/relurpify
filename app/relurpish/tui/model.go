package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	runtimesvc "github.com/lexcodex/relurpify/app/relurpish/runtime"
	"github.com/lexcodex/relurpify/framework"
)

// Run bootstraps the new agentic TUI experience.
func Run(ctx context.Context, rt *runtimesvc.Runtime) error {
	if rt == nil {
		return fmt.Errorf("runtime is required")
	}
	model := NewModel(rt)
	program := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := program.Run()
	return err
}

// Model implements the Bubble Tea Model interface and coordinates the feed,
// prompt bar, and status bar components described in the new UX spec.
type Model struct {
	runtime *runtimesvc.Runtime
	config  runtimesvc.Config

	feed  *viewport.Model
	input textinput.Model
	spinner spinner.Model

	statusBar StatusBar

	messages []Message
	context  *AgentContext
	session  *Session

	width  int
	height int
	ready  bool

	mode InputMode

	streaming bool
	streamBuf *MessageBuilder
	streamCh  chan tea.Msg

	focusIndex int
	autoFollow bool
}

// InputMode tracks the role of the prompt bar.
type InputMode int

const (
	ModeNormal InputMode = iota
	ModeCommand
	ModeFilePicker
)

// Message structures mirror the specification for rendering rich agent output.
type Message struct {
	ID        string
	Timestamp time.Time
	Role      MessageRole
	Content   MessageContent
	Metadata  MessageMetadata
}

// MessageRole identifies the role of each entry in the feed.
type MessageRole string

const (
	RoleUser   MessageRole = "user"
	RoleAgent  MessageRole = "agent"
	RoleSystem MessageRole = "system"
)

// MessageContent stores the text, plan, and change information for a message.
type MessageContent struct {
	Text     string
	Thinking []ThinkingStep
	Changes  []FileChange
	Plan     *TaskPlan
	Expanded map[string]bool
}

// ThinkingStep captures an individual reasoning step emitted by the agent.
type ThinkingStep struct {
	Type        StepType
	Description string
	StartTime   time.Time
	EndTime     time.Time
	Details     []string
}

// StepType enumerates reasoning phases.
type StepType string

const (
	StepAnalyzing StepType = "analyzing"
	StepPlanning  StepType = "planning"
	StepCoding    StepType = "coding"
	StepTesting   StepType = "testing"
)

// FileChange represents a diff surfaced by the agent.
type FileChange struct {
	Path         string
	Status       ChangeStatus
	Type         ChangeType
	Diff         string
	LinesAdded   int
	LinesRemoved int
	Expanded     bool
}

// ChangeStatus tracks approval state for file changes.
type ChangeStatus string

const (
	StatusPending  ChangeStatus = "pending"
	StatusApproved ChangeStatus = "approved"
	StatusRejected ChangeStatus = "rejected"
)

// ChangeType identifies type of modification.
type ChangeType string

const (
	ChangeCreate ChangeType = "create"
	ChangeModify ChangeType = "modify"
	ChangeDelete ChangeType = "delete"
)

// TaskPlan mirrors the agent plan summary in the spec.
type TaskPlan struct {
	Tasks     []Task
	StartTime time.Time
}

// Task describes one actionable item in the plan.
type Task struct {
	Description string
	Status      TaskStatus
	StartTime   time.Time
	EndTime     time.Time
}

// TaskStatus enumerates plan state.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
)

// MessageMetadata contains per-message metrics (duration, tokens).
type MessageMetadata struct {
	Duration    time.Duration
	TokensUsed  int
	TokensTotal int
}

// Session tracks high-level session metadata for the status bar.
type Session struct {
	ID            string
	StartTime     time.Time
	Workspace     string
	Model         string
	Agent         string
	Mode          string
	Strategy      string
	TotalTokens   int
	TotalDuration time.Duration
}

// AgentContext records the active context files and token budget.
type AgentContext struct {
	Files       []string
	Directories []string
	MaxTokens   int
	UsedTokens  int
}

// AddFile registers a file path with de-duplication and budget validation.
func (ac *AgentContext) AddFile(path string) error {
	if ac == nil {
		return fmt.Errorf("context unavailable")
	}
	clean := filepath.Clean(path)
	for _, existing := range ac.Files {
		if existing == clean {
			return fmt.Errorf("%s already in context", clean)
		}
	}
	ac.Files = append(ac.Files, clean)
	return nil
}

// RemoveFile removes the file from the context list if present.
func (ac *AgentContext) RemoveFile(path string) {
	if ac == nil {
		return
	}
	clean := filepath.Clean(path)
	for i, existing := range ac.Files {
		if existing == clean {
			ac.Files = append(ac.Files[:i], ac.Files[i+1:]...)
			return
		}
	}
}

// List returns a snapshot of files currently in context.
func (ac *AgentContext) List() []string {
	if ac == nil {
		return nil
	}
	out := make([]string, len(ac.Files))
	copy(out, ac.Files)
	return out
}

// NewModel initializes the prompt/input/feed model with defaults from runtime.
func NewModel(rt *runtimesvc.Runtime) Model {
	cfg := rt.Config
	input := textinput.New()
	input.Placeholder = "Type a message or /help for commands"
	input.Focus()

	v := viewport.New(0, 0)
	vp := &v

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	session := &Session{
		ID:        fmt.Sprintf("session-%d", time.Now().UnixNano()),
		StartTime: time.Now(),
		Workspace: cfg.Workspace,
		Model:     cfg.OllamaModel,
		Agent:     cfg.AgentLabel(),
		Mode:      string(framework.AgentModePrimary),
	}

	if rt.Registration != nil && rt.Registration.Manifest != nil {
		manifest := rt.Registration.Manifest
		session.Agent = manifest.Metadata.Name
		if manifest.Spec.Agent != nil {
			if manifest.Spec.Agent.Model.Name != "" {
				session.Model = manifest.Spec.Agent.Model.Name
			}
			if manifest.Spec.Agent.Mode != "" {
				session.Mode = string(manifest.Spec.Agent.Mode)
			}
		}
	}

	status := StatusBar{
		workspace:  session.Workspace,
		model:      session.Model,
		agent:      session.Agent,
		mode:       session.Mode,
		tokens:     session.TotalTokens,
		duration:   session.TotalDuration,
		lastUpdate: time.Now(),
	}

	ctx := &AgentContext{
		Files:     []string{},
		MaxTokens: 100_000,
	}
	if rt.Registration != nil && rt.Registration.Manifest != nil && rt.Registration.Manifest.Spec.Agent != nil {
		if limit := rt.Registration.Manifest.Spec.Agent.Context.MaxTokens; limit > 0 {
			ctx.MaxTokens = limit
		}
	}

	return Model{
		runtime:    rt,
		config:     cfg,
		feed:       vp,
		input:      input,
		spinner:    sp,
		statusBar:  status,
		messages:   []Message{},
		context:    ctx,
		session:    session,
		mode:       ModeNormal,
		autoFollow: true,
	}
}

// submitPrompt orchestrates sending the current input to the agent runtime.
func (m Model) submitPrompt() (Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		return m, nil
	}

	userMsg := Message{
		ID:        generateID(),
		Timestamp: time.Now(),
		Role:      RoleUser,
		Content: MessageContent{
			Text: value,
		},
	}
	m.messages = append(m.messages, userMsg)
	m = m.refreshFeedContent()

	m.input.SetValue("")
	m.mode = ModeNormal

	m.streaming = true
	m.streamBuf = NewMessageBuilder()

	ch := make(chan tea.Msg)
	m.streamCh = ch
	go m.runAgentStream(ch, value)

	return m, listenToStream(ch)
}

// runAgentStream executes the runtime instruction and emits streaming events.
func (m Model) runAgentStream(ch chan tea.Msg, prompt string) {
	if ch == nil {
		return
	}
	start := time.Now()
	ch <- StreamTokenMsg{
		TokenType: TokenThinking,
		Metadata: map[string]interface{}{
			"kind":        "start",
			"stepType":    string(StepAnalyzing),
			"description": "Analyzing request",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	metadata := map[string]any{
		"source":        "relurpish",
		"context_files": append([]string(nil), m.context.Files...),
	}
	if _, ok := metadata["mode"]; !ok && m.session != nil {
		metadata["mode"] = m.session.Mode
	}
	if _, ok := metadata["strategy"]; !ok && m.session != nil && m.session.Strategy != "" {
		metadata["strategy"] = m.session.Strategy
	}
	result, err := m.runtime.ExecuteInstruction(ctx, prompt, framework.TaskTypeCodeGeneration, metadata)
	if err != nil {
		ch <- StreamErrorMsg{Error: err}
		ch <- StreamCompleteMsg{Duration: time.Since(start), TokensUsed: 0}
		close(ch)
		return
	}

	summary := summarizeResult(result)
	if summary != "" {
		ch <- StreamTokenMsg{TokenType: TokenText, Token: summary}
	}

	ch <- StreamCompleteMsg{Duration: time.Since(start), TokensUsed: estimateTokens(summary)}
	close(ch)
}

// summarizeResult turns a framework.Result into human readable feed text.
func summarizeResult(res *framework.Result) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Task node: ")
	b.WriteString(res.NodeID)
	b.WriteString("\nSuccess: ")
	if res.Success {
		b.WriteString("true")
	} else {
		b.WriteString("false")
	}
	if len(res.Data) > 0 {
		b.WriteString("\nData: ")
		b.WriteString(fmt.Sprintf("%v", res.Data))
	}
	if res.Error != nil {
		b.WriteString("\nError: ")
		b.WriteString(res.Error.Error())
	}
	return b.String()
}

// estimateTokens performs a rough heuristic conversion from characters to tokens.
func estimateTokens(content string) int {
	if content == "" {
		return 0
	}
	return max(1, len(content)/4)
}

// generateID produces a lightweight unique identifier for feed entries.
func generateID() string {
	return fmt.Sprintf("msg-%d", time.Now().UnixNano())
}

// max helper avoids importing math for a single use.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// refreshFeedContent ensures the viewport reflects the latest messages.
func (m Model) refreshFeedContent() Model {
	if !m.ready || m.feed == nil {
		return m
	}
	m.feed.SetContent(m.renderMessages())
	if m.autoFollow {
		m.feed.GotoBottom()
	}
	return m
}
