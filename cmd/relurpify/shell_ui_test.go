package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lexcodex/relurpify/cmd/internal/setup"
	"github.com/lexcodex/relurpify/cmd/internal/toolchain"
	"github.com/lexcodex/relurpify/framework"
)

func TestShellWizardFlow(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &setup.Config{
		Workspace: tmpDir,
		Languages: []string{"go"},
		Ollama: setup.OllamaConfig{
			AvailableModels: []string{"deepseek", "qwen"},
			SelectedModel:   "deepseek",
		},
		LSPServers: []setup.LSPServer{
			{ID: "go", Language: "Go", Available: false},
			{ID: "rust", Language: "Rust", Available: false},
		},
	}
	tc, err := toolchain.NewManager(tmpDir, cfg.LSPServers, nil)
	require.NoError(t, err)
	model := newShellModel(nil, "", cfg, nil, tc, nil)

	require.Equal(t, wizardStepWorkspace, model.wizard.step)
	model.advanceWizard()
	require.Equal(t, wizardStepAgents, model.wizard.step)

	model.advanceWizard()
	require.Equal(t, wizardStepToolAllowlist, model.wizard.step)

	model.advanceWizard()
	require.Equal(t, wizardStepModel, model.wizard.step)

	model.modelsList.Select(1)
	model.advanceWizard()
	require.Equal(t, "qwen", cfg.Ollama.SelectedModel)
	require.Equal(t, wizardStepLanguages, model.wizard.step)

	model.selectedLanguages["rust"] = true
	model.advanceWizard()
	require.ElementsMatch(t, []string{"go", "rust"}, cfg.Languages)
	require.Equal(t, wizardStepTooling, model.wizard.step)

	model.advanceWizard()
	require.Equal(t, phaseShell, model.phase)
}

func TestShellDetectionAndJobMessages(t *testing.T) {
	cfg := &setup.Config{
		Workspace: ".",
		Ollama: setup.OllamaConfig{
			SelectedModel: "deepseek",
		},
	}
	tc, err := toolchain.NewManager(".", nil, nil)
	require.NoError(t, err)
	model := newShellModel(nil, "", cfg, nil, tc, nil)
	model.phase = phaseShell

	model.Update(detectionProgressMsg{Language: "go", Index: 0, Total: 2, Status: "checking"})
	model.detection.Lock()
	require.True(t, model.detection.Running)
	require.Equal(t, 1, model.detection.Progress)
	require.Equal(t, "checking", model.detection.Status["go"])
	model.detection.Unlock()

	newCfg := *cfg
	newCfg.Ollama.SelectedModel = "qwen"
	model.Update(detectionCompleteMsg{Config: &newCfg})
	require.False(t, model.detection.Running)
	require.Equal(t, "qwen", model.cfg.Ollama.SelectedModel)

	job := &jobEntry{ID: "job-1", Status: jobQueued}
	model.jobs[job.ID] = job
	model.jobOrder = append(model.jobOrder, job.ID)

	model.Update(jobStartedMsg{Job: job})
	require.Equal(t, jobRunning, job.Status)
	require.False(t, job.Started.IsZero())

	model.Update(jobUpdateMsg{JobID: job.ID, Phase: "llm", Status: jobRunning, Info: "thinking"})
	require.Equal(t, jobRunning, job.Phases["llm"])
	require.Equal(t, "thinking", job.PhaseInfo["llm"])

	res := &framework.Result{NodeID: "node-1", Success: true}
	state := framework.NewContext()
	model.Update(jobFinishedMsg{JobID: job.ID, Result: res, State: state})
	require.Equal(t, jobSuccess, job.Status)
	require.Equal(t, res, job.Result)
	require.Equal(t, state, job.State)
	require.False(t, job.Finished.IsZero())
}
