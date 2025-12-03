package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorPrimary   = lipgloss.Color("39")
	colorSecondary = lipgloss.Color("86")
	colorSuccess   = lipgloss.Color("42")
	colorWarning   = lipgloss.Color("220")
	colorError     = lipgloss.Color("196")
	colorDim       = lipgloss.Color("241")

	messageBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(1, 2).
			MarginBottom(1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	sectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorSecondary)

	textStyle = lipgloss.NewStyle()

	dimStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	detailStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Italic(true)

	completedStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	inProgressStyle = lipgloss.NewStyle().
			Foreground(colorWarning)

	pendingStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	filePathStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSecondary)

	diffBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)

	diffAddStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	diffRemoveStyle = lipgloss.NewStyle().
			Foreground(colorError)

	diffHeaderStyle = lipgloss.NewStyle().
			Foreground(colorSecondary)

	diffContextStyle = lipgloss.NewStyle()

	statusStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("255")).
			Padding(0, 1)

	promptBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Padding(0, 1)

	buttonStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)

	welcomeStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Italic(true).
			Align(lipgloss.Center)
)
