package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lexcodex/relurpify/framework"
	runtimesvc "github.com/lexcodex/relurpify/internal/relurpish/runtime"
)

// Mode enumerates the primary panes.
type Mode int

const (
	ModeWizard Mode = iota
	ModeStatus
	ModeChat
)

type wizardStep int

const (
	wizardStepDetect wizardStep = iota
	wizardStepModel
	wizardStepAgents
	wizardStepTools
	wizardStepPermissions
	wizardStepSummary
)

// Options configure the Bubble Tea program.
type Options struct {
	InitialMode Mode
}

// Run launches the Bubble Tea UI.
func Run(ctx context.Context, rt *runtimesvc.Runtime, cfg runtimesvc.Config, opts Options) error {
	if opts.InitialMode > ModeChat {
		opts.InitialMode = ModeWizard
	}
	m := newModel(ctx, rt, cfg, opts)
	program := tea.NewProgram(m, tea.WithContext(ctx), tea.WithAltScreen())
	_, err := program.Run()
	return err
}

type model struct {
	ctx     context.Context
	runtime *runtimesvc.Runtime
	cfg     runtimesvc.Config
	mode    Mode

	width  int
	height int

	paletteOpen bool
	helpOpen    bool
	err         error

	wizard wizardState
	status statusState
	chat   chatState
}

type wizardState struct {
	loading        bool
	report         runtimesvc.EnvironmentReport
	step           wizardStep
	profile        runtimesvc.PermissionProfile
	agentOptions   []string
	agentIndex     int
	modelOptions   []string
	modelIndex     int
	selectedAgents map[string]bool
	toolOptions    []string
	toolIndex      int
	selectedTools  map[string]bool
	message        string
	saving         bool
	initialized    bool
}

type statusState struct {
	snapshot   runtimesvc.StatusSnapshot
	refreshing bool
}

type chatState struct {
	input    textinput.Model
	viewport viewport.Model
	log      []string
	sending  bool
}

func newModel(ctx context.Context, rt *runtimesvc.Runtime, cfg runtimesvc.Config, opts Options) model {
	chatInput := textinput.New()
	chatInput.Placeholder = "Enter instruction or /task <type> <instruction>"
	chatInput.CharLimit = 4000
	chatInput.Prompt = "❯ "
	chatInput.Blur()

	vp := viewport.New(0, 0)
	vp.SetContent("Welcome to relurpish chat. Type /task analysis <prompt> to run specific task types.")

	choices := []string{"coding", "planner", "react", "reflection", "manual", "expert"}
	agentIndex := 0
	for i, a := range choices {
		if a == cfg.AgentLabel() {
			agentIndex = i
			break
		}
	}
	toolNames := collectToolNames(rt.Tools.All())
	selectedTools := make(map[string]bool, len(toolNames))
	for _, name := range toolNames {
		selectedTools[name] = true
	}
	selectedAgents := map[string]bool{choices[agentIndex]: true}

	m := model{
		ctx:     ctx,
		runtime: rt,
		cfg:     cfg,
		mode:    opts.InitialMode,
		wizard: wizardState{
			loading:        true,
			step:           wizardStepDetect,
			profile:        runtimesvc.DefaultPermissionProfile(),
			agentOptions:   choices,
			agentIndex:     agentIndex,
			modelOptions:   []string{cfg.OllamaModel},
			modelIndex:     0,
			selectedAgents: selectedAgents,
			toolOptions:    toolNames,
			selectedTools:  selectedTools,
		},
		status: statusState{},
		chat: chatState{
			input:    chatInput,
			viewport: vp,
			log:      []string{time.Now().Format(time.Kitchen) + " · Session started"},
		},
	}
	if m.mode == ModeChat {
		m.chat.input.Focus()
	}
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		loadWizardCmd(m.cfg),
		refreshStatusCmd(m.runtime),
		statusTickerCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.chat.viewport.Width != msg.Width {
			m.chat.viewport.Width = max(10, msg.Width-2)
			m.chat.viewport.Height = max(5, msg.Height-5)
			m.refreshChatViewport()
		}
		return m, nil
	case tea.KeyMsg:
		if cmd := m.handleKey(msg); cmd != nil {
			return m, cmd
		}
		return m, nil
	case wizardReportMsg:
		m.wizard.loading = false
		m.applyWizardReport(msg.report)
		return m, nil
	case statusMsg:
		m.status.refreshing = false
		m.status.snapshot = msg.snapshot
		return m, nil
	case savedManifestMsg:
		m.wizard.saving = false
		if msg.err != nil {
			m.wizard.message = fmt.Sprintf("manifest error: %v", msg.err)
		} else {
			m.wizard.report.Manifest = msg.summary
			m.wizard.report.Config.Model = msg.selection.Model
			m.wizard.report.Config.Agents = msg.selection.Agents
			m.wizard.report.Config.AllowedTools = msg.selection.Tools
			if msg.selection.Model != "" {
				m.cfg.OllamaModel = msg.selection.Model
			}
			if len(msg.selection.Agents) > 0 {
				m.cfg.AgentName = msg.selection.Agents[0]
			}
			m.setInitialAgentSelection(msg.selection.Agents)
			m.setInitialToolSelection(msg.selection.Tools)
			m.wizard.message = fmt.Sprintf("manifest updated %s", msg.summary.UpdatedAt.Format(time.RFC822))
		}
		return m, nil
	case chatResultMsg:
		m.chat.sending = false
		if msg.err != nil {
			m.appendChatLine(fmt.Sprintf("⚠️  %v", msg.err))
		} else {
			m.appendChatLine(msg.content)
		}
		return m, nil
	case hitlMsg:
		m.appendChatLine(msg.content)
		return m, nil
	case tickMsg:
		return m, tea.Batch(refreshStatusCmd(m.runtime), statusTickerCmd())
	case errMsg:
		m.err = msg.err
		return m, nil
	default:
		var cmd tea.Cmd
		if m.mode == ModeChat {
			m.chat.input, cmd = m.chat.input.Update(msg)
		}
		return m, cmd
	}
}

