package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// RunTestsTool executes test commands.
type RunTestsTool struct {
	Command []string
	Workdir string
	Timeout time.Duration
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
	stdout, stderr, err := runCommand(ctx, t.Workdir, t.Timeout, cmdline...)
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

// ExecuteCodeTool runs arbitrary snippets inside an interpreter.
type ExecuteCodeTool struct {
	Command []string
	Workdir string
	Timeout time.Duration
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
	stdout, stderr, err := runCommandWithInput(ctx, t.Workdir, t.Timeout, code, t.Command...)
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

// RunLinterTool executes lint commands.
type RunLinterTool struct {
	Command []string
	Workdir string
	Timeout time.Duration
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
	stdout, stderr, err := runCommand(ctx, t.Workdir, t.Timeout, cmdline...)
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

// RunBuildTool runs arbitrary build commands.
type RunBuildTool struct {
	Command []string
	Workdir string
	Timeout time.Duration
}

func (t *RunBuildTool) Name() string        { return "exec_run_build" }
func (t *RunBuildTool) Description() string { return "Runs builds or compiles the project." }
func (t *RunBuildTool) Category() string    { return "execution" }
func (t *RunBuildTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{}
}
func (t *RunBuildTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	stdout, stderr, err := runCommand(ctx, t.Workdir, t.Timeout, t.Command...)
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

func runCommand(ctx context.Context, workdir string, timeout time.Duration, args ...string) (string, string, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("command required")
	}
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func runCommandWithInput(ctx context.Context, workdir string, timeout time.Duration, input string, args ...string) (string, string, error) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = bytes.NewBufferString(input)
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout == 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
