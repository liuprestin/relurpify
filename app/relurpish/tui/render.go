package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// RenderMessage converts a Message into a styled string for the viewport.
func RenderMessage(msg Message, width int) string {
	var b strings.Builder

	header := renderMessageHeader(msg)
	b.WriteString(header)
	b.WriteString("\n")

	switch msg.Role {
	case RoleUser:
		b.WriteString(renderUserMessage(msg))
	case RoleAgent:
		b.WriteString(renderAgentMessage(msg, width))
	case RoleSystem:
		b.WriteString(renderSystemMessage(msg))
	}

	if msg.Metadata.Duration > 0 {
		footer := renderMessageFooter(msg)
		b.WriteString("\n")
		b.WriteString(footer)
	}

	boxWidth := max(0, width-4)
	return messageBoxStyle.Width(boxWidth).Render(b.String())
}

func renderMessageHeader(msg Message) string {
	timestamp := msg.Timestamp.Format("15:04:05")
	icon := "💬"
	roleText := "User"
	switch msg.Role {
	case RoleUser:
		icon = "👤"
		roleText = "You"
	case RoleAgent:
		icon = "🤖"
		roleText = "Agent"
	case RoleSystem:
		icon = "⚙️"
		roleText = "System"
	}
	return headerStyle.Render(fmt.Sprintf("%s [%s] %s", icon, timestamp, roleText))
}

func renderUserMessage(msg Message) string {
	return textStyle.Render(msg.Content.Text)
}

func renderSystemMessage(msg Message) string {
	return dimStyle.Render(msg.Content.Text)
}

func renderAgentMessage(msg Message, width int) string {
	var b strings.Builder

	if len(msg.Content.Thinking) > 0 {
		b.WriteString(renderThinkingSection(msg.Content.Thinking, msg.Content.Expanded["thinking"], width))
		b.WriteString("\n\n")
	}

	if msg.Content.Plan != nil {
		b.WriteString(renderPlanSection(msg.Content.Plan, msg.Content.Expanded["plan"], width))
		b.WriteString("\n\n")
	}

	if len(msg.Content.Changes) > 0 {
		b.WriteString(renderChangesSection(msg.Content.Changes, msg.Content.Expanded["changes"], width))
		b.WriteString("\n\n")
	}

	if msg.Content.Text != "" {
		b.WriteString(textStyle.Render(msg.Content.Text))
	}

	return b.String()
}

func renderThinkingSection(steps []ThinkingStep, expanded bool, width int) string {
	var b strings.Builder
	header := "🤔 Thinking"
	toggle := "[−]"
	if !expanded {
		toggle = "[+]"
	}
	b.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("%s %s", header, dimStyle.Render(toggle))))
	b.WriteString("\n")
	if !expanded {
		summary := fmt.Sprintf("%d steps", len(steps))
		b.WriteString(dimStyle.Render(summary))
		return b.String()
	}
	for i, step := range steps {
		isLast := i == len(steps)-1
		prefix := "├─"
		if isLast {
			prefix = "└─"
		}
		icon := getStepIcon(step.Type)
		duration := ""
		if !step.EndTime.IsZero() {
			d := step.EndTime.Sub(step.StartTime)
			duration = dimStyle.Render(fmt.Sprintf(" (%s)", formatDuration(d)))
		}
		b.WriteString(fmt.Sprintf("%s %s %s%s\n", dimStyle.Render(prefix), icon, step.Description, duration))
		for _, detail := range step.Details {
			subPrefix := "│ "
			if isLast {
				subPrefix = "  "
			}
			b.WriteString(dimStyle.Render(subPrefix) + "  " + detailStyle.Render(detail) + "\n")
		}
	}
	return b.String()
}

func getStepIcon(t StepType) string {
	switch t {
	case StepAnalyzing:
		return "🔍"
	case StepPlanning:
		return "💭"
	case StepCoding:
		return "✏️"
	case StepTesting:
		return "🧪"
	default:
		return "•"
	}
}

