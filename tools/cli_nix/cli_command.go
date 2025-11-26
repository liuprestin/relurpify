package clinix

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// CommandToolConfig captures metadata for wrapping an external CLI utility.
type CommandToolConfig struct {
	Name        string
	Description string
	Command     string
	Category    string
	DefaultArgs []string
	Timeout     time.Duration
}

// CommandTool executes a configured CLI binary with user-provided arguments.
type CommandTool struct {
	cfg      CommandToolConfig
	basePath string
}

// NewCommandTool builds a reusable CLI wrapper.
func NewCommandTool(basePath string, cfg CommandToolConfig) *CommandTool {
	if cfg.Category == "" {
		cfg.Category = "cli"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &CommandTool{cfg: cfg, basePath: basePath}
}

func (t *CommandTool) Name() string        { return t.cfg.Name }
func (t *CommandTool) Description() string { return t.cfg.Description }
func (t *CommandTool) Category() string    { return t.cfg.Category }
func (t *CommandTool) Parameters() []framework.ToolParameter {
	return []framework.ToolParameter{
		{Name: "args", Type: "array", Required: false, Description: "Arguments passed to the CLI tool."},
		{Name: "stdin", Type: "string", Required: false, Description: "Optional standard input piped to the command."},
		{Name: "working_directory", Type: "string", Required: false, Description: "Directory to run the command in (relative to workspace)."},
	}
}

func (t *CommandTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	userArgs, err := toStringSlice(args["args"])
	if err != nil {
		return nil, err
	}
	finalArgs := append([]string{}, t.cfg.DefaultArgs...)
	finalArgs = append(finalArgs, userArgs...)
	workdir := t.basePath
	if raw, ok := args["working_directory"]; ok && raw != nil {
		path := fmt.Sprint(raw)
		if path != "" {
			workdir = resolvePath(t.basePath, path)
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, t.cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, t.cfg.Command, finalArgs...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if raw, ok := args["stdin"]; ok && raw != nil {
		cmd.Stdin = strings.NewReader(fmt.Sprint(raw))
	}
	err = cmd.Run()
	success := err == nil
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	return &framework.ToolResult{
		Success: success,
		Data: map[string]interface{}{
			"stdout": stdout.String(),
			"stderr": stderr.String(),
		},
		Error: errMsg,
		Metadata: map[string]interface{}{
			"command":  t.cfg.Command,
			"args":     finalArgs,
			"work_dir": cmd.Dir,
		},
	}, nil
}

func (t *CommandTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	_, err := exec.LookPath(t.cfg.Command)
	return err == nil
}

func toStringSlice(value interface{}) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case []string:
		return v, nil
	case []interface{}:
		res := make([]string, 0, len(v))
		for _, item := range v {
			res = append(res, fmt.Sprint(item))
		}
		return res, nil
	default:
		return nil, fmt.Errorf("expected array for args, got %T", value)
	}
}

func resolvePath(base, path string) string {
	if base == "" {
		return filepath.Clean(path)
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}
