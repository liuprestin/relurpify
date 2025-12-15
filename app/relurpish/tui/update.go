package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lexcodex/relurpify/framework"
)

// Init fulfills the Bubble Tea Model interface.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick, listenHITLEvents(m.hitlCh))
}

// Update applies incoming Bubble Tea messages to mutate the Model state.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
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
		case ModeHITL:
			return m.handleHITLMode(msg)
		}
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
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
	case hitlResolvedMsg:
		return m.handleHITLResolved(msg)
	case hitlEventMsg:
		return m.handleHITLEvent(msg)
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

	if !m.ready || m.feed == nil {
		v := viewport.New(msg.Width, feedHeight)
		m.feed = &v
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
	case "up", "pgup", "home":
		if m.feed == nil {
			return m, nil
		}
		var cmd tea.Cmd
		updated, cmd := m.feed.Update(msg)
		m.feed = &updated
		m.autoFollow = m.feed.AtBottom()
		return m, cmd
	case "down", "pgdown", "end":
		if m.feed == nil {
			return m, nil
		}
		var cmd tea.Cmd
		updated, cmd := m.feed.Update(msg)
		m.feed = &updated
		if m.feed.AtBottom() {
			m.autoFollow = true
		}
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
	m = m.refreshFeedContent()
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
	m = m.refreshFeedContent()

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
	return m.refreshFeedContent(), nil
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
	return m.refreshFeedContent()
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
	return m.refreshFeedContent(), nil
}

func (m Model) handleHITLResolved(msg hitlResolvedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.addSystemMessage(fmt.Sprintf("HITL %s failed: %v", msg.requestID, msg.err)), listenHITLEvents(m.hitlCh)
	}
	if msg.approved {
		m = m.addSystemMessage(fmt.Sprintf("Approved %s", msg.requestID))
	} else {
		m = m.addSystemMessage(fmt.Sprintf("Denied %s", msg.requestID))
	}
	m = m.exitHITL()
	return m, listenHITLEvents(m.hitlCh)
}

func (m Model) handleHITLMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.hitlRequest == nil {
		return m.exitHITL(), listenHITLEvents(m.hitlCh)
	}
	switch msg.String() {
	case "y", "Y":
		return m, approveHITLCmd(m.hitl, m.hitlRequest.ID)
	case "n", "N", "esc":
		return m, denyHITLCmd(m.hitl, m.hitlRequest.ID)
	default:
		return m, nil
	}
}

func (m Model) handleHITLEvent(msg hitlEventMsg) (tea.Model, tea.Cmd) {
	// Keep listening for the next event.
	next := listenHITLEvents(m.hitlCh)

	// Sync to current pending list for robustness (handles bursts/missed events).
	var pending []*framework.PermissionRequest
	if m.hitl != nil {
		pending = m.hitl.PendingHITL()
	}

	switch msg.event.Type {
	case framework.HITLEventRequested:
		if len(pending) > 0 && m.mode != ModeHITL {
			req := pending[0]
			m = m.enterHITL(req)
			m = m.addSystemMessage(fmt.Sprintf("Permission requested: %s %s (%s)", req.ID, req.Permission.Action, req.Justification))
		}
	case framework.HITLEventResolved, framework.HITLEventExpired:
		// If current request is gone, exit HITL or advance to next pending.
		if m.mode == ModeHITL && m.hitlRequest != nil {
			current := m.hitlRequest.ID
			found := false
			for _, req := range pending {
				if req.ID == current {
					found = true
					break
				}
			}
			if !found {
				m = m.exitHITL()
				if len(pending) > 0 {
					m = m.enterHITL(pending[0])
				}
			}
		} else if len(pending) > 0 && m.mode != ModeHITL {
			m = m.enterHITL(pending[0])
		}
		if msg.event.Type == framework.HITLEventExpired && msg.event.Request != nil {
			reason := msg.event.Error
			if reason == "" {
				reason = "expired"
			}
			m = m.addSystemMessage(fmt.Sprintf("Permission %s expired: %s", msg.event.Request.ID, reason))
		}
	}

	return m, next
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
func summarizePendingHITL(rt hitlService) tea.Cmd {
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
