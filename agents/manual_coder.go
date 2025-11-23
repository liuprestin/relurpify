package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lexcodex/relurpify/framework"
)

// ManualCodingAgent handles coding tasks without LLM tool-calling support.
type ManualCodingAgent struct {
	Model  framework.LanguageModel
	Tools  *framework.ToolRegistry
	Config *framework.Config
}

// Initialize sets defaults.
func (a *ManualCodingAgent) Initialize(cfg *framework.Config) error {
	a.Config = cfg
	if a.Tools == nil {
		a.Tools = framework.NewToolRegistry()
	}
	return nil
}

// Capabilities mirrors the standard coding agent.
func (a *ManualCodingAgent) Capabilities() []framework.Capability {
	return []framework.Capability{
		framework.CapabilityCode,
		framework.CapabilityExplain,
		framework.CapabilityExecute,
	}
}

// Execute builds and runs the manual coding graph.
func (a *ManualCodingAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	graph, err := a.BuildGraph(task)
	if err != nil {
		return nil, err
	}
	return graph.Execute(ctx, state)
}

// BuildGraph wires analysis -> generation -> apply -> validate.
func (a *ManualCodingAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	if a.Model == nil {
		return nil, fmt.Errorf("manual coding agent missing model")
	}
	graph := framework.NewGraph()
	analyze := &manualAnalyzeNode{id: "manual_analyze", agent: a, task: task}
	generate := &manualGenerateNode{id: "manual_generate", agent: a, task: task}
	apply := &manualApplyNode{id: "manual_apply", agent: a}
	validate := &manualValidateNode{id: "manual_validate", agent: a}
	done := framework.NewTerminalNode("manual_done")

	for _, node := range []framework.Node{analyze, generate, apply, validate, done} {
		if err := graph.AddNode(node); err != nil {
			return nil, err
		}
	}
	if err := graph.SetStart(analyze.ID()); err != nil {
		return nil, err
	}
	edges := [][2]string{
		{analyze.ID(), generate.ID()},
		{generate.ID(), apply.ID()},
		{apply.ID(), validate.ID()},
		{validate.ID(), done.ID()},
	}
	for _, e := range edges {
		if err := graph.AddEdge(e[0], e[1], nil, false); err != nil {
			return nil, err
		}
	}
	return graph, nil
}

type manualAnalyzeNode struct {
	id    string
	agent *ManualCodingAgent
	task  *framework.Task
}

func (n *manualAnalyzeNode) ID() string               { return n.id }
func (n *manualAnalyzeNode) Type() framework.NodeType { return framework.NodeTypeObservation }

func (n *manualAnalyzeNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("analysis")
	files := gatherTaskFiles(n.task, state)
	language := detectTaskLanguage(n.task, state, files)
	prompt := fmt.Sprintf(`You are analyzing a coding task without automated tools.
Task: "%s"
Language: %s
Known files: %s
Respond with JSON {"plan":["step1","step2"],"files":["file1","file2"],"risks":["..."]}`, n.task.Instruction, language, strings.Join(files, ", "))
	resp, err := n.agent.Model.Generate(ctx, prompt, &framework.LLMOptions{
		Model:       n.agent.Config.Model,
		Temperature: 0.1,
		MaxTokens:   400,
	})
	if err != nil {
		return nil, err
	}
	state.AddInteraction("assistant", resp.Text, map[string]interface{}{"node": n.id})
	analysis, err := parseCodingAnalysis(resp.Text)
	if err != nil {
		analysis = CodingAnalysis{Plan: []string{resp.Text}, Files: files, Raw: resp.Text}
	}
	state.Set("manual.analysis", analysis)
	state.Set("manual.language", language)
	state.Set("manual.files", analysis.Files)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"analysis": analysis}}, nil
}

type ManualAction struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
}

type ManualActionPlan struct {
	Actions []ManualAction `json:"actions"`
	Summary string         `json:"summary"`
}

type manualGenerateNode struct {
	id    string
	agent *ManualCodingAgent
	task  *framework.Task
}

func (n *manualGenerateNode) ID() string               { return n.id }
func (n *manualGenerateNode) Type() framework.NodeType { return framework.NodeTypeObservation }

