package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// RunTestsTool executes test commands.
type RunTestsTool struct {
	Command []string
	Workdir string
	Timeout time.Duration
	Runner  framework.CommandRunner
	manager *framework.PermissionManager
	agentID string
	spec    *framework.AgentRuntimeSpec
}

func (t *RunTestsTool) SetPermissionManager(manager *framework.PermissionManager, agentID string) {
	t.manager = manager
	t.agentID = agentID
}

func (t *RunTestsTool) SetAgentSpec(spec *framework.AgentRuntimeSpec, agentID string) {
	t.spec = spec
	t.agentID = agentID
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
	if err := t.authorizeCommand(ctx, cmdline); err != nil {
		return nil, err
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
	return framework.ToolPermissions{Permissions: framework.NewExecutionPermissionSet(t.Workdir, t.Command[0], t.Command[1:])}
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
	manager *framework.PermissionManager
	agentID string
	spec    *framework.AgentRuntimeSpec
}

func (t *ExecuteCodeTool) SetPermissionManager(manager *framework.PermissionManager, agentID string) {
	t.manager = manager
	t.agentID = agentID
}

func (t *ExecuteCodeTool) SetAgentSpec(spec *framework.AgentRuntimeSpec, agentID string) {
	t.spec = spec
	t.agentID = agentID
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
	cmdline := append([]string{}, t.Command...)
	cmdline = append(cmdline, code)
	if err := t.authorizeCommand(ctx, cmdline); err != nil {
		return nil, err
	}
	stdout, stderr, err := t.run(ctx, cmdline, "")
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
	perms := framework.NewExecutionPermissionSet(t.Workdir, t.Command[0], t.Command[1:])
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
	manager *framework.PermissionManager
	agentID string
	spec    *framework.AgentRuntimeSpec
}

func (t *RunLinterTool) SetPermissionManager(manager *framework.PermissionManager, agentID string) {
	t.manager = manager
	t.agentID = agentID
}

func (t *RunLinterTool) SetAgentSpec(spec *framework.AgentRuntimeSpec, agentID string) {
	t.spec = spec
	t.agentID = agentID
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
	if err := t.authorizeCommand(ctx, cmdline); err != nil {
		return nil, err
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
	return framework.ToolPermissions{Permissions: framework.NewExecutionPermissionSet(t.Workdir, t.Command[0], t.Command[1:])}
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
	manager *framework.PermissionManager
	agentID string
	spec    *framework.AgentRuntimeSpec
}

func (t *RunBuildTool) SetPermissionManager(manager *framework.PermissionManager, agentID string) {
	t.manager = manager
	t.agentID = agentID
}

func (t *RunBuildTool) SetAgentSpec(spec *framework.AgentRuntimeSpec, agentID string) {
	t.spec = spec
	t.agentID = agentID
}

func (t *RunBuildTool) Name() string        { return "exec_run_build" }
func (t *RunBuildTool) Description() string { return "Runs builds or compiles the project." }
func (t *RunBuildTool) Category() string    { return "execution" }
func (t *RunBuildTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{}
}
func (t *RunBuildTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	if err := t.authorizeCommand(ctx, t.Command); err != nil {
		return nil, err
	}
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
	return framework.ToolPermissions{Permissions: framework.NewExecutionPermissionSet(t.Workdir, t.Command[0], t.Command[1:])}
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

func (t *RunTestsTool) authorizeCommand(ctx context.Context, cmdline []string) error {
	return authorizeCommand(ctx, t.manager, t.agentID, t.spec, cmdline)
}

func (t *ExecuteCodeTool) authorizeCommand(ctx context.Context, cmdline []string) error {
	return authorizeCommand(ctx, t.manager, t.agentID, t.spec, cmdline)
}

func (t *RunLinterTool) authorizeCommand(ctx context.Context, cmdline []string) error {
	return authorizeCommand(ctx, t.manager, t.agentID, t.spec, cmdline)
}

func (t *RunBuildTool) authorizeCommand(ctx context.Context, cmdline []string) error {
	return authorizeCommand(ctx, t.manager, t.agentID, t.spec, cmdline)
}

func authorizeCommand(ctx context.Context, manager *framework.PermissionManager, agentID string, spec *framework.AgentRuntimeSpec, cmdline []string) error {
	if len(cmdline) == 0 {
		return fmt.Errorf("command empty")
	}
	binary := cmdline[0]
	args := []string{}
	if len(cmdline) > 1 {
		args = cmdline[1:]
	}
	if manager != nil {
		if err := manager.CheckExecutable(ctx, agentID, binary, args, nil); err != nil {
			return err
		}
	}
	if spec != nil {
		commandString := strings.TrimSpace(binary + " " + strings.Join(args, " "))
		decision, _ := framework.DecideByPatterns(commandString, spec.Bash.AllowPatterns, spec.Bash.DenyPatterns, spec.Bash.Default)
		switch decision {
		case framework.AgentPermissionDeny:
			return fmt.Errorf("command blocked: denied by bash_permissions")
		case framework.AgentPermissionAsk:
			if manager == nil {
				return fmt.Errorf("command blocked: approval required but permission manager missing")
			}
			return manager.RequireApproval(ctx, agentID, framework.PermissionDescriptor{
				Type:         framework.PermissionTypeHITL,
				Action:       "bash:exec",
				Resource:     commandString,
				RequiresHITL: true,
			}, "bash permission policy", framework.GrantScopeOneTime, framework.RiskLevelMedium, 0)
		}
	}
	return nil
}
