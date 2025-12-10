package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar renders workspace/model/agent metadata plus tokens & duration.
type StatusBar struct {
	workspace  string
	model      string
	agent      string
	mode       string
	strategy   string
	tokens     int
	duration   time.Duration
	lastUpdate time.Time
}

func (s StatusBar) View(width int) string {
	modeStr := s.mode
	if s.strategy != "" {
		modeStr = fmt.Sprintf("%s (%s)", s.mode, s.strategy)
	}
	left := fmt.Sprintf("📁 %s | 🤖 %s | 👤 %s | 🔧 %s",
		truncate(s.workspace, 20),
		s.model,
		s.agent,
		modeStr,
	)
	right := fmt.Sprintf("🪙 %s | ⏱️  %s",
		formatTokens(s.tokens),
		formatDuration(s.duration),
	)
	padding := width - lipgloss.Width(left) - lipgloss.Width(right)
	if padding < 0 {
		padding = 0
	}
	return statusStyle.Render(left + strings.Repeat(" ", padding) + right)
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:1]
	}
	return s[:n-1] + "…"
}