func (m model) View() string {
	var body string
	switch m.mode {
	case ModeWizard:
		body = m.renderWizard()
	case ModeStatus:
		body = m.renderStatus()
	case ModeChat:
		body = m.renderChat()
	}
	if m.paletteOpen {
		body = lipgloss.JoinVertical(lipgloss.Left, body, paletteStyle.Render("Palette: w=wizard · s=status · c=chat · q=quit"))
	}
	if m.helpOpen {
		body = lipgloss.JoinVertical(lipgloss.Left, body, helpStyle.Render("? toggles this help. [:] opens palette. Chat supports /task, /hitl, /approve <id>, /deny <id> <reason>."))
	}
	if m.err != nil {
		body = lipgloss.JoinVertical(lipgloss.Left, body, errorStyle.Render(m.err.Error()))
	}
	return body
}

func (m *model) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyCtrlC:
		return tea.Quit
	}
	key := msg.String()
	switch key {
	case ":":
		m.paletteOpen = !m.paletteOpen
		return nil
	case "?":
		m.helpOpen = !m.helpOpen
		return nil
	case "q":
		if m.paletteOpen {
			m.paletteOpen = false
			return tea.Quit
		}
	}
	if m.paletteOpen {
		switch key {
		case "w":
			m.mode = ModeWizard
			m.paletteOpen = false
			m.chat.input.Blur()
		case "s":
			m.mode = ModeStatus
			m.paletteOpen = false
			m.chat.input.Blur()
		case "c":
			m.mode = ModeChat
			m.paletteOpen = false
			m.chat.input.Focus()
		}
		return nil
	}
	switch m.mode {
	case ModeWizard:
		return m.handleWizardKey(msg)
	case ModeStatus:
		if key == "r" {
			m.status.refreshing = true
			return refreshStatusCmd(m.runtime)
		}
	case ModeChat:
		switch key {
		case "enter":
			return m.submitChat()
		}
		var cmd tea.Cmd
		m.chat.input, cmd = m.chat.input.Update(msg)
		return cmd
	}
	return nil
}

