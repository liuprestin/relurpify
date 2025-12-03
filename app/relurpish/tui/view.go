package tui

import (
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
	for _, msg := range m.messages {
		rendered = append(rendered, RenderMessage(msg, m.width))
	}
	return strings.Join(rendered, "\n\n")
}

func (m Model) renderPromptBar() string {
	prefix := "> "
	hint := dimStyle.Render(" / for commands | @ for context | ctrl+l to clear")

	switch m.mode {
	case ModeCommand:
		prefix = "/ "
		hint = dimStyle.Render(" Enter to run | Esc to cancel")
	case ModeFilePicker:
		prefix = "@ "
		hint = dimStyle.Render(" Enter to add file | Esc to cancel")
	}

	content := prefix + m.input.View()
	if hint != "" {
		content += " " + hint
	}
	return promptBarStyle.Width(m.width).Render(content)
}
