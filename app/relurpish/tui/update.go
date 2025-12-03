package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	runtimesvc "github.com/lexcodex/relurpify/app/relurpish/runtime"
)

// Init fulfills the Bubble Tea Model interface.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update applies incoming Bubble Tea messages to mutate the Model state.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case tea.KeyMsg:
		// Global shortcuts
		switch msg.String() {
		case "ctrl+c", "ctrl+d":
			return m, tea.Quit
		case "ctrl+l":
			m.messages = nil
			return m, nil
		}
		switch m.mode {
		case ModeNormal:
			return m.handleNormalMode(msg)
		case ModeCommand:
			return m.handleCommandMode(msg)
		case ModeFilePicker:
			return m.handleFilePickerMode(msg)
		}
	case StreamTokenMsg:
		return m.handleStreamToken(msg)
	case StreamCompleteMsg:
		return m.handleStreamComplete(msg)
	case StreamErrorMsg:
		return m.handleStreamError(msg)
	case UpdateTaskMsg:
		return m.handleUpdateTask(msg)
	case hitlMsg:
		return m.addSystemMessage(msg.content), nil
	}
	return m, nil
}

// handleResize adjusts the feed/input layout on terminal resize events.
func (m Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height

	statusBarHeight := 1
	promptBarHeight := 1
	feedHeight := max(1, msg.Height-statusBarHeight-promptBarHeight)

	if !m.ready {
		m.feed = viewport.New(msg.Width, feedHeight)
		m.ready = true
	} else {
		m.feed.Width = msg.Width
		m.feed.Height = feedHeight
	}
	m.input.Width = max(10, msg.Width-4)
	return m, nil
}

// handleNormalMode implements the default prompt behavior described in the spec.
func (m Model) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyRunes {
		value := msg.String()
		if value == "/" && strings.TrimSpace(m.input.Value()) == "" {
			m.mode = ModeCommand
			m.input.SetValue("/")
			m.input.CursorEnd()
			return m, nil
		}
		if value == "@" && strings.TrimSpace(m.input.Value()) == "" {
			m.mode = ModeFilePicker
			m.input.SetValue("")
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "enter":
		return m.submitPrompt()
	case "up", "down", "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		m.feed, cmd = m.feed.Update(msg)
		return m, cmd
	case "tab":
		return m.toggleExpandAtCursor()
	case "ctrl+a":
		return m.approveCurrentChange()
	case "ctrl+r":
		return m.rejectCurrentChange()
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

// handleCommandMode processes slash-prefixed commands.
func (m Model) handleCommandMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		raw := strings.TrimSpace(m.input.Value())
		if raw == "" {
			m.mode = ModeNormal
			return m, nil
		}
		if !strings.HasPrefix(raw, "/") {
			raw = "/" + raw
		}
		cmdName, args := parseCommand(raw)
		m.input.SetValue("")
		m.mode = ModeNormal
		if cmdName == "" {
			return m, nil
		}
		return handleCommand(m, cmdName, args)
	case "esc":
		m.mode = ModeNormal
		m.input.SetValue("")
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

// handleFilePickerMode lets the user add files to agent context via @.
func (m Model) handleFilePickerMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		selection := strings.TrimSpace(m.input.Value())
		if selection != "" {
			if err := m.context.AddFile(selection); err != nil {
				m = m.addSystemMessage(fmt.Sprintf("Context error: %v", err))
			} else {
				m = m.addSystemMessage(fmt.Sprintf("Added to context: %s", selection))
			}
		}
		m.input.SetValue("")
		m.mode = ModeNormal
		return m, nil
	case "esc":
		m.mode = ModeNormal
		m.input.SetValue("")
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

// handleStreamToken updates the live streaming message as new tokens arrive.
func (m Model) handleStreamToken(msg StreamTokenMsg) (tea.Model, tea.Cmd) {
	if !m.streaming || m.streamBuf == nil {
		return m, nil
	}
	m.streamBuf.AddToken(msg)
	partial := m.streamBuf.BuildPartial()

	if len(m.messages) > 0 && m.messages[len(m.messages)-1].ID == "streaming" {
		m.messages[len(m.messages)-1] = partial
	} else {
		m.messages = append(m.messages, partial)
	}
	m.feed.GotoBottom()
	if m.streamCh != nil {
		return m, listenToStream(m.streamCh)
	}
	return m, nil
}

// handleStreamComplete finalizes the message once streaming stops.
func (m Model) handleStreamComplete(msg StreamCompleteMsg) (tea.Model, tea.Cmd) {
	if !m.streaming || m.streamBuf == nil {
		return m, nil
	}
	final := m.streamBuf.Build(msg.Duration, msg.TokensUsed)
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].ID == "streaming" {
		m.messages[len(m.messages)-1] = final
	} else {
		m.messages = append(m.messages, final)
	}

	m.session.TotalTokens += msg.TokensUsed
	m.session.TotalDuration += msg.Duration

	m.statusBar.tokens = m.session.TotalTokens
	m.statusBar.duration = m.session.TotalDuration
	m.statusBar.lastUpdate = time.Now()

	m.streaming = false
	m.streamBuf = nil
	m.streamCh = nil
	return m, nil
}

// handleStreamError writes system level errors whenever streaming fails.
func (m Model) handleStreamError(msg StreamErrorMsg) (tea.Model, tea.Cmd) {
	m.streaming = false
	m.streamBuf = nil
	m.streamCh = nil
	return m.addSystemMessage(fmt.Sprintf("⚠️  agent error: %v", msg.Error)), nil
}

// UpdateTaskMsg allows external messages to update plan status in-place.
type UpdateTaskMsg struct {
	TaskIndex int
	Status    TaskStatus
}

// handleUpdateTask toggles plan status updates in real time.
func (m Model) handleUpdateTask(msg UpdateTaskMsg) (tea.Model, tea.Cmd) {
	for i := len(m.messages) - 1; i >= 0; i-- {
		content := &m.messages[i].Content
		if content.Plan == nil {
			continue
		}
		if msg.TaskIndex >= 0 && msg.TaskIndex < len(content.Plan.Tasks) {
			task := &content.Plan.Tasks[msg.TaskIndex]
			task.Status = msg.Status
			switch msg.Status {
			case TaskInProgress:
				task.StartTime = time.Now()
			case TaskCompleted:
				task.EndTime = time.Now()
			}
			break
		}
	}
	return m, nil
}

// handleCommand dispatchers -------------------------------------------------

func (m Model) addSystemMessage(text string) Model {
	sys := Message{
		ID:        generateID(),
		Timestamp: time.Now(),
		Role:      RoleSystem,
		Content:   MessageContent{Text: text},
	}
	m.messages = append(m.messages, sys)
	m.feed.GotoBottom()
	return m
}

func (m Model) approveCurrentChange() (tea.Model, tea.Cmd) {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.Role != RoleAgent {
			continue
		}
		for j := range msg.Content.Changes {
			change := &msg.Content.Changes[j]
			if change.Status == StatusPending {
				change.Status = StatusApproved
				return m.addSystemMessage(fmt.Sprintf("Approved %s", change.Path)), nil
			}
		}
	}
	return m, nil
}