func (m *model) handleWizardKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyUp:
		return m.handleWizardArrow(-1, msg.Type)
	case tea.KeyDown:
		return m.handleWizardArrow(1, msg.Type)
	case tea.KeyLeft:
		return m.handleWizardHorizontal(-1)
	case tea.KeyRight:
		return m.handleWizardHorizontal(1)
	}
	key := msg.String()
	switch key {
	case "n":
		return m.nextWizardStep()
	case "b":
		m.prevWizardStep()
		return nil
	}
	switch m.wizard.step {
	case wizardStepDetect:
		if key == "r" {
			m.wizard.loading = true
			return loadWizardCmd(m.cfg)
		}
	case wizardStepModel:
		switch key {
		case "<":
			m.stepModel(-1)
		case ">":
			m.stepModel(1)
		}
	case wizardStepAgents:
		switch key {
		case "[":
			m.stepAgent(-1)
		case "]":
			m.stepAgent(1)
		case " ":
			m.toggleAgentSelection()
		}
	case wizardStepTools:
		switch key {
		case ",":
			m.stepTool(-1)
		case ".":
			m.stepTool(1)
		case "t":
			m.toggleToolSelection()
		}
	case wizardStepPermissions:
		if key == "p" {
			m.toggleProfile()
		}
	case wizardStepSummary:
		if key == "s" {
			return m.saveWizardSelection()
		}
	}
	return nil
}

func (m *model) handleWizardArrow(delta int, key tea.KeyType) tea.Cmd {
	switch m.wizard.step {
	case wizardStepModel:
		if delta < 0 {
			m.stepModel(-1)
		} else {
			m.stepModel(1)
		}
	case wizardStepAgents:
		if delta < 0 {
			m.stepAgent(-1)
		} else {
			m.stepAgent(1)
		}
	case wizardStepTools:
		if delta < 0 {
			m.stepTool(-1)
		} else {
			m.stepTool(1)
		}
	}
	return nil
}

func (m *model) handleWizardHorizontal(delta int) tea.Cmd {
	switch m.wizard.step {
	case wizardStepModel:
		if delta < 0 {
			m.stepModel(-1)
		} else {
			m.stepModel(1)
		}
	case wizardStepTools:
		if delta < 0 {
			m.stepTool(-1)
		} else {
			m.stepTool(1)
		}
	}
	return nil
}

func (m *model) nextWizardStep() tea.Cmd {
	if !m.canAdvanceWizard() {
		return nil
	}
	if m.wizard.step < wizardStepSummary {
		m.wizard.step++
	}
	return nil
}

func (m *model) prevWizardStep() {
	if m.wizard.step > wizardStepDetect {
		m.wizard.step--
	}
}

func (m *model) canAdvanceWizard() bool {
	switch m.wizard.step {
	case wizardStepDetect:
		if m.wizard.loading {
			m.wizard.message = "Still detecting environment..."
			return false
		}
	case wizardStepAgents:
		if len(m.selectedAgentList()) == 0 {
			m.wizard.message = "Select at least one agent before continuing."
			return false
		}
	case wizardStepTools:
		if len(m.selectedToolList()) == 0 {
			m.wizard.message = "Select at least one tool before continuing."
			return false
		}
	case wizardStepSummary:
		return false
	}
	m.wizard.message = ""
	return true
}

func (m *model) saveWizardSelection() tea.Cmd {
	selection := m.currentSelection()
	if len(selection.Agents) == 0 {
		m.wizard.message = "Select at least one agent before saving."
		return nil
	}
	if len(selection.Tools) == 0 {
		m.wizard.message = "Select at least one tool before saving."
		return nil
	}
	m.wizard.saving = true
	m.wizard.message = "Saving configuration..."
	return saveManifestCmd(m.cfg, selection)
}

func (m *model) stepAgent(delta int) {
	if len(m.wizard.agentOptions) == 0 {
		return
	}
	m.wizard.agentIndex = (m.wizard.agentIndex + len(m.wizard.agentOptions) + delta) % len(m.wizard.agentOptions)
}

func (m *model) stepModel(delta int) {
	if len(m.wizard.modelOptions) == 0 {
		return
	}
	m.wizard.modelIndex = (m.wizard.modelIndex + len(m.wizard.modelOptions) + delta) % len(m.wizard.modelOptions)
}

func (m *model) toggleAgentSelection() {
	if len(m.wizard.agentOptions) == 0 {
		return
	}
	if m.wizard.selectedAgents == nil {
		m.wizard.selectedAgents = make(map[string]bool)
	}
	name := m.wizard.agentOptions[m.wizard.agentIndex]
	m.wizard.selectedAgents[name] = !m.wizard.selectedAgents[name]
}

