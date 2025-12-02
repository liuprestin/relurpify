
---

## Tooling Quick Start

Agents discover capabilities through the `framework.ToolRegistry`. Every CLI entry point now registers tools only after a manifest has been validated and a gVisor sandbox runner is available. You can mirror that flow manually:

```go
ctx := context.Background()
registration, _ := framework.RegisterAgent(ctx, framework.RuntimeConfig{
    ManifestPath: filepath.Join(workspace, "relurpify_cfg", "agent.manifest.yaml"),
    Sandbox:      framework.SandboxConfig{RunscPath: "runsc", ContainerRuntime: "docker", NetworkIsolation: true},
    BaseFS:       workspace,
})
runner, _ := framework.NewSandboxCommandRunner(registration.Manifest, registration.Runtime, workspace)
registry, _ := runtime.BuildToolRegistry(workspace, runner)
registry.UsePermissionManager(registration.ID, registration.Permissions)

state := framework.NewContext()
tool, _ := registry.Get("file_read")
result, _ := tool.Execute(ctx, state, map[string]interface{}{"path": "README.md"})
fmt.Println(result.Data["content"])
```


The table below lists each built-in tool with an example argument payload. Plug any row into the snippet above by swapping the tool name and the `map[string]interface{}{...}` body.

| Tool name | Description | Example args |
|-----------|-------------|--------------|
| `file_read` | Read a UTF-8 file | `{"path": "README.md"}` |
| `file_write` | Write content (creates backup first) | `{"path": "notes/todo.md", "content": "add docs"}` |
| `file_list` | Recursively list files | `{"directory": "agents", "pattern": "*.go"}` |
| `file_search` | Search for a substring | `{"directory": "framework", "pattern": "Context"}` |
| `file_create` | Create a brand-new file | `{"path": "docs/new.md", "content": "draft"}` |
| `file_delete` | Move a file into `.trash` | `{"path": "scratch/tmp.txt"}` |
| `git_diff` | Show working tree diff | `{}` |
| `git_history` | Show recent commits for a file | `{"file": "README.md", "limit": 3}` |
| `git_branch` | Create and switch to a branch | `{"name": "feature/llm-upgrade"}` |
| `git_commit` | Stage & commit changes | `{"message": "docs: update readme", "files": ["README.md"]}` |
| `git_blame` | Inspect ownership for a range | `{"file": "framework/context.go", "start": 10, "end": 40}` |
| `search_grep` | Substring search across repo | `{"pattern": "NewContext", "directory": "framework"}` |
| `search_find_similar` | Jaccard-based snippet similarity | `{"snippet": "func NewGraph()", "directory": "framework"}` |
| `search_semantic` | Heuristic semantic search | `{"query": "graph snapshot"}` |
| `exec_run_tests` | Run tests (`go test ./...` by default) | `{"pattern": "./agents"}` |
| `exec_run_code` | Run an interpreter (e.g., `python - <<'EOF'`) | `{"code": "print('hello world')"}` |
| `exec_run_linter` | Invoke lint command | `{"path": "./..."}` |
| `exec_run_build` | Compile/build command | `{}` |
| `lsp_get_definition` | Jump to symbol definition | `{"file": "framework/graph.go", "symbol": "Execute", "position": {"line": 120, "character": 2}}` |
| `lsp_get_references` | List references | `{"file": "agents/planner.go", "symbol": "Execute", "position": {"line": 60, "character": 5}}` |
| `lsp_get_hover` | Show type info/docs | `{"file": "framework/context.go", "position": {"line": 27, "character": 3}}` |
| `lsp_get_diagnostics` | Report lint errors for a file | `{"file": "agents/planner.go"}` |
| `lsp_search_symbols` | Workspace-wide symbol search | `{"query": "Planner"}` |
| `lsp_document_symbols` | Outline symbols in a file | `{"file": "framework/context.go"}` |
| `lsp_format` | Format code using the registered language server | `{"file": "agents/planner.go", "code": "<current file contents>"}` |

> **Tip:** For tools expecting nested structs (like `position`), you can build the argument map inline as demonstrated above or marshal a JSON object from user input before invoking `tool.Execute`.
