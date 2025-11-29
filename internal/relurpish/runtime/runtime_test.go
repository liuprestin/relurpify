package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkspaceGlob(t *testing.T) {
	dir := filepath.Join("/tmp", "relurpish")
	glob := workspaceGlob(dir)
	require.Equal(t, filepath.ToSlash(dir)+"/**", glob)
}

func TestBuildPermissionSetProfiles(t *testing.T) {
	dir := t.TempDir()
	readonly := buildPermissionSet(dir, PermissionProfileReadOnly)
	require.Len(t, readonly.FileSystem, 2)

	write := buildPermissionSet(dir, PermissionProfileWorkspaceWrite)
	require.Greater(t, len(write.FileSystem), len(readonly.FileSystem))
}

func TestSaveManifestCreatesFile(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Workspace = dir
	cfg.ManifestPath = filepath.Join(dir, "agent.manifest.yaml")
	cfg.ConfigPath = filepath.Join(dir, ".relurpish", "config.yaml")
	selection := WizardSelection{
		Model:   "deepseek-r1:7b",
		Agents:  []string{"coding"},
		Profile: PermissionProfileWorkspaceWrite,
		Tools:   []string{"file_read", "file_write"},
	}
	summary, err := SaveManifest(context.Background(), cfg, selection)
	require.NoError(t, err)
	require.True(t, summary.Exists)
	_, err = os.Stat(cfg.ManifestPath)
	require.NoError(t, err)
	wcfg, err := LoadWorkspaceConfig(cfg.ConfigPath)
	require.NoError(t, err)
	require.Equal(t, selection.Model, wcfg.Model)
	require.ElementsMatch(t, selection.Agents, wcfg.Agents)
}

func TestProbeEnvironmentHandlesMissingRunsc(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Workspace = dir
	cfg.ManifestPath = filepath.Join(dir, "agent.manifest.yaml")
	cfg.ConfigPath = filepath.Join(dir, ".relurpish", "config.yaml")
	cfg.Sandbox.RunscPath = "runsc-missing"
	report := ProbeEnvironment(context.Background(), cfg)
	require.Contains(t, strings.Join(report.Sandbox.Errors, " "), "runsc not found")
}
