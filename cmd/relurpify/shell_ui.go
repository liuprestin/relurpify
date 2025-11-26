package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/cmd/internal/cliutils"
	"github.com/lexcodex/relurpify/cmd/internal/setup"
	"github.com/lexcodex/relurpify/cmd/internal/toolchain"
	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/llm"
	"github.com/lexcodex/relurpify/server"
)

type shellPhase int

const (
	phaseWizard shellPhase = iota
	phaseShell
)

type shellPane int

const (
	paneSummary shellPane = iota
	paneHistory
	paneServices
	paneTimeline
	paneLogs
	paneJobs
	panePalette
)

type jobStatus string

const (
	jobQueued  jobStatus = "queued"
	jobRunning jobStatus = "running"
	jobSuccess jobStatus = "success"
	jobFailed  jobStatus = "failed"
)

type jobEntry struct {
	ID        string
	Label     string
	Type      string
	Status    jobStatus
	Started   time.Time
	Finished  time.Time
	Err       error
	Result    *framework.Result
	State     *framework.Context
	Phases    map[string]jobStatus
	PhaseInfo map[string]string
}

type timelineEntry struct {
	JobID     string
	Timestamp time.Time
	Label     string
	Content   string
}

type logEntry struct {
	Timestamp time.Time
	Source    string
	Line      string
}

type detectionState struct {
	sync.Mutex
	Running  bool
	Progress int
	Total    int
	Status   map[string]string
	Message  string
}

type wizardStep int

const (
	wizardStepWorkspace wizardStep = iota
	wizardStepModel
	wizardStepLanguages
	wizardStepTooling
	wizardStepComplete
)

type wizardModel struct {
	step wizardStep
}

type shellModel struct {
	cmd               *cobra.Command
	configPath        string
	cfg               *setup.Config
	tc                *toolchain.Manager
	eventCh           <-chan toolchain.Event
	logStream         chan tea.Msg
	timelineCh        chan tea.Msg
	detectCh          chan tea.Msg
	phase             shellPhase
	wizard            *wizardModel
	width             int
	height            int
	activePane        shellPane
	textInput         textinput.Model
	modelsList        list.Model
	lspsList          list.Model
	historyPort       viewport.Model
	logPort           viewport.Model
	timelinePort      viewport.Model
	servicesTbl       table.Model
	jobsTbl           table.Model
	spinner           spinner.Model
	progress          progress.Model
	history           []string
	logs              []logEntry
	timeline          []timelineEntry
	jobs              map[string]*jobEntry
	jobOrder          []string
	jobCounter        int
	statusLine        string
	detection         detectionState
	selectedLanguages map[string]bool
}

func newShellModel(cmd *cobra.Command, configPath string, cfg *setup.Config, tc *toolchain.Manager, tcEvents <-chan toolchain.Event) *shellModel {
	txt := textinput.New()
	txt.Placeholder = "Type a command (task, write, analyze, apply, detect, wizard, quit)"
	txt.Focus()
	txt.CharLimit = 512

	spin := spinner.New()
	spin.Spinner = spinner.Dot

	modelsList := list.New([]list.Item{}, list.NewDefaultDelegate(), 20, 10)
	modelsList.Title = "Models"

	lspsList := list.New([]list.Item{}, list.NewDefaultDelegate(), 20, 10)
	lspsList.Title = "LSP Servers"

	historyPort := viewport.New(40, 10)
	logPort := viewport.New(40, 10)
	timelinePort := viewport.New(40, 10)

	servicesTbl := table.New(table.WithColumns([]table.Column{
		{Title: "Lang", Width: 8},
		{Title: "PID", Width: 8},
		{Title: "Command", Width: 20},
		{Title: "Status", Width: 10},
	}))
	servicesTbl.SetRows([]table.Row{})

	jobsTbl := table.New(table.WithColumns([]table.Column{
		{Title: "ID", Width: 8},
		{Title: "Type", Width: 10},
		{Title: "Status", Width: 10},
		{Title: "Duration", Width: 12},
		{Title: "Label", Width: 32},
	}))

	wizard := &wizardModel{step: wizardStepWorkspace}

	progress := progress.New(progress.WithDefaultGradient())
	selected := initialLanguageSelection(cfg)

	model := &shellModel{
		cmd:          cmd,
		configPath:   configPath,
		cfg:          cfg,
		tc:           tc,
		eventCh:      tcEvents,
		phase:        phaseWizard,
		wizard:       wizard,
		activePane:   paneSummary,
		textInput:    txt,
		modelsList:   modelsList,
		lspsList:     lspsList,
		historyPort:  historyPort,
		logPort:      logPort,
		timelinePort: timelinePort,
		servicesTbl:  servicesTbl,
		jobsTbl:      jobsTbl,
		spinner:      spin,
		progress:     progress,
		history:      []string{},
		logs:         []logEntry{},
		timeline:     []timelineEntry{},
		jobs:         map[string]*jobEntry{},
		detection: detectionState{
			Status: map[string]string{},
		},
		logStream:         make(chan tea.Msg, 32),
		timelineCh:        make(chan tea.Msg, 32),
		detectCh:          make(chan tea.Msg, 32),
		selectedLanguages: selected,
	}
	model.refreshLists()
	return model
}