func (m *model) stepTool(delta int) {
	if len(m.wizard.toolOptions) == 0 {
		return
	}
	m.wizard.toolIndex = (m.wizard.toolIndex + len(m.wizard.toolOptions) + delta) % len(m.wizard.toolOptions)
}

func (m *model) toggleToolSelection() {
	if len(m.wizard.toolOptions) == 0 {
		return
	}
	if m.wizard.selectedTools == nil {
		m.wizard.selectedTools = make(map[string]bool)
	}
	name := m.wizard.toolOptions[m.wizard.toolIndex]
	m.wizard.selectedTools[name] = !m.wizard.selectedTools[name]
}

func (m *model) toggleProfile() {
	if m.wizard.profile == runtimesvc.PermissionProfileReadOnly {
		m.wizard.profile = runtimesvc.PermissionProfileWorkspaceWrite
	} else {
		m.wizard.profile = runtimesvc.PermissionProfileReadOnly
	}
}

func (m *model) currentModel() string {
	if len(m.wizard.modelOptions) == 0 {
		return ""
	}
	if m.wizard.modelIndex < 0 || m.wizard.modelIndex >= len(m.wizard.modelOptions) {
		m.wizard.modelIndex = 0
	}
	return m.wizard.modelOptions[m.wizard.modelIndex]
}

func (m *model) selectedAgentList() []string {
	var agents []string
	seen := m.wizard.selectedAgents
	if seen == nil {
		return agents
	}
	for _, name := range m.wizard.agentOptions {
		if seen[name] {
			agents = append(agents, name)
		}
	}
	return agents
}

func (m *model) selectedToolList() []string {
	var tools []string
	seen := m.wizard.selectedTools
	if seen == nil {
		return tools
	}
	for _, name := range m.wizard.toolOptions {
		if seen[name] {
			tools = append(tools, name)
		}
	}
	return tools
}

func (m *model) currentSelection() runtimesvc.WizardSelection {
	return runtimesvc.WizardSelection{
		Model:   m.currentModel(),
		Agents:  m.selectedAgentList(),
		Profile: m.wizard.profile,
		Tools:   m.selectedToolList(),
	}
}

func (m *model) submitChat() tea.Cmd {
	if m.chat.sending {
		return nil
	}
	text := strings.TrimSpace(m.chat.input.Value())
	if text == "" {
		return nil
	}
	m.chat.input.SetValue("")
	m.appendChatLine(fmt.Sprintf("🧑  %s", text))
	if strings.HasPrefix(text, "/") {
		return m.handleSlash(text)
	}
	m.chat.sending = true
	return sendChatCmd(m.runtime, text, framework.TaskTypeCodeModification)
}

func (m *model) handleSlash(text string) tea.Cmd {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil
	}
	switch fields[0] {
	case "/task":
		if len(fields) < 3 {
			m.appendChatLine("⚠️  usage: /task <type> <instruction>")
			return nil
		}
		taskType := parseTaskType(fields[1])
		instruction := strings.Join(fields[2:], " ")
		m.chat.sending = true
		return sendChatCmd(m.runtime, instruction, taskType)
	case "/hitl":
		return summarizeHITLCmd(m.runtime)
	case "/approve":
		if len(fields) < 2 {
			m.appendChatLine("⚠️  usage: /approve <request_id>")
			return nil
		}
		return approveHitlCmd(m.runtime, fields[1])
	case "/deny":
		if len(fields) < 3 {
			m.appendChatLine("⚠️  usage: /deny <request_id> <reason>")
			return nil
		}
		return denyHitlCmd(m.runtime, fields[1], strings.Join(fields[2:], " "))
	default:
		m.appendChatLine(fmt.Sprintf("⚠️  unknown command %s", fields[0]))
		return nil
	}
}

func (m *model) appendChatLine(content string) {
	timestamp := time.Now().Format(time.Kitchen)
	m.chat.log = append(m.chat.log, fmt.Sprintf("%s · %s", timestamp, content))
	m.refreshChatViewport()
}

func (m *model) refreshChatViewport() {
	if m.chat.viewport.Width == 0 {
		return
	}
	m.chat.viewport.SetContent(strings.Join(m.chat.log, "\n"))
}