func renderPlanSection(plan *TaskPlan, expanded bool, width int) string {
	var b strings.Builder
	completed := 0
	for _, task := range plan.Tasks {
		if task.Status == TaskCompleted {
			completed++
		}
	}
	header := fmt.Sprintf("💡 Plan (%d/%d)", completed, len(plan.Tasks))
	toggle := "[−]"
	if !expanded {
		toggle = "[+]"
	}
	b.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("%s %s", header, dimStyle.Render(toggle))))
	b.WriteString("\n")
	if !expanded {
		return b.String()
	}
	for _, task := range plan.Tasks {
		var icon string
		var style lipgloss.Style
		switch task.Status {
		case TaskCompleted:
			icon = "✅"
			style = completedStyle
		case TaskInProgress:
			icon = "⏳"
			style = inProgressStyle
		default:
			icon = "☐"
			style = pendingStyle
		}
		duration := ""
		if task.Status == TaskCompleted && !task.EndTime.IsZero() {
			d := task.EndTime.Sub(task.StartTime)
			duration = dimStyle.Render(fmt.Sprintf(" (%s)", formatDuration(d)))
		}
		b.WriteString(fmt.Sprintf("%s %s%s\n", icon, style.Render(task.Description), duration))
	}
	return b.String()
}

func renderChangesSection(changes []FileChange, expanded bool, width int) string {
	var b strings.Builder
	totalAdded := 0
	totalRemoved := 0
	for _, change := range changes {
		totalAdded += change.LinesAdded
		totalRemoved += change.LinesRemoved
	}
	header := fmt.Sprintf("✏️  Changes (%d files, +%d -%d)", len(changes), totalAdded, totalRemoved)
	toggle := "[−]"
	if !expanded {
		toggle = "[+]"
	}
	b.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("%s %s", header, dimStyle.Render(toggle))))
	b.WriteString("\n")
	if !expanded {
		for _, change := range changes {
			b.WriteString(renderChangeCompact(change))
			b.WriteString("\n")
		}
		return b.String()
	}
	for i, change := range changes {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(renderChangeFull(change, width))
	}
	pending := false
	for _, c := range changes {
		if c.Status == StatusPending {
			pending = true
			break
		}
	}
	if pending {
		b.WriteString("\n")
		b.WriteString(buttonStyle.Render("[A]pprove All") + "  " + buttonStyle.Render("[R]eject All"))
	}
	return b.String()
}

func renderChangeCompact(change FileChange) string {
	icon := "~"
	switch change.Type {
	case ChangeCreate:
		icon = "+"
	case ChangeDelete:
		icon = "-"
	}
	statusIcon := "🟡"
	switch change.Status {
	case StatusApproved:
		statusIcon = "✅"
	case StatusRejected:
		statusIcon = "❌"
	}
	stats := fmt.Sprintf("+%d -%d", change.LinesAdded, change.LinesRemoved)
	return fmt.Sprintf("%s %s %s %s", statusIcon, filePathStyle.Render(change.Path), dimStyle.Render(icon), dimStyle.Render(stats))
}

func renderChangeFull(change FileChange, width int) string {
	var b strings.Builder
	b.WriteString(renderChangeCompact(change))
	b.WriteString("\n")
	if change.Expanded {
		diff := renderDiff(change.Diff)
		b.WriteString(diffBoxStyle.Width(max(0, width-6)).Render(diff))
	}
	return b.String()
}

func renderDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			rendered = append(rendered, "")
			continue
		}
		style := diffContextStyle
		switch line[0] {
		case '+':
			style = diffAddStyle
		case '-':
			style = diffRemoveStyle
		case '@':
			style = diffHeaderStyle
		}
		rendered = append(rendered, style.Render(line))
	}
	return strings.Join(rendered, "\n")
}

func renderMessageFooter(msg Message) string {
	duration := formatDuration(msg.Metadata.Duration)
	tokens := fmt.Sprintf("%d tokens", msg.Metadata.TokensUsed)
	return dimStyle.Render(fmt.Sprintf("⏱️  %s | 🪙 %s", duration, tokens))
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}