func (m *shellModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.EnterAltScreen,
		m.spinner.Tick,
		m.listenToolchainEvents(),
		m.listenLogChannel(),
		m.listenTimelineChannel(),
		m.listenDetectionChannel(),
	}
	return tea.Batch(cmds...)
}

type toolchainEventMsg struct {
	Event toolchain.Event
}

type logLineMsg struct {
	Entry logEntry
}

type timelineMsg struct {
	Entry timelineEntry
}

type detectionProgressMsg struct {
	Language string
	Index    int
	Total    int
	Status   string
}

type detectionCompleteMsg struct {
	Config *setup.Config
	Err    error
}

type jobStartedMsg struct {
	Job *jobEntry
}

type jobUpdateMsg struct {
	JobID  string
	Phase  string
	Status jobStatus
	Info   string
}

type jobFinishedMsg struct {
	JobID  string
	Result *framework.Result
	State  *framework.Context
	Err    error
}

func (m *shellModel) listenToolchainEvents() tea.Cmd {
	if m.eventCh == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-m.eventCh
		if !ok {
			return nil
		}
		return toolchainEventMsg{Event: evt}
	}
}

func (m *shellModel) listenLogChannel() tea.Cmd {
	if m.logStream == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-m.logStream
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *shellModel) listenTimelineChannel() tea.Cmd {
	if m.timelineCh == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-m.timelineCh
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *shellModel) listenDetectionChannel() tea.Cmd {
	if m.detectCh == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-m.detectCh
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *shellModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.phase == phaseWizard {
			handled := true
			switch msg.String() {
			case "ctrl+c", "esc":
				return m, tea.Quit
			case "shift+tab", "left", "h":
				m.rewindWizard()
			case " ":
				if m.wizard.step == wizardStepLanguages {
					m.toggleSelectedLanguage()
				} else if m.wizard.step == wizardStepTooling {
					m.toggleToolCalling()
				}
			case "enter":
				if cmd := m.advanceWizard(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			default:
				handled = false
			}
			if handled {
				return m, tea.Batch(cmds...)
			}
		}
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "tab":
			m.activePane = (m.activePane + 1) % panePalette
		case "shift+tab":
			if m.activePane == 0 {
				m.activePane = panePalette
			} else {
				m.activePane--
			}
		case "enter":
			cmds = append(cmds, m.handleSubmittedCommand())
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.historyPort.Width = msg.Width / 2
		m.logPort.Width = msg.Width / 2
		m.timelinePort.Width = msg.Width / 2
	case toolchainEventMsg:
		m.consumeToolchainEvent(msg.Event)
		cmds = append(cmds, m.listenToolchainEvents())
	case logLineMsg:
		m.appendLog(msg.Entry)
		cmds = append(cmds, m.listenLogChannel())
	case timelineMsg:
		m.appendTimeline(msg.Entry)
		cmds = append(cmds, m.listenTimelineChannel())
	case detectionProgressMsg:
		m.detection.Lock()
		m.detection.Running = true
		m.detection.Progress = msg.Index + 1
		m.detection.Total = msg.Total
		m.detection.Status[msg.Language] = msg.Status
		m.detection.Unlock()
		cmds = append(cmds, m.listenDetectionChannel())
	case detectionCompleteMsg:
		if msg.Err != nil {
			m.statusLine = fmt.Sprintf("Detection failed: %v", msg.Err)
		} else if msg.Config != nil {
			m.cfg = msg.Config
			m.selectedLanguages = initialLanguageSelection(msg.Config)
			m.refreshLists()
			m.statusLine = "Detection complete"
		}
		m.detection.Lock()
		m.detection.Running = false
		m.detection.Unlock()
		cmds = append(cmds, m.listenDetectionChannel())
	case jobStartedMsg:
		if job, ok := m.jobs[msg.Job.ID]; ok {
			job.Status = jobRunning
			job.Started = time.Now()
		}
		m.refreshJobsTable()
	case jobUpdateMsg:
		if job, ok := m.jobs[msg.JobID]; ok {
			if job.Phases == nil {
				job.Phases = map[string]jobStatus{}
			}
			job.Phases[msg.Phase] = msg.Status
			if job.PhaseInfo == nil {
				job.PhaseInfo = map[string]string{}
			}
			job.PhaseInfo[msg.Phase] = msg.Info
		}
	case jobFinishedMsg:
		if job, ok := m.jobs[msg.JobID]; ok {
			job.Status = jobSuccess
			job.Result = msg.Result
			job.State = msg.State
			job.Finished = time.Now()
			if msg.Err != nil {
				job.Status = jobFailed
				job.Err = msg.Err
			}
		}
		m.refreshJobsTable()
	default:
	}

	if m.phase == phaseWizard {
		var cmd tea.Cmd
		switch m.wizard.step {
		case wizardStepModel:
			m.modelsList, cmd = m.modelsList.Update(msg)
		case wizardStepLanguages:
			m.lspsList, cmd = m.lspsList.Update(msg)
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	} else {
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
	}
	m.spinner, _ = m.spinner.Update(msg)

	return m, tea.Batch(cmds...)
}

func (m *shellModel) View() string {
	if m.phase == phaseWizard {
		return m.renderWizard()
	}
	var b strings.Builder
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n")
	b.WriteString(m.renderSummaryPane())
	b.WriteString("\n")
	b.WriteString(m.renderServicesPane())
	b.WriteString("\n")
	b.WriteString(m.renderHistoryPane())
	b.WriteString("\n")
	b.WriteString(m.renderJobsPane())
	b.WriteString("\n")
	b.WriteString(m.renderLogsPane())
	b.WriteString("\n")
	b.WriteString(m.renderTimelinePane())
	b.WriteString("\n")
	b.WriteString(m.renderCommandPalette())
	b.WriteString("\n")
	b.WriteString(m.textInput.View())
	return b.String()
}

func (m *shellModel) renderWizard() string {
	switch m.wizard.step {
	case wizardStepWorkspace:
		return fmt.Sprintf("Workspace detected: %s\nPress Enter to continue to model selection.", workspaceFromConfig(m.cfg))
	case wizardStepModel:
		var b strings.Builder
		b.WriteString("Select default model (use j/k and enter):\n")
		b.WriteString(m.modelsList.View())
		b.WriteString("\nPress Enter to continue or Shift+Tab to go back.")
		return b.String()
	case wizardStepLanguages:
		var b strings.Builder
		b.WriteString("Toggle languages with space. Enter to continue or Shift+Tab to go back.\n")
		b.WriteString(m.lspsList.View())
		return b.String()
	case wizardStepTooling:
		current := "enabled"
		if m.cfg.ToolCalling != nil && !*m.cfg.ToolCalling {
			current = "disabled"
		}
		return fmt.Sprintf("Tool calling is currently %s.\nPress Space to toggle, Enter to finish, or Shift+Tab to go back.", current)
	}
	return "Wizard complete"
}

func (m *shellModel) advanceWizard() tea.Cmd {
	switch m.wizard.step {
	case wizardStepWorkspace:
		m.wizard.step = wizardStepModel
	case wizardStepModel:
		if item, ok := m.modelsList.SelectedItem().(uiListItem); ok {
			m.cfg.Ollama.SelectedModel = item.value
		}
		m.wizard.step = wizardStepLanguages
	case wizardStepLanguages:
		m.cfg.Languages = m.selectedLanguageValues()
		_ = setup.SaveConfig(m.configPath, m.cfg)
		if err := m.tc.WarmLanguages(m.cfg.Languages); err != nil {
			m.logStream <- logLineMsg{Entry: logEntry{
				Timestamp: time.Now(),
				Source:    "toolchain",
				Line:      fmt.Sprintf("warm warning: %v", err),
			}}
		}
		m.wizard.step = wizardStepTooling
	case wizardStepTooling:
		m.wizard.step = wizardStepComplete
		_ = setup.SaveConfig(m.configPath, m.cfg)
		m.phase = phaseShell
		return nil
	}
	return nil
}

func (m *shellModel) rewindWizard() {
	if m.wizard.step == wizardStepWorkspace {
		return
	}
	m.wizard.step--
}

func saveWizardConfig(path string, cfg *setup.Config) {
	_ = setup.SaveConfig(path, cfg)
}

func (m *shellModel) handleSubmittedCommand() tea.Cmd {
	value := strings.TrimSpace(m.textInput.Value())
	if value == "" {
		return nil
	}
	m.history = append(m.history, value)
	if len(m.history) > 200 {
		m.history = m.history[len(m.history)-200:]
	}
	m.textInput.SetValue("")
	verb, rest := splitCommand(value)
	switch verb {
	case "quit", "exit":
		return tea.Quit
	case "status":
		m.statusLine = fmt.Sprintf("Workspace: %s, Model: %s", workspaceFromConfig(m.cfg), m.cfg.ModelOrDefault(flagModel))
	case "detect":
		return m.runDetection()
	case "task":
		if rest == "" {
			m.statusLine = "usage: task <instruction>"
			return nil
		}
		return m.runJob("task", framework.TaskTypeCodeModification, rest, "", "")
	case "write":
		if rest == "" {
			m.statusLine = "usage: write <instruction>"
			return nil
		}
		return m.runJob("write", framework.TaskTypeCodeGeneration, rest, "", "")
	case "analyze":
		if rest == "" {
			m.statusLine = "usage: analyze <instruction>"
			return nil
		}
		return m.runJob("analyze", framework.TaskTypeAnalysis, rest, "", "")
	case "apply":
		file, lang, instr, err := parseApplyArgs(rest)
		if err != nil {
			m.statusLine = err.Error()
			return nil
		}
		return m.runJob("apply", framework.TaskTypeCodeModification, instr, file, lang)
	case "wizard":
		m.phase = phaseWizard
		m.wizard.step = wizardStepWorkspace
		return nil
	default:
		m.statusLine = fmt.Sprintf("unknown command: %s", verb)
	}
	return nil
}

func (m *shellModel) runDetection() tea.Cmd {
	servers := make([]setup.LSPServer, len(m.cfg.LSPServers))
	copy(servers, m.cfg.LSPServers)
	ch := m.detectCh
	return func() tea.Msg {
		go func() {
			total := len(servers)
			for i, srv := range servers {
				ch <- detectionProgressMsg{
					Language: srv.Language,
					Index:    i,
					Total:    total,
					Status:   "checking",
				}
				time.Sleep(120 * time.Millisecond)
			}
			cfg, err := refreshShellConfig(m.configPath, m.cfg)
			ch <- detectionCompleteMsg{Config: cfg, Err: err}
		}()
		return detectionProgressMsg{Language: "starting", Status: "begin", Index: 0, Total: len(servers)}
	}
}

func (m *shellModel) runJob(label string, taskType framework.TaskType, instruction, file, lang string) tea.Cmd {
	jobID := fmt.Sprintf("job-%d", m.jobCounter+1)
	m.jobCounter++
	job := &jobEntry{
		ID:     jobID,
		Label:  instruction,
		Type:   label,
		Status: jobQueued,
		Phases: map[string]jobStatus{},
	}
	m.jobs[jobID] = job
	m.jobOrder = append(m.jobOrder, jobID)
	m.refreshJobsTable()
	return func() tea.Msg {
		return m.launchJob(jobID, label, taskType, instruction, file, lang)
	}
}

func (m *shellModel) launchJob(jobID, label string, taskType framework.TaskType, instruction, file, lang string) tea.Msg {
	go func() {
		job := m.jobs[jobID]
		if job == nil {
			return
		}
		workspace := workspaceFromConfig(m.cfg)
		agentCfg := buildFrameworkConfig(m.cfg)
		memory, err := framework.NewHybridMemory(filepath.Join(workspace, ".memory"))
		if err != nil {
			m.finishJob(jobID, nil, nil, err)
			return
		}
		var registry *framework.ToolRegistry
		var tcErr error
		if lang != "" {
			if trackErr := ensureLanguageTracked(m.cmd, m.cfg, m.configPath, lang); trackErr != nil {
				m.logStream <- logLineMsg{Entry: logEntry{Timestamp: time.Now(), Source: "config", Line: trackErr.Error()}}
			}
			if warmErr := m.tc.WarmLanguages([]string{lang}); warmErr != nil {
				m.logStream <- logLineMsg{Entry: logEntry{Timestamp: time.Now(), Source: "toolchain", Line: warmErr.Error()}}
			}
			tcErr = m.tc.EnsureLanguage(lang)
		}
		if tcErr != nil {
			m.logStream <- logLineMsg{Entry: logEntry{Timestamp: time.Now(), Source: label, Line: tcErr.Error()}}
			m.timelineCh <- timelineMsg{Entry: timelineEntry{JobID: jobID, Timestamp: time.Now(), Label: "error", Content: tcErr.Error()}}
			m.finishJob(jobID, nil, nil, tcErr)
			return
		}
		registry, err = m.tc.BuildRegistry(lang)
		if err != nil {
			m.finishJob(jobID, nil, nil, err)
			return
		}
		modelClient := llm.NewClient(agentCfg.OllamaEndpoint, agentCfg.Model)
		agent := server.AgentFactory(modelClient, registry, memory, agentCfg)
		task := &framework.Task{
			ID:          fmt.Sprintf("%s-%d", label, time.Now().UnixNano()),
			Type:        taskType,
			Instruction: instruction,
			Context: map[string]any{
				"workspace": workspace,
			},
		}
		if file != "" {
			absFile := file
			if !filepath.IsAbs(absFile) {
				absFile = filepath.Join(workspace, file)
			}
			data, readErr := os.ReadFile(absFile)
			if readErr != nil {
				m.finishJob(jobID, nil, nil, readErr)
				return
			}
			task.Context["file"] = absFile
			task.Context["files"] = []string{absFile}
			task.Context["code"] = string(data)
			if lang == "" {
				lang = cliutils.InferLanguageByExtension(absFile)
			}
			if lang != "" {
				task.Context["language"] = lang
			}
		}
		state := framework.NewContext()
		state.Set("task.id", task.ID)
		state.Set("workspace.root", workspace)
		state.Set("toolchain.describe", m.tc.Describe())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go m.streamContext(ctx, jobID, state)
		result, execErr := agent.Execute(ctx, task, state)
		m.finishJob(jobID, result, state, execErr)
	}()
	return jobStartedMsg{Job: m.jobs[jobID]}
}

func (m *shellModel) finishJob(jobID string, result *framework.Result, state *framework.Context, err error) {
	m.logStream <- jobFinishedMsg{JobID: jobID, Result: result, State: state, Err: err}
	if state != nil {
		if final, ok := state.Get("react.final_output"); ok {
			m.timelineCh <- timelineMsg{Entry: timelineEntry{
				JobID:     jobID,
				Timestamp: time.Now(),
				Label:     "final",
				Content:   fmt.Sprint(final),
			}}
		}
	}
}

func (m *shellModel) streamContext(ctx context.Context, jobID string, state *framework.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var last int
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			history := state.Snapshot().History
			if len(history) > last {
				newEntries := history[last:]
				last = len(history)
				for _, entry := range newEntries {
					m.timelineCh <- timelineMsg{Entry: timelineEntry{
						JobID:     jobID,
						Timestamp: entry.Timestamp,
						Label:     entry.Role,
						Content:   entry.Content,
					}}
				}
			}
		}
	}
}

