package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/cmd/internal/cliutils"
	"github.com/lexcodex/relurpify/cmd/internal/setup"
	"github.com/lexcodex/relurpify/cmd/internal/toolchain"
	"github.com/lexcodex/relurpify/cmd/internal/workspacecfg"
	"github.com/lexcodex/relurpify/framework"
)

func newShellCmd() *cobra.Command {
	var configPath string
	var forceDetect bool
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Interactive agent shell with autodetected tooling",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfgFlag := cmd.Flags().Lookup("config"); cfgFlag == nil || !cfgFlag.Changed {
				configPath = setup.DefaultConfigPath(flagWorkspace)
			}
			cfg, created, err := ensureShellConfig(configPath, forceDetect)
			if err != nil {
				return err
			}
			if created {
				cmd.Printf("Environment detected for workspace %s\n", workspaceFromConfig(cfg))
			}
			wsRoot := workspaceFromConfig(cfg)
			wsCfg, wsErr := workspacecfg.Load(wsRoot)
			if wsErr != nil {
				if !os.IsNotExist(wsErr) {
					return wsErr
				}
				wsCfg = nil
			}
			if wsCfg != nil {
				if err := workspacecfg.EnsureManifests(wsCfg); err != nil {
					return err
				}
			}
			return runShell(cmd, configPath, cfg, wsCfg)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", setup.DefaultConfigPath(flagWorkspace), "Config file to load/save")
	cmd.Flags().BoolVar(&forceDetect, "detect", false, "Re-run detection before starting the shell")
	return cmd
}

func ensureShellConfig(path string, force bool) (*setup.Config, bool, error) {
	prev, err := loadSetupConfig(path)
	if err != nil {
		return nil, false, err
	}
	if prev == nil || force {
		cfg, err := setup.Detect(flagWorkspace, flagEndpoint, flagModel, prev)
		if err != nil {
			return nil, false, err
		}
		if err := setup.SaveConfig(path, cfg); err != nil {
			return nil, false, err
		}
		return cfg, true, nil
	}
	return prev, false, nil
}

func runShell(cmd *cobra.Command, configPath string, cfg *setup.Config, wsCfg *workspacecfg.WorkspaceConfig) error {
	workspace := workspaceFromConfig(cfg)
	if wsCfg != nil && wsCfg.Workspace != "" {
		workspace = wsCfg.Workspace
	}
	eventCh := make(chan toolchain.Event, 128)
	tc, err := toolchain.NewManager(workspace, cfg.LSPServers, eventCh)
	if err != nil {
		close(eventCh)
		return err
	}
	model := newShellModel(cmd, configPath, cfg, wsCfg, tc, eventCh)
	if model.phase == phaseShell {
		if err := tc.WarmLanguages(cfg.Languages); err != nil {
			model.appendLog(logEntry{
				Timestamp: time.Now(),
				Source:    "toolchain",
				Line:      fmt.Sprintf("warm warning: %v", err),
			})
		}
	}
	program := tea.NewProgram(model, tea.WithInput(cmd.InOrStdin()), tea.WithOutput(cmd.OutOrStdout()))
	defer func() {
		tc.Close()
		close(eventCh)
	}()
	_, err = program.Run()
	return err
}

func refreshShellConfig(path string, prev *setup.Config) (*setup.Config, error) {
	cfg, err := setup.Detect(flagWorkspace, flagEndpoint, flagModel, prev)
	if err != nil {
		return nil, err
	}
	if err := setup.SaveConfig(path, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func workspaceFromConfig(cfg *setup.Config) string {
	if cfg != nil && cfg.Workspace != "" {
		if abs, err := filepath.Abs(cfg.Workspace); err == nil {
			return abs
		}
		return cfg.Workspace
	}
	if abs, err := filepath.Abs(flagWorkspace); err == nil {
		return abs
	}
	return flagWorkspace
}

func buildFrameworkConfig(cfg *setup.Config) *framework.Config {
	model := flagModel
	if cfg != nil {
		if cfg.Ollama.SelectedModel != "" {
			model = cfg.Ollama.SelectedModel
		}
	}
	endpoint := flagEndpoint
	if cfg != nil && cfg.Ollama.Endpoint != "" {
		endpoint = cfg.Ollama.Endpoint
	}
	disableTools := flagDisableTools
	if !disableTools && cfg != nil && cfg.ToolCalling != nil {
		disableTools = !*cfg.ToolCalling
	}
	return &framework.Config{
		Model:              model,
		OllamaEndpoint:     endpoint,
		DefaultAgent:       "coding",
		MaxIterations:      8,
		DisableToolCalling: disableTools,
	}
}

func ensureLanguageTracked(cmd *cobra.Command, cfg *setup.Config, configPath, lang string) error {
	if cfg == nil || lang == "" {
		return nil
	}
	lang = canonicalLangKey(lang)
	if lang == "" {
		return nil
	}
	for _, existing := range cfg.Languages {
		if existing == lang {
			return nil
		}
	}
	cfg.Languages = append(cfg.Languages, lang)
	cfg.Languages = normalizeLanguageList(cfg.Languages)
	cfg.LastUpdated = time.Now()
	if err := setup.SaveConfig(configPath, cfg); err != nil {
		return err
	}
	cmd.Printf("Added language %s to workspace config.\n", lang)
	return nil
}

func normalizeLanguageList(langs []string) []string {
	if len(langs) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(langs))
	for _, lang := range langs {
		lang = canonicalLangKey(lang)
		if lang == "" {
			continue
		}
		if _, ok := seen[lang]; ok {
			continue
		}
		seen[lang] = struct{}{}
		result = append(result, lang)
	}
	sort.Strings(result)
	return result
}

func canonicalLangKey(lang string) string {
	if lang == "" {
		return ""
	}
	if desc, ok := cliutils.LookupLSPDescriptor(lang); ok && desc.ID != "" {
		return desc.ID
	}
	return strings.ToLower(lang)
}

func parseApplyArgs(input string) (string, string, string, error) {
	rest := strings.TrimSpace(input)
	if rest == "" {
		return "", "", "", errors.New("usage: apply [lang=<id>] <file> :: <instruction>")
	}
	language := ""
	lower := strings.ToLower(rest)
	if strings.HasPrefix(lower, "lang=") {
		rest = rest[5:]
		idx := strings.IndexAny(rest, " \t")
		if idx == -1 {
			return "", "", "", errors.New("file path required after lang option")
		}
		language = strings.TrimSpace(rest[:idx])
		rest = strings.TrimSpace(rest[idx+1:])
	}
	parts := strings.SplitN(rest, "::", 2)
	if len(parts) != 2 {
		return "", "", "", errors.New("usage: apply [lang=<id>] <file> :: <instruction>")
	}
	file := strings.TrimSpace(parts[0])
	instruction := strings.TrimSpace(parts[1])
	if file == "" || instruction == "" {
		return "", "", "", errors.New("usage: apply [lang=<id>] <file> :: <instruction>")
	}
	return file, language, instruction, nil
}

func splitCommand(line string) (string, string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", ""
	}
	idx := strings.IndexAny(trimmed, " \t")
	if idx == -1 {
		return strings.ToLower(trimmed), ""
	}
	return strings.ToLower(trimmed[:idx]), strings.TrimSpace(trimmed[idx+1:])
}