func (m Model) rejectCurrentChange() (tea.Model, tea.Cmd) {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.Role != RoleAgent {
			continue
		}
		for j := range msg.Content.Changes {
			change := &msg.Content.Changes[j]
			if change.Status == StatusPending {
				change.Status = StatusRejected
				return m.addSystemMessage(fmt.Sprintf("Rejected %s", change.Path)), nil
			}
		}
	}
	return m, nil
}

func (m Model) toggleExpandAtCursor() (tea.Model, tea.Cmd) {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.Role != RoleAgent {
			continue
		}
		if msg.Content.Expanded == nil {
			msg.Content.Expanded = map[string]bool{}
		}
		msg.Content.Expanded["thinking"] = !msg.Content.Expanded["thinking"]
		msg.Content.Expanded["plan"] = !msg.Content.Expanded["plan"]
		msg.Content.Expanded["changes"] = !msg.Content.Expanded["changes"]
		break
	}
	return m, nil
}

// listenToStream adapts Go channels to Bubble Tea commands for streaming.
func listenToStream(ch <-chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// Utility command to surface pending HITL approvals via /hitl.
func summarizePendingHITL(rt *runtimesvc.Runtime) tea.Cmd {
	if rt == nil {
		return nil
	}
	return func() tea.Msg {
		pending := rt.PendingHITL()
		if len(pending) == 0 {
			return hitlMsg{content: "No pending approvals"}
		}
		var b strings.Builder
		b.WriteString("Pending approvals:\n")
		for _, req := range pending {
			b.WriteString(fmt.Sprintf(" - %s %s (%s)\n", req.ID, req.Permission.Action, req.Justification))
		}
		return hitlMsg{content: b.String()}
	}
}

// hitlMsg surfaces HITL info back into the feed.
type hitlMsg struct{ content string }