func (m *shellModel) renderStatusBar() string {
	status := fmt.Sprintf("Workspace: %s | Model: %s", workspaceFromConfig(m.cfg), m.cfg.ModelOrDefault(flagModel))
	if m.detection.Running {
		pct := float64(m.detection.Progress) / float64(max(1, m.detection.Total))
		status += " | Detecting " + m.progress.ViewAs(pct)
	}
	if m.statusLine != "" {
		status += " | " + m.statusLine
	}
	return lipgloss.NewStyle().Bold(true).Render(status)
}

func (m *shellModel) renderSummaryPane() string {
	var b strings.Builder
	b.WriteString("Environment Summary\n")
	b.WriteString(fmt.Sprintf("Workspace: %s\n", workspaceFromConfig(m.cfg)))
	b.WriteString(fmt.Sprintf("Model: %s\n", m.cfg.ModelOrDefault(flagModel)))
	b.WriteString(fmt.Sprintf("Languages: %s\n", strings.Join(m.cfg.Languages, ", ")))
	b.WriteString("Agents: coding\n")
	b.WriteString("\nModels:\n")
	b.WriteString(m.modelsList.View())
	b.WriteString("\nLSP Servers:\n")
	b.WriteString(m.lspsList.View())
	return b.String()
}

func (m *shellModel) renderServicesPane() string {
	return "Services\n" + m.servicesTbl.View()
}

