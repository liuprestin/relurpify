package framework

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPermissionSetValidate ensures Validate catches missing paths/binaries and
// accepts well-formed permission sets.
func TestPermissionSetValidate(t *testing.T) {
	valid := &PermissionSet{
		FileSystem: []FileSystemPermission{
			{Action: FileSystemRead, Path: "/workspace/**"},
		},
		Executables: []ExecutablePermission{
			{Binary: "go", Args: []string{"test"}},
		},
	}
	require.NoError(t, valid.Validate())

	invalid := &PermissionSet{
		FileSystem: []FileSystemPermission{{Action: FileSystemRead}},
	}
	require.Error(t, invalid.Validate(), "missing path should fail validation")

	badExec := &PermissionSet{
		FileSystem: []FileSystemPermission{{Action: FileSystemRead, Path: "/**"}},
		Executables: []ExecutablePermission{
			{Binary: ""},
		},
	}
	require.Error(t, badExec.Validate(), "missing binary should fail validation")
}

// TestPermissionManagerAuthorizeToolEnforcesSubset verifies that tool-specific
// manifests cannot request filesystem scopes beyond the agent manifest.
func TestPermissionManagerAuthorizeToolEnforcesSubset(t *testing.T) {
	ctx := context.Background()
	manager := newTestManager(t, "/workspace", &PermissionSet{
		FileSystem: []FileSystemPermission{
			{Action: FileSystemRead, Path: "/workspace/**"},
			{Action: FileSystemList, Path: "/workspace/**"},
		},
	})

	okTool := stubTool{
		name: "list",
		perms: &PermissionSet{
			FileSystem: []FileSystemPermission{
				{Action: FileSystemRead, Path: "/workspace/**"},
			},
		},
	}
	require.NoError(t, manager.AuthorizeTool(ctx, "agent-1", okTool, nil))

	badTool := stubTool{
		name: "escape",
		perms: &PermissionSet{
			FileSystem: []FileSystemPermission{
				{Action: FileSystemRead, Path: "/etc/**"},
			},
		},
	}
	err := manager.AuthorizeTool(ctx, "agent-1", badTool, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds agent permissions")
}

// TestPermissionManagerCheckFileAccess checks file authorization rejects
// traversal attempts and unauthorized actions.
func TestPermissionManagerCheckFileAccess(t *testing.T) {
	ctx := context.Background()
	manager := newTestManager(t, "/workspace", &PermissionSet{
		FileSystem: []FileSystemPermission{
			{Action: FileSystemRead, Path: "/workspace/src/**"},
		},
	})

	require.NoError(t, manager.CheckFileAccess(ctx, "agent-1", FileSystemRead, "src/main.go"))

	err := manager.CheckFileAccess(ctx, "agent-1", FileSystemRead, "../etc/passwd")
	require.Error(t, err, "path traversal should be denied")

	err = manager.CheckFileAccess(ctx, "agent-1", FileSystemWrite, "src/main.go")
	require.Error(t, err, "write action not declared should be denied")
}

// TestPermissionHelpers confirms helper constructors produce intuitive globs
// and executable permissions.
func TestPermissionHelpers(t *testing.T) {
	fs := NewFileSystemPermissionSet("/workspace", FileSystemRead, FileSystemList)
	require.Len(t, fs.FileSystem, 2)
	require.Equal(t, "/workspace/**", fs.FileSystem[0].Path)

	exec := NewExecutionPermissionSet("/workspace", "python3", []string{"script.py"})
	require.Len(t, exec.Executables, 1)
	require.Equal(t, "python3", exec.Executables[0].Binary)
	require.Contains(t, exec.FileSystem, FileSystemPermission{Action: FileSystemExecute, Path: "/workspace/**"})
}

type stubTool struct {
	name  string
	perms *PermissionSet
}

// Name identifies the stub tool in registry lookups.
func (t stubTool) Name() string { return t.name }

// Description satisfies the Tool interface.
func (t stubTool) Description() string { return "stub" }

// Category returns the testing category for clarity.
func (t stubTool) Category() string { return "test" }

// Parameters indicates the stub tool takes no arguments.
func (t stubTool) Parameters() []ToolParameter { return nil }

// Execute returns a successful result so authorization paths can be tested in
// isolation.
func (t stubTool) Execute(context.Context, *Context, map[string]interface{}) (*ToolResult, error) {
	return &ToolResult{Success: true}, nil
}

// IsAvailable indicates the stub is always ready to run.
func (t stubTool) IsAvailable(context.Context, *Context) bool { return true }

// Permissions returns the configured permission set for the stub tool.
func (t stubTool) Permissions() ToolPermissions { return ToolPermissions{Permissions: t.perms} }

// newTestManager is a helper that fails tests immediately when the permission
// manager cannot be constructed.
func newTestManager(t *testing.T, base string, perms *PermissionSet) *PermissionManager {
	t.Helper()
	manager, err := NewPermissionManager(base, perms, nil, nil)
	require.NoError(t, err)
	return manager
}
