package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// RunTestsTool executes test commands.
type RunTestsTool struct {
	Command []string
	Workdir string
	Timeout time.Duration
	Runner  framework.CommandRunner
}

func (t *RunTestsTool) Name() string        { return "exec_run_tests" }
func (t *RunTestsTool) Description() string { return "Runs project tests." }
func (t *RunTestsTool) Category() string    { return "execution" }
func (t *RunTestsTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "pattern", Type: "string", Required: false},
	}
}
func (t *RunTestsTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	pattern := fmt.Sprint(args["pattern"])
	cmdline := append([]string{}, t.Command...)
	if pattern != "" {
		cmdline = append(cmdline, pattern)
	}
	stdout, stderr, err := t.run(ctx, cmdline, "")
	if err != nil {
		return &framework.ToolResult{
			Success: false,
			Data: map[string]interface{}{
				"stdout": stdout,
				"stderr": stderr,
			},
			Error: err.Error(),
		}, nil
	}
	return &framework.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"stdout": stdout,
			"stderr": stderr,
		},
	}, nil
}
func (t *RunTestsTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return len(t.Command) > 0
}

func (t *RunTestsTool) Permissions() framework.ToolPermissions {
	if len(t.Command) == 0 {
		return framework.ToolPermissions{Permissions: framework.NewFileSystemPermissionSet(t.Workdir, framework.FileSystemRead, framework.FileSystemList)}
	}
	return framework.ToolPermissions{Permissions: framework.NewExecutionPermissionSet(t.Workdir, t.Command[0], t.Command)}
}

func (t *RunTestsTool) run(ctx context.Context, args []string, input string) (string, string, error) {
	if t.Runner == nil {
		return "", "", fmt.Errorf("command runner missing")
	}
	req := framework.CommandRequest{
		Workdir: t.Workdir,
		Args:    args,
		Input:   input,
		Timeout: t.Timeout,
	}
	return t.Runner.Run(ctx, req)
}

// ExecuteCodeTool runs arbitrary snippets inside an interpreter.
type ExecuteCodeTool struct {
	Command []string
	Workdir string
	Timeout time.Duration
	Runner  framework.CommandRunner
}

func (t *ExecuteCodeTool) Name() string { return "exec_run_code" }
func (t *ExecuteCodeTool) Description() string {
	return "Executes arbitrary code snippets in a sandbox."
}
func (t *ExecuteCodeTool) Category() string { return "execution" }
func (t *ExecuteCodeTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "code", Type: "string", Required: true},
	}
}
func (t *ExecuteCodeTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	code := fmt.Sprint(args["code"])
	stdout, stderr, err := t.run(ctx, t.Command, code)
	success := err == nil
	resultErr := ""
	if err != nil {
		resultErr = err.Error()
	}
	return &framework.ToolResult{
		Success: success,
		Data: map[string]interface{}{
			"stdout": stdout,
			"stderr": stderr,
		},
		Error: resultErr,
	}, nil
}
func (t *ExecuteCodeTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return len(t.Command) > 0
}

func (t *ExecuteCodeTool) Permissions() framework.ToolPermissions {
	if len(t.Command) == 0 {
		return framework.ToolPermissions{Permissions: framework.NewFileSystemPermissionSet(t.Workdir, framework.FileSystemRead)}
	}
	perms := framework.NewExecutionPermissionSet(t.Workdir, t.Command[0], t.Command)
	// Arbitrary code execution should always require HITL.
	for i := range perms.FileSystem {
		perms.FileSystem[i].HITLRequired = true
	}
	if len(perms.Executables) > 0 {
		perms.Executables[0].HITLRequired = true
	}
	return framework.ToolPermissions{Permissions: perms}
}

func (t *ExecuteCodeTool) run(ctx context.Context, args []string, input string) (string, string, error) {
	if t.Runner == nil {
		return "", "", fmt.Errorf("command runner missing")
	}
	req := framework.CommandRequest{
		Workdir: t.Workdir,
		Args:    args,
		Input:   input,
		Timeout: t.Timeout,
	}
	return t.Runner.Run(ctx, req)
}

// RunLinterTool executes lint commands.
type RunLinterTool struct {
	Command []string
	Workdir string
	Timeout time.Duration
	Runner  framework.CommandRunner
}

func (t *RunLinterTool) Name() string        { return "exec_run_linter" }
func (t *RunLinterTool) Description() string { return "Runs linting tools." }
func (t *RunLinterTool) Category() string    { return "execution" }
func (t *RunLinterTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "path", Type: "string", Required: false},
	}
}
func (t *RunLinterTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	cmdline := append([]string{}, t.Command...)
	if path := fmt.Sprint(args["path"]); path != "" {
		cmdline = append(cmdline, path)
	}
	stdout, stderr, err := t.run(ctx, cmdline)
	success := err == nil
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	return &framework.ToolResult{
		Success: success,
		Data: map[string]interface{}{
			"stdout": stdout,
			"stderr": stderr,
		},
		Error: errStr,
	}, nil
}
func (t *RunLinterTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return len(t.Command) > 0
}

func (t *RunLinterTool) Permissions() framework.ToolPermissions {
	if len(t.Command) == 0 {
		return framework.ToolPermissions{Permissions: framework.NewFileSystemPermissionSet(t.Workdir, framework.FileSystemRead)}
	}
	return framework.ToolPermissions{Permissions: framework.NewExecutionPermissionSet(t.Workdir, t.Command[0], t.Command)}
}

func (t *RunLinterTool) run(ctx context.Context, args []string) (string, string, error) {
	if t.Runner == nil {
		return "", "", fmt.Errorf("command runner missing")
	}
	req := framework.CommandRequest{
		Workdir: t.Workdir,
		Args:    args,
		Timeout: t.Timeout,
	}
	return t.Runner.Run(ctx, req)
}

// RunBuildTool runs arbitrary build commands.
type RunBuildTool struct {
	Command []string
	Workdir string
	Timeout time.Duration
	Runner  framework.CommandRunner
}

func (t *RunBuildTool) Name() string        { return "exec_run_build" }
func (t *RunBuildTool) Description() string { return "Runs builds or compiles the project." }
func (t *RunBuildTool) Category() string    { return "execution" }
func (t *RunBuildTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{}
}
func (t *RunBuildTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	stdout, stderr, err := t.run(ctx)
	success := err == nil
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	return &framework.ToolResult{
		Success: success,
		Data: map[string]interface{}{
			"stdout": stdout,
			"stderr": stderr,
		},
		Error: errStr,
	}, nil
}
func (t *RunBuildTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	return len(t.Command) > 0
}

func (t *RunBuildTool) Permissions() framework.ToolPermissions {
	if len(t.Command) == 0 {
		return framework.ToolPermissions{Permissions: framework.NewFileSystemPermissionSet(t.Workdir, framework.FileSystemRead)}
	}
	return framework.ToolPermissions{Permissions: framework.NewExecutionPermissionSet(t.Workdir, t.Command[0], t.Command)}
}

func (t *RunBuildTool) run(ctx context.Context) (string, string, error) {
	if t.Runner == nil {
		return "", "", fmt.Errorf("command runner missing")
	}
	req := framework.CommandRequest{
		Workdir: t.Workdir,
		Args:    t.Command,
		Timeout: t.Timeout,
	}
	return t.Runner.Run(ctx, req)
}