func (n *manualGenerateNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("generation")
	analysisVal, _ := state.Get("manual.analysis")
	analysis, _ := analysisVal.(CodingAnalysis)
	workspace := workspaceFromState(state, n.task)
	activeFiles := strings.Join(analysis.Files, ", ")
	if activeFiles == "" {
		activeFiles = "(none provided)"
	}
	prompt := fmt.Sprintf(`You must produce concrete file edits without relying on tool calls.
Workspace root: %s
Known files: %s
Plan: %s

Respond with compact JSON:
{"actions":[{"path":"relative/or absolute path","mode":"create|replace","content":"full file content"}],"summary":"short description"}`, workspace, activeFiles, strings.Join(analysis.Plan, "; "))
	resp, err := n.agent.Model.Generate(ctx, prompt, &framework.LLMOptions{
		Model:       n.agent.Config.Model,
		Temperature: 0.2,
		MaxTokens:   700,
	})
	if err != nil {
		return nil, err
	}
	state.AddInteraction("assistant", resp.Text, map[string]interface{}{"node": n.id})
	plan, err := parseManualPlan(resp.Text)
	if err != nil {
		return nil, err
	}
	state.Set("manual.plan", plan)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"plan": plan}}, nil
}

type manualApplyNode struct {
	id    string
	agent *ManualCodingAgent
}

func (n *manualApplyNode) ID() string               { return n.id }
func (n *manualApplyNode) Type() framework.NodeType { return framework.NodeTypeSystem }

func (n *manualApplyNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("applying")
	planVal, ok := state.Get("manual.plan")
	if !ok {
		return nil, fmt.Errorf("missing manual plan")
	}
	plan, _ := planVal.(ManualActionPlan)
	if len(plan.Actions) == 0 {
		return &framework.Result{NodeID: n.id, Success: true}, nil
	}
	var written []string
	for _, action := range plan.Actions {
		if err := n.writeAction(ctx, state, action); err != nil {
			return nil, err
		}
		written = append(written, action.Path)
	}
	state.Set("manual.applied_files", written)
	return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"files": written}}, nil
}

func (n *manualApplyNode) writeAction(ctx context.Context, state *framework.Context, action ManualAction) error {
	path := action.Path
	if path == "" {
		if val, ok := state.Get("active.file"); ok {
			if file, ok := val.(string); ok {
				path = file
			}
		}
	}
	if path == "" {
		if val, ok := state.Get("manual.files"); ok {
			switch files := val.(type) {
			case []string:
				if len(files) > 0 {
					path = files[0]
				}
			case interface{}:
				if list, ok := files.([]interface{}); ok && len(list) > 0 {
					if str, ok := list[0].(string); ok {
						path = str
					}
				}
			}
		}
	}
	if path == "" {
		path = defaultManualFilename(state)
	}
	if path == "" {
		return fmt.Errorf("action missing path")
	}
	if !filepath.IsAbs(path) {
		root := workspaceFromState(state, nil)
		if root != "" {
			rootBase := filepath.Base(root)
			prefix := rootBase + string(filepath.Separator)
			cleaned := filepath.Clean(path)
			if strings.HasPrefix(cleaned, prefix) {
				cleaned = strings.TrimPrefix(cleaned, prefix)
			}
			path = filepath.Join(root, cleaned)
		}
	}
	content := action.Content
	if content == "" {
		return fmt.Errorf("action for %s missing content", action.Path)
	}
	if err := ensureDir(path); err != nil {
		return err
	}
	tool, ok := n.agent.Tools.Get("file_write")
	if !ok {
		return fmt.Errorf("file_write tool unavailable")
	}
	args := map[string]interface{}{
		"path":    path,
		"content": content,
	}
	_, err := tool.Execute(ctx, state, args)
	return err
}

type manualValidateNode struct {
	id    string
	agent *ManualCodingAgent
}

func (n *manualValidateNode) ID() string               { return n.id }
func (n *manualValidateNode) Type() framework.NodeType { return framework.NodeTypeObservation }

