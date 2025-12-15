package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View composes the scrollable feed, prompt bar, and status bar.
func (m Model) View() string {
	if !m.ready || m.feed == nil {
		return "Initializing..."
	}

	feed := m.feed.View()
	prompt := m.renderPromptBar()
	status := m.statusBar.View(m.width)

	return lipgloss.JoinVertical(lipgloss.Left, feed, prompt, status)
}

func (m Model) renderMessages() string {
	if len(m.messages) == 0 {
		return welcomeStyle.Render("Welcome! Type a message or use /help for commands.")
	}
	rendered := make([]string, 0, len(m.messages))
	spinnerView := m.spinner.View()
	for _, msg := range m.messages {
		rendered = append(rendered, RenderMessage(msg, m.width, spinnerView))
	}
	return strings.Join(rendered, "\n\n")
}

func (m Model) renderPromptBar() string {
	prefix := "> "
	hint := dimStyle.Render(" / for commands | @ for context | ctrl+l to clear")
	promptText := ""

	switch m.mode {
	case ModeCommand:
		prefix = "/ "
		hint = dimStyle.Render(" Enter to run | Esc to cancel")
	case ModeFilePicker:
		prefix = "@ "
		hint = dimStyle.Render(" Enter to add file | Esc to cancel")
	case ModeHITL:
		prefix = "! "
		hint = dimStyle.Render(" y approve | n deny | Esc cancel")
		if m.hitlRequest != nil {
			promptText = fmt.Sprintf("Approve %s: %s (%s)?", m.hitlRequest.ID, m.hitlRequest.Permission.Action, m.hitlRequest.Justification)
		} else {
			promptText = "Approve pending permission?"
		}
	}

	content := prefix
	if m.mode == ModeHITL {
		content += promptText
	} else {
		content += m.input.View()
	}
	if hint != "" {
		content += " " + hint
	}
	return promptBarStyle.Width(m.width).Render(content)
}