func (m model) renderToolList() string {
	if len(m.wizard.toolOptions) == 0 {
		return "  (no registered tools)\n"
	}
	var b strings.Builder
	start := max(0, m.wizard.toolIndex-3)
	end := min(len(m.wizard.toolOptions), start+6)
	for i := start; i < end; i++ {
		cursor := " "
		if i == m.wizard.toolIndex {
			cursor = ">"
		}
		name := m.wizard.toolOptions[i]
		selected := " "
		if m.wizard.selectedTools != nil && m.wizard.selectedTools[name] {
			selected = "x"
		}
		b.WriteString(fmt.Sprintf(" %s [%s] %s\n", cursor, selected, name))
	}
	if end < len(m.wizard.toolOptions) {
		b.WriteString(fmt.Sprintf("   ... (+%d more)\n", len(m.wizard.toolOptions)-end))
	}
	return b.String()
}

func (m *model) applyWizardReport(report runtimesvc.EnvironmentReport) {
	currentModel := m.currentModel()
	m.wizard.report = report
	modelCandidates := append([]string{}, report.Ollama.Models...)
	if report.Config.Model != "" {
		modelCandidates = append(modelCandidates, report.Config.Model)
	}
	if currentModel != "" {
		modelCandidates = append(modelCandidates, currentModel)
	}
	if m.cfg.OllamaModel != "" {
		modelCandidates = append(modelCandidates, m.cfg.OllamaModel)
	}
	models := uniqueStrings(modelCandidates)
	if len(models) == 0 {
		models = []string{m.cfg.OllamaModel}
	}
	m.wizard.modelOptions = models
	targetModel := currentModel
	if targetModel == "" {
		if report.Config.Model != "" {
			targetModel = report.Config.Model
		} else if m.cfg.OllamaModel != "" {
			targetModel = m.cfg.OllamaModel
		}
	}
	if targetModel == "" && len(models) > 0 {
		targetModel = models[0]
	}
	m.wizard.modelIndex = indexOf(models, targetModel)
	if m.wizard.modelIndex < 0 {
		m.wizard.modelIndex = 0
	}
	if !m.wizard.initialized {
		if report.Config.PermissionProfile != "" {
			m.wizard.profile = report.Config.PermissionProfile
		}
		m.setInitialAgentSelection(report.Config.Agents)
		m.setInitialToolSelection(report.Config.AllowedTools)
		m.wizard.initialized = true
	}
}

func (m *model) setInitialAgentSelection(selected []string) {
	m.wizard.selectedAgents = make(map[string]bool)
	if len(selected) == 0 && len(m.wizard.agentOptions) > 0 {
		selected = []string{m.wizard.agentOptions[m.wizard.agentIndex]}
	}
	for _, name := range selected {
		for _, option := range m.wizard.agentOptions {
			if option == name {
				m.wizard.selectedAgents[option] = true
			}
		}
	}
	if len(m.wizard.selectedAgents) == 0 && len(m.wizard.agentOptions) > 0 {
		m.wizard.selectedAgents[m.wizard.agentOptions[m.wizard.agentIndex]] = true
	}
}

func (m *model) setInitialToolSelection(selected []string) {
	m.wizard.selectedTools = make(map[string]bool)
	if len(selected) == 0 {
		for _, name := range m.wizard.toolOptions {
			m.wizard.selectedTools[name] = true
		}
		return
	}
	for _, tool := range selected {
		for _, option := range m.wizard.toolOptions {
			if option == tool {
				m.wizard.selectedTools[option] = true
			}
		}
	}
	if len(m.wizard.selectedTools) == 0 {
		for _, name := range m.wizard.toolOptions {
			m.wizard.selectedTools[name] = true
		}
	}
}

func (m model) renderWizard() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Wizard"))
	b.WriteString("\n")
	switch m.wizard.step {
	case wizardStepDetect:
		b.WriteString(m.renderWizardDetect())
	case wizardStepModel:
		b.WriteString(m.renderWizardModel())
	case wizardStepAgents:
		b.WriteString(m.renderWizardAgents())
	case wizardStepTools:
		b.WriteString(m.renderWizardTools())
	case wizardStepPermissions:
		b.WriteString(m.renderWizardPermissions())
	case wizardStepSummary:
		b.WriteString(m.renderWizardSummary())
	}
	if m.wizard.message != "" {
		b.WriteString("\n")
		b.WriteString(infoStyle.Render(m.wizard.message))
	}
	return frameStyle.Render(b.String())
}