func (m *shellModel) renderHistoryPane() string {
	content := strings.Join(m.history, "\n")
	m.historyPort.SetContent(content)
	return "Command History\n" + m.historyPort.View()
}

func (m *shellModel) renderJobsPane() string {
	return "Jobs\n" + m.jobsTbl.View()
}

func (m *shellModel) renderLogsPane() string {
	lines := make([]string, len(m.logs))
	for i, entry := range m.logs {
		lines[i] = fmt.Sprintf("[%s] %s: %s", entry.Timestamp.Format(time.Kitchen), entry.Source, entry.Line)
	}
	m.logPort.SetContent(strings.Join(lines, "\n"))
	return "Logs\n" + m.logPort.View()
}

func (m *shellModel) renderTimelinePane() string {
	lines := make([]string, len(m.timeline))
	for i, entry := range m.timeline {
		lines[i] = fmt.Sprintf("[%s][%s] %s", entry.Timestamp.Format(time.Kitchen), entry.Label, entry.Content)
	}
	m.timelinePort.SetContent(strings.Join(lines, "\n"))
	return "Timeline\n" + m.timelinePort.View()
}

func (m *shellModel) renderCommandPalette() string {
	return "Commands: status | detect | task <instr> | write <instr> | analyze <instr> | apply file :: instruction | wizard | quit"
}

