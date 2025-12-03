package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// CommandHandler mutates model state for /commands in the prompt bar.
type CommandHandler func(Model, []string) (Model, tea.Cmd)

// Command describes a slash command entry.
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
	Handler     CommandHandler
}

var commandRegistry = map[string]Command{}

func init() {
	registerCommand(Command{
		Name:        "help",
		Aliases:     []string{"h", "?"},
		Description: "Show available commands",
		Usage:       "/help [command]",
		Handler:     handleHelp,
	})
	registerCommand(Command{
		Name:        "add",
		Aliases:     []string{"a"},
		Description: "Add file or directory to context",
		Usage:       "/add <path>",
		Handler:     handleAdd,
	})
	registerCommand(Command{
		Name:        "remove",
		Aliases:     []string{"rm"},
		Description: "Remove file from context",
		Usage:       "/remove <path>",
		Handler:     handleRemove,
	})
	registerCommand(Command{
		Name:        "context",
		Aliases:     []string{"ctx", "c"},
		Description: "Show current context",
		Usage:       "/context",
		Handler:     handleContext,
	})
	registerCommand(Command{
		Name:        "clear",
		Aliases:     []string{"cls"},
		Description: "Clear chat history",
		Usage:       "/clear",
		Handler:     handleClear,
	})
	registerCommand(Command{
		Name:        "approve",
		Aliases:     []string{"ap"},
		Description: "Approve pending changes",
		Usage:       "/approve",
		Handler:     handleApprove,
	})
	registerCommand(Command{
		Name:        "reject",
		Aliases:     []string{"rej"},
		Description: "Reject pending changes",
		Usage:       "/reject",
		Handler:     handleReject,
	})
	registerCommand(Command{
		Name:        "hitl",
		Aliases:     []string{"hi"},
		Description: "Show pending HITL approvals",
		Usage:       "/hitl",
		Handler:     handleHITL,
	})
}

func registerCommand(cmd Command) {
	commandRegistry[cmd.Name] = cmd
}

// parseCommand splits the slash-prefixed input into command + args.
func parseCommand(input string) (string, []string) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return "", nil
	}
	if !strings.HasPrefix(parts[0], "/") {
		return "", nil
	}
	name := strings.TrimPrefix(parts[0], "/")
	return name, parts[1:]
}

// handleCommand finds the registered command (with alias fallback).
func handleCommand(m Model, name string, args []string) (Model, tea.Cmd) {
	if name == "" {
		return m, nil
	}
	cmd, ok := commandRegistry[name]
	if !ok {
		for _, registered := range commandRegistry {
			for _, alias := range registered.Aliases {
				if alias == name {
					cmd = registered
					ok = true
					break
				}
			}
			if ok {
				break
			}
		}
	}
	if !ok {
		return m.addSystemMessage(fmt.Sprintf("Unknown command: %s", name)), nil
	}
	return cmd.Handler(m, args)
}

func handleHelp(m Model, args []string) (Model, tea.Cmd) {
	if len(args) > 0 {
		name := args[0]
		if cmd, ok := commandRegistry[name]; ok {
			text := fmt.Sprintf("%s - %s\nUsage: %s", cmd.Name, cmd.Description, cmd.Usage)
			return m.addSystemMessage(text), nil
		}
	}
	names := make([]string, 0, len(commandRegistry))
	for name := range commandRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("Available commands:\n\n")
	for _, name := range names {
		cmd := commandRegistry[name]
		b.WriteString(fmt.Sprintf("  %s - %s\n", cmd.Usage, cmd.Description))
	}
	return m.addSystemMessage(b.String()), nil
}

func handleAdd(m Model, args []string) (Model, tea.Cmd) {
	if len(args) == 0 {
		return m.addSystemMessage("Usage: /add <path>"), nil
	}
	path := args[0]
	if err := m.context.AddFile(path); err != nil {
		return m.addSystemMessage(fmt.Sprintf("Context error: %v", err)), nil
	}
	return m.addSystemMessage(fmt.Sprintf("Added to context: %s", path)), nil
}

func handleRemove(m Model, args []string) (Model, tea.Cmd) {
	if len(args) == 0 {
		return m.addSystemMessage("Usage: /remove <path>"), nil
	}
	path := args[0]
	m.context.RemoveFile(path)
	return m.addSystemMessage(fmt.Sprintf("Removed from context: %s", path)), nil
}

func handleContext(m Model, args []string) (Model, tea.Cmd) {
	files := m.context.List()
	if len(files) == 0 {
		return m.addSystemMessage("Context is empty"), nil
	}
	var b strings.Builder
	b.WriteString("Files in context:\n\n")
	for _, f := range files {
		b.WriteString(fmt.Sprintf("  • %s\n", f))
	}
	b.WriteString(fmt.Sprintf("\nTokens: %d / %d", m.context.UsedTokens, m.context.MaxTokens))
	return m.addSystemMessage(b.String()), nil
}

func handleClear(m Model, args []string) (Model, tea.Cmd) {
	m.messages = nil
	return m.addSystemMessage("History cleared"), nil
}

func handleApprove(m Model, args []string) (Model, tea.Cmd) {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.Role != RoleAgent {
			continue
		}
		for j := range msg.Content.Changes {
			if msg.Content.Changes[j].Status == StatusPending {
				msg.Content.Changes[j].Status = StatusApproved
				return m.addSystemMessage(fmt.Sprintf("Approved %s", msg.Content.Changes[j].Path)), nil
			}
		}
	}
	return m.addSystemMessage("No pending changes"), nil
}

func handleReject(m Model, args []string) (Model, tea.Cmd) {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.Role != RoleAgent {
			continue
		}
		for j := range msg.Content.Changes {
			if msg.Content.Changes[j].Status == StatusPending {
				msg.Content.Changes[j].Status = StatusRejected
				return m.addSystemMessage(fmt.Sprintf("Rejected %s", msg.Content.Changes[j].Path)), nil
			}
		}
	}
	return m.addSystemMessage("No pending changes"), nil
}

func handleHITL(m Model, args []string) (Model, tea.Cmd) {
	return m, summarizePendingHITL(m.runtime)
}