func (m model) renderWizardDetect() string {
	var b strings.Builder
	r := m.wizard.report
	if m.wizard.loading {
		b.WriteString("Detecting environment…\n\n")
	}
	b.WriteString(fmt.Sprintf("Workspace: %s\n", r.Workspace))
	b.WriteString("Sandbox status:\n")
	b.WriteString("  " + summarizeSandboxBinary("runsc", r.Sandbox.Runsc) + "\n")
	b.WriteString("  " + summarizeSandboxBinary("docker", r.Sandbox.Docker) + "\n")
	b.WriteString("  " + summarizeSandboxBinary("containerd", r.Sandbox.Containerd) + "\n")
	b.WriteString(fmt.Sprintf("Ollama: %s\n", describeOllama(r.Ollama)))
	if r.Manifest.Exists {
		b.WriteString(fmt.Sprintf("Existing manifest: %s (permissions:%d network:%d)\n", r.Manifest.Path, r.Manifest.Permissions, r.Manifest.Network))
	} else {
		b.WriteString(fmt.Sprintf("Manifest missing at %s\n", r.Manifest.Path))
	}
	b.WriteString("\nPress n to continue or r to rerun detection.")
	return b.String()
}

func (m model) renderWizardModel() string {
	var b strings.Builder
	b.WriteString("Select Ollama model (< and > to change)\n\n")
	if len(m.wizard.modelOptions) == 0 {
		b.WriteString("No models detected. You can still proceed with the default.\n")
	} else {
		for i, model := range m.wizard.modelOptions {
			cursor := " "
			if i == m.wizard.modelIndex {
				cursor = ">"
			}
			b.WriteString(fmt.Sprintf(" %s %s\n", cursor, model))
		}
	}
	b.WriteString("\nPress n to continue or b to go back.")
	return b.String()
}

func (m model) renderWizardAgents() string {
	var b strings.Builder
	b.WriteString("Select agents ([ / ] to move, space to toggle)\n\n")
	for i, name := range m.wizard.agentOptions {
		cursor := " "
		if i == m.wizard.agentIndex {
			cursor = ">"
		}
		selected := " "
		if m.wizard.selectedAgents != nil && m.wizard.selectedAgents[name] {
			selected = "x"
		}
		b.WriteString(fmt.Sprintf(" %s [%s] %s\n", cursor, selected, name))
	}
	b.WriteString("\nPress n to continue or b to go back.")
	return b.String()
}

func (m model) renderWizardTools() string {
	var b strings.Builder
	b.WriteString("Select tools (, / . to move, t to toggle)\n\n")
	b.WriteString(m.renderToolList())
	b.WriteString("\nPress n to continue or b to go back.")
	return b.String()
}

func (m model) renderWizardPermissions() string {
	var b strings.Builder
	b.WriteString("Choose permission profile (press p to toggle)\n\n")
	b.WriteString(fmt.Sprintf("Current profile: %s\n", m.wizard.profile))
	b.WriteString(m.wizard.profile.Description() + "\n")
	b.WriteString("\nPress n to continue or b to go back.")
	return b.String()
}

func (m model) renderWizardSummary() string {
	var b strings.Builder
	selection := m.currentSelection()
	b.WriteString("Review selections before saving\n\n")
	b.WriteString(fmt.Sprintf("Model: %s\n", valueOrFallback(selection.Model, "not set")))
	if len(selection.Agents) > 0 {
		b.WriteString(fmt.Sprintf("Agents: %s\n", strings.Join(selection.Agents, ", ")))
	} else {
		b.WriteString("Agents: none selected\n")
	}
	if len(selection.Tools) > 0 {
		b.WriteString(fmt.Sprintf("Tools: %s\n", strings.Join(selection.Tools, ", ")))
	} else {
		b.WriteString("Tools: none selected\n")
	}
	b.WriteString(fmt.Sprintf("Permissions: %s (%s)\n", selection.Profile, selection.Profile.Description()))
	b.WriteString("\nPress s to save, b to revise previous steps.")
	return b.String()
}