func (m *shellModel) appendLog(entry logEntry) {
	m.logs = append(m.logs, entry)
	if len(m.logs) > 400 {
		m.logs = m.logs[len(m.logs)-400:]
	}
}

func (m *shellModel) appendTimeline(entry timelineEntry) {
	m.timeline = append(m.timeline, entry)
	if len(m.timeline) > 400 {
		m.timeline = m.timeline[len(m.timeline)-400:]
	}
}

func (m *shellModel) consumeToolchainEvent(evt toolchain.Event) {
	switch evt.Type {
	case toolchain.EventLogLine:
		m.appendLog(logEntry{
			Timestamp: evt.Timestamp,
			Source:    fmt.Sprintf("lsp:%s", evt.Language),
			Line:      evt.Message,
		})
	case toolchain.EventWarmStart, toolchain.EventEnsureStart:
		m.statusLine = fmt.Sprintf("%s %s", evt.Type, evt.Language)
	case toolchain.EventWarmSuccess, toolchain.EventEnsureDone:
		m.statusLine = fmt.Sprintf("%s ready", evt.Language)
		m.refreshServicesTable()
	case toolchain.EventWarmFailed, toolchain.EventEnsureFailed:
		m.statusLine = fmt.Sprintf("%s failed: %v", evt.Language, evt.Err)
	case toolchain.EventShutdown:
		m.statusLine = fmt.Sprintf("%s shut down", evt.Language)
	}
}