func (n *manualValidateNode) Execute(ctx context.Context, state *framework.Context) (*framework.Result, error) {
	state.SetExecutionPhase("validating")
	tool, ok := n.agent.Tools.Get("exec_run_tests")
	if !ok {
		state.Set("manual.tests", "skipped")
		return &framework.Result{NodeID: n.id, Success: true, Data: map[string]interface{}{"tests": "skipped"}}, nil
	}
	res, err := tool.Execute(ctx, state, map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	state.Set("manual.tests", res.Data)
	return &framework.Result{NodeID: n.id, Success: res.Success, Data: res.Data, Error: toolErr(res.Error)}, nil
}

func parseManualPlan(raw string) (ManualActionPlan, error) {
	snippet := stripCodeFence(extractJSONSnippet(raw))
	if snippet == "" {
		return ManualActionPlan{}, fmt.Errorf("manual plan missing JSON body")
	}
	safeSnippet := escapeNewlinesInStrings(snippet)
	var plan ManualActionPlan
	if err := json.Unmarshal([]byte(safeSnippet), &plan); err != nil {
		clean := escapeNewlinesInStrings(stripTrailingCommas(safeSnippet))
		if clean == safeSnippet {
			return ManualActionPlan{}, err
		}
		if err := json.Unmarshal([]byte(clean), &plan); err != nil {
			return ManualActionPlan{}, err
		}
	}
	if len(plan.Actions) == 0 {
		return ManualActionPlan{}, fmt.Errorf("manual plan has no actions")
	}
	for i := range plan.Actions {
		if plan.Actions[i].Mode == "" {
			plan.Actions[i].Mode = "replace"
		}
	}
	return plan, nil
}

func workspaceFromState(state *framework.Context, task *framework.Task) string {
	if state != nil {
		if v, ok := state.Get("workspace.root"); ok {
			if root, ok := v.(string); ok && root != "" {
				if abs, err := filepath.Abs(root); err == nil {
					return abs
				}
				return root
			}
		}
	}
	if task != nil && task.Context != nil {
		if root, ok := task.Context["workspace"].(string); ok && root != "" {
			if abs, err := filepath.Abs(root); err == nil {
				return abs
			}
			return root
		}
	}
	return ""
}

func stripCodeFence(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return body
	}
	if strings.HasPrefix(body, "```") {
		body = strings.TrimPrefix(body, "```")
		newline := strings.IndexRune(body, '\n')
		if newline >= 0 {
			body = body[newline+1:]
		}
		if idx := strings.LastIndex(body, "```"); idx >= 0 {
			body = body[:idx]
		}
	}
	return strings.TrimSpace(body)
}

func stripTrailingCommas(body string) string {
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		ch := body[i]
		if ch == ',' {
			j := i + 1
			for j < len(body) && (body[j] == ' ' || body[j] == '\n' || body[j] == '\r' || body[j] == '\t') {
				j++
			}
			if j < len(body) && (body[j] == ']' || body[j] == '}') {
				continue
			}
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func ensureDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func escapeNewlinesInStrings(src string) string {
	var b strings.Builder
	inString := false
	escaped := false
	for i := 0; i < len(src); i++ {
		ch := src[i]
		if ch == '"' && !escaped {
			inString = !inString
		}
		if inString && ch == '\n' && !escaped {
			b.WriteString("\\n")
			continue
		}
		b.WriteByte(ch)
		if ch == '\\' && !escaped {
			escaped = true
		} else {
			escaped = false
		}
	}
	return b.String()
}

func defaultManualFilename(state *framework.Context) string {
	root := workspaceFromState(state, nil)
	lang := ""
	if val, ok := state.Get("manual.language"); ok {
		if s, ok := val.(string); ok {
			lang = s
		}
	}
	ext := manualExtensionForLang(lang)
	name := fmt.Sprintf("manual_output%s", ext)
	if root == "" {
		return name
	}
	return filepath.Join(root, name)
}

func manualExtensionForLang(lang string) string {
	switch strings.ToLower(lang) {
	case "go":
		return ".go"
	case "rust", "rs":
		return ".rs"
	case "clangd", "c", "cpp", "cxx":
		return ".c"
	case "ts", "typescript":
		return ".ts"
	case "js", "javascript":
		return ".js"
	case "python", "py":
		return ".py"
	case "lua":
		return ".lua"
	case "haskell", "hs":
		return ".hs"
	default:
		return ".txt"
	}
}