func (m model) renderStatus() string {
	snap := m.status.snapshot
	var b strings.Builder
	b.WriteString(titleStyle.Render("Status"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Workspace: %s\n", snap.Environment.Workspace))
	b.WriteString(fmt.Sprintf("Sandbox: %s\n", describeSandbox(snap.Environment.Sandbox)))
	b.WriteString(fmt.Sprintf("Ollama: %s\n", describeOllama(snap.Environment.Ollama)))
	if snap.Environment.Config.Model != "" {
		b.WriteString(fmt.Sprintf("Configured model: %s\n", snap.Environment.Config.Model))
	}
	if len(snap.Environment.Config.Agents) > 0 {
		b.WriteString(fmt.Sprintf("Configured agents: %s\n", strings.Join(snap.Environment.Config.Agents, ", ")))
	}
	if len(snap.Environment.Config.AllowedTools) > 0 {
		toolSummary := append([]string{}, snap.Environment.Config.AllowedTools...)
		if len(toolSummary) > 6 {
			toolSummary = append(toolSummary[:6], fmt.Sprintf("...(+%d)", len(snap.Environment.Config.AllowedTools)-6))
		}
		b.WriteString(fmt.Sprintf("Allowed tools: %s\n", strings.Join(toolSummary, ", ")))
	}
	if snap.RegistrationError != "" {
		b.WriteString(errorStyle.Render("Permissions: "+snap.RegistrationError) + "\n")
	}
	b.WriteString(fmt.Sprintf("Server: %v\n", snap.ServerActive))
	if len(snap.PendingHITL) > 0 {
		b.WriteString("Pending HITL approvals:\n")
		for _, req := range snap.PendingHITL {
			b.WriteString(fmt.Sprintf(" • %s %s (%s)\n", req.ID, req.Permission.Action, req.Justification))
		}
	}
	b.WriteString("Press r to refresh\n")
	return frameStyle.Render(b.String())
}

func (m model) renderChat() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Chat"))
	b.WriteString("\n")
	b.WriteString(m.chat.viewport.View())
	b.WriteString("\n")
	b.WriteString(m.chat.input.View())
	return frameStyle.Render(b.String())
}

// --- Commands ---

type wizardReportMsg struct{ report runtimesvc.EnvironmentReport }
type statusMsg struct{ snapshot runtimesvc.StatusSnapshot }
type savedManifestMsg struct {
	summary   runtimesvc.ManifestSummary
	selection runtimesvc.WizardSelection
	err       error
}
type chatResultMsg struct {
	content string
	err     error
}
type hitlMsg struct{ content string }
type tickMsg struct{}
type errMsg struct{ err error }

func loadWizardCmd(cfg runtimesvc.Config) tea.Cmd {
	return func() tea.Msg {
		report := runtimesvc.ProbeEnvironment(context.Background(), cfg)
		return wizardReportMsg{report: report}
	}
}

func refreshStatusCmd(rt *runtimesvc.Runtime) tea.Cmd {
	return func() tea.Msg {
		snapshot := rt.Status(context.Background())
		return statusMsg{snapshot: snapshot}
	}
}

func statusTickerCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func saveManifestCmd(cfg runtimesvc.Config, selection runtimesvc.WizardSelection) tea.Cmd {
	return func() tea.Msg {
		summary, err := runtimesvc.SaveManifest(context.Background(), cfg, selection)
		return savedManifestMsg{summary: summary, selection: selection, err: err}
	}
}

func sendChatCmd(rt *runtimesvc.Runtime, instruction string, taskType framework.TaskType) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		result, err := rt.ExecuteInstruction(ctx, instruction, taskType, map[string]any{"source": "relurpish"})
		if err != nil {
			return chatResultMsg{err: enrichChatError(err, rt.Config.OllamaModel)}
		}
		if result == nil {
			return chatResultMsg{content: "✅ task completed"}
		}
		summary := fmt.Sprintf("✅ %s · success=%t", result.NodeID, result.Success)
		if len(result.Data) > 0 {
			summary += fmt.Sprintf(" data=%v", result.Data)
		}
		if result.Error != nil {
			if enriched := enrichChatError(result.Error, rt.Config.OllamaModel); enriched != nil {
				summary += fmt.Sprintf(" error=%v", enriched)
			} else {
				summary += fmt.Sprintf(" error=%v", result.Error)
			}
		}
		return chatResultMsg{content: summary}
	}
}