func (m *shellModel) refreshLists() {
	items := make([]list.Item, 0, len(m.cfg.Ollama.AvailableModels))
	for _, name := range m.cfg.Ollama.AvailableModels {
		title := name
		if name == m.cfg.Ollama.SelectedModel {
			title = fmt.Sprintf("%s (current)", name)
		}
		items = append(items, uiListItem{title: title, value: name})
	}
	m.modelsList.SetItems(items)
	m.refreshLanguageList()
	m.refreshServicesTable()
}

func (m *shellModel) refreshLanguageList() {
	items := make([]list.Item, 0, len(m.cfg.LSPServers))
	for _, srv := range m.cfg.LSPServers {
		status := "missing"
		if srv.Available {
			status = "available"
		}
		id := canonicalLangKey(srv.ID)
		if id == "" {
			id = srv.ID
		}
		check := " "
		if m.selectedLanguages[id] {
			check = "x"
		}
		title := fmt.Sprintf("[%s] %s (%s)", check, srv.Language, status)
		items = append(items, uiListItem{
			title: title,
			value: id,
			desc:  strings.Join(srv.Extensions, ","),
		})
	}
	m.lspsList.SetItems(items)
}

func (m *shellModel) refreshServicesTable() {
	rows := []table.Row{}
	desc := m.tc.Describe()
	if services, ok := desc["services"].([]map[string]any); ok {
		for _, service := range services {
			rows = append(rows, table.Row{
				fmt.Sprint(service["language"]),
				fmt.Sprint(service["pid"]),
				fmt.Sprint(service["command"]),
				"running",
			})
		}
	}
	m.servicesTbl.SetRows(rows)
}

