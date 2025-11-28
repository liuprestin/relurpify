package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// GitCommandTool executes predefined git commands.
type GitCommandTool struct {
	RepoPath string
	Command  string
}

func (t *GitCommandTool) Name() string { return "git_" + t.Command }

func (t *GitCommandTool) Description() string {
	switch t.Command {
	case "diff":
		return "Shows changes in the working tree."
	case "history":
		return "Retrieves git history for a file."
	case "branch":
		return "Creates a new branch."
	case "commit":
		return "Creates a commit (without pushing)."
	case "blame":
		return "Shows blame information."
	default:
		return "Git command"
	}
}

func (t *GitCommandTool) Category() string { return "git" }

func (t *GitCommandTool) Parameters() []framework.ToolParameter {
	switch t.Command {
	case "history":
		return []framework.ToolParameter{
			{Name: "file", Type: "string", Required: true},
			{Name: "limit", Type: "int", Required: false, Default: 5},
		}
	case "branch":
		return []framework.ToolParameter{{Name: "name", Type: "string", Required: true}}
	case "commit":
		return []framework.ToolParameter{
			{Name: "message", Type: "string", Required: true},
			{Name: "files", Type: "array", Required: false},
		}
	case "blame":
		return []framework.ToolParameter{
			{Name: "file", Type: "string", Required: true},
			{Name: "start", Type: "int", Required: false, Default: 1},
			{Name: "end", Type: "int", Required: false, Default: 1},
		}
	default:
		return []framework.ToolParameter{}
	}
}

func (t *GitCommandTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	if !t.IsAvailable(ctx, state) {
		return nil, fmt.Errorf("git repository not detected")
	}
	switch t.Command {
	case "diff":
		return t.runGit(ctx, []string{"diff"})
	case "history":
		file := fmt.Sprint(args["file"])
		limit := toInt(args["limit"])
		if limit == 0 {
			limit = 5
		}
		return t.runGit(ctx, []string{"log", fmt.Sprintf("-n%d", limit), "--oneline", "--", file})
	case "branch":
		name := fmt.Sprint(args["name"])
		return t.runGit(ctx, []string{"checkout", "-b", name})
	case "commit":
		message := fmt.Sprint(args["message"])
		filesAny, ok := args["files"].([]string)
		if ok && len(filesAny) > 0 {
			if _, err := t.runGit(ctx, append([]string{"add"}, filesAny...)); err != nil {
				return nil, err
			}
		} else {
			if _, err := t.runGit(ctx, []string{"add", "--all"}); err != nil {
				return nil, err
			}
		}
		return t.runGit(ctx, []string{"commit", "-m", message})
	case "blame":
		file := fmt.Sprint(args["file"])
		start := toInt(args["start"])
		end := toInt(args["end"])
		rangeArg := fmt.Sprintf("-L%d,%d", start, end)
		return t.runGit(ctx, []string{"blame", rangeArg, file})
	default:
		return nil, fmt.Errorf("unsupported git command %s", t.Command)
	}
}

func (t *GitCommandTool) runGit(ctx context.Context, args []string) (*framework.ToolResult, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = t.RepoPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("git %s failed: %s", strings.Join(args, " "), stderr.String())
	}
	return &framework.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"output": stdout.String(),
			"time":   time.Now().UTC(),
		},
	}, nil
}

func (t *GitCommandTool) IsAvailable(ctx context.Context, state *framework.Context) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = t.RepoPath
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func (t *GitCommandTool) Permissions() framework.ToolPermissions {
	return framework.ToolPermissions{Permissions: framework.NewExecutionPermissionSet(t.RepoPath, "git", []string{"*"})}
}