func enrichChatError(err error, model string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "ollama error") {
		hint := "Verify your Ollama server is reachable"
		if strings.TrimSpace(model) != "" {
			hint += fmt.Sprintf(" and the `%s` model is installed (`ollama pull %s`).", model, model)
		} else {
			hint += " and the requested model is installed."
		}
		msg = fmt.Sprintf("%s. %s", msg, hint)
	}
	return fmt.Errorf("%s", msg)
}

func summarizeHITLCmd(rt *runtimesvc.Runtime) tea.Cmd {
	return func() tea.Msg {
		pending := rt.PendingHITL()
		if len(pending) == 0 {
			return hitlMsg{content: "No pending HITL approvals."}
		}
		var b strings.Builder
		b.WriteString("Pending approvals:\n")
		for _, req := range pending {
			b.WriteString(fmt.Sprintf(" - %s %s (%s)\n", req.ID, req.Permission.Action, req.Justification))
		}
		return hitlMsg{content: b.String()}
	}
}

func approveHitlCmd(rt *runtimesvc.Runtime, id string) tea.Cmd {
	return func() tea.Msg {
		err := rt.ApproveHITL(id, "relurpish", framework.GrantScopeSession, time.Hour)
		if err != nil {
			return hitlMsg{content: fmt.Sprintf("⚠️  approve failed: %v", err)}
		}
		return hitlMsg{content: fmt.Sprintf("Approved %s", id)}
	}
}

func denyHitlCmd(rt *runtimesvc.Runtime, id, reason string) tea.Cmd {
	return func() tea.Msg {
		if err := rt.DenyHITL(id, reason); err != nil {
			return hitlMsg{content: fmt.Sprintf("⚠️  deny failed: %v", err)}
		}
		return hitlMsg{content: fmt.Sprintf("Denied %s", id)}
	}
}

func describeSandbox(s runtimesvc.SandboxReport) string {
	var parts []string
	parts = append(parts, summarizeSandboxBinary("runsc", s.Runsc))
	parts = append(parts, summarizeSandboxBinary("docker", s.Docker))
	parts = append(parts, summarizeSandboxBinary("containerd", s.Containerd))
	desc := strings.Join(parts, " | ")
	if len(s.Errors) > 0 {
		desc = desc + " · " + strings.Join(s.Errors, "; ")
	}
	return desc
}

func summarizeSandboxBinary(label string, binary runtimesvc.SandboxBinary) string {
	if binary.Name == "" {
		return fmt.Sprintf("%s: not checked", label)
	}
	if binary.Error != "" {
		return fmt.Sprintf("%s: %s", label, binary.Error)
	}
	info := fmt.Sprintf("%s ok", label)
	if binary.Version != "" {
		info += fmt.Sprintf(" (%s)", binary.Version)
	}
	if binary.SupportsRunsc && label != "runsc" {
		info += " [runsc]"
	}
	return info
}

func describeOllama(o runtimesvc.OllamaReport) string {
	if o.Error != "" {
		return o.Error
	}
	if !o.Healthy {
		return "unreachable"
	}
	return fmt.Sprintf("%s (models: %s)", o.Endpoint, strings.Join(o.Models, ", "))
}

func parseTaskType(raw string) framework.TaskType {
	switch strings.ToLower(raw) {
	case "plan":
		return framework.TaskTypePlanning
	case "generate", "code":
		return framework.TaskTypeCodeGeneration
	case "review":
		return framework.TaskTypeReview
	case "analysis":
		return framework.TaskTypeAnalysis
	default:
		return framework.TaskTypeCodeModification
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func valueOrFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	var res []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		res = append(res, v)
	}
	return res
}

func indexOf(values []string, target string) int {
	for i, v := range values {
		if v == target {
			return i
		}
	}
	return -1
}

func collectToolNames(tools []framework.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		names = append(names, tool.Name())
	}
	sort.Strings(names)
	return names
}

var (
	frameStyle   = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(1, 2)
	titleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	infoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("79"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("204")).Bold(true)
	paletteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("150")).Padding(0, 1)
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Padding(0, 1)
)