func (m *shellModel) refreshJobsTable() {
	rows := make([]table.Row, 0, len(m.jobOrder))
	for _, id := range m.jobOrder {
		job := m.jobs[id]
		duration := "-"
		if !job.Started.IsZero() {
			end := job.Finished
			if end.IsZero() {
				end = time.Now()
			}
			duration = end.Sub(job.Started).Truncate(time.Second).String()
		}
		rows = append(rows, table.Row{
			job.ID,
			job.Type,
			string(job.Status),
			duration,
			trimString(job.Label, 30),
		})
	}
	m.jobsTbl.SetRows(rows)
}

func trimString(val string, size int) string {
	if len(val) <= size {
		return val
	}
	return val[:size-3] + "..."
}

func (m *shellModel) toggleSelectedLanguage() {
	item, ok := m.lspsList.SelectedItem().(uiListItem)
	if !ok {
		return
	}
	id := item.value
	if id == "" {
		return
	}
	m.selectedLanguages[id] = !m.selectedLanguages[id]
	m.refreshLanguageList()
}

func (m *shellModel) selectedLanguageValues() []string {
	langs := make([]string, 0, len(m.selectedLanguages))
	for id, selected := range m.selectedLanguages {
		if selected {
			langs = append(langs, id)
		}
	}
	return langs
}

func (m *shellModel) toggleToolCalling() {
	next := true
	if m.cfg.ToolCalling != nil {
		next = !*m.cfg.ToolCalling
	}
	m.cfg.ToolCalling = &next
}

func initialLanguageSelection(cfg *setup.Config) map[string]bool {
	selected := map[string]bool{}
	if cfg == nil {
		return selected
	}
	for _, lang := range cfg.Languages {
		key := canonicalLangKey(lang)
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(lang))
		}
		selected[key] = true
	}
	return selected
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type uiListItem struct {
	title string
	value string
	desc  string
}

func (l uiListItem) Title() string       { return l.title }
func (l uiListItem) Description() string { return l.desc }
func (l uiListItem) FilterValue() string { return l.value }
