# Relurpify

 Relurpify is an extensible Go framework that orchestrates planning agents,
  reasoning graphs, and IDE-facing tools to accelerate code modifications. 
  It exposes an HTTP API, an editor-friendly LSP wrapper, and a CLI so you can embed 
  the same automation stack in multiple environments.

Experimental project for learning purposes and for local agenic automation ; whose sole goal 
is to one day re-write itself.

---

## Repository Layout

```
framework/   Graph runtime, shared context, memory, telemetry, tool registry
agents/      Planner, coder, reflection, and ReAct-inspired orchestrators
tools/       File, git, search, execution, and LSP proxy implementations
cmd/         CLI entry points (server, relurpify toolbox, coder helper)
server/      HTTP + LSP servers and dependency wiring
persistence/ Workflow + message stores for pause/resume and logging
llm/         Ollama HTTP client that satisfies framework.LanguageModel
scripts/     Helper scripts (documentation generation, etc.)
```

Use `ARCHITECTURE.md` for a high-level diagram and data-flow outline. The generated docs website (via `scripts/gen-docs.sh`) bundles that outline next to the Golds API pages.

---

## Prerequisites

- **Go 1.21+**
- **Local Ollama instance** (or an HTTP-compatible endpoint) with a code-capable model such as `codellama`
- **golds** documentation tool (optional, only required for static docs): `go install go101.org/golds@latest`

In sandboxed environments you can keep module/cache directories inside the repo:

```bash
export GOMODCACHE=$PWD/.gomodcache
export GOCACHE=$PWD/.gocache
```

---

## Build, Run, and Test

### Install dependencies

```bash
go mod tidy
```

### Build everything

```bash
go build ./...
```

### Run the full test suite

```bash
go test ./...
```

### Launch the HTTP server

```bash
export OLLAMA_ENDPOINT=http://localhost:11434
export OLLAMA_MODEL=codellama

go run ./cmd/server
```

The server exposes `POST /api/task`. Example request:

```bash
curl -s http://localhost:8080/api/task \
  -H 'Content-Type: application/json' \
  -d '{
    "instruction": "Summarize README.md and list missing tests",
    "type": "analysis",
    "context": {"path": "README.md"}
  }' | jq
```

### Use the CLI toolbox instead of the raw server

```bash
# Serve the same API but with CLI-configured model + memory paths
go run ./cmd/relurpify serve --workspace . --addr :8080

# Run a one-off task
go run ./cmd/relurpify task \
  --instruction "Add logging to framework/context.go" \
  --type code_modification

# List persisted workflow snapshots (pause/resume state)
go run ./cmd/relurpify workflow list

# Inspect shared memory entries
go run ./cmd/relurpify memory list
```

### Autodetect tooling & launch the interactive shell

The new `relurpify setup` command probes your machine for supported language servers, queries the local Ollama endpoint for available models, and writes a shared config to `.relurpify/config.json` inside the workspace. Other entry points (including the shell) reuse that file so you only have to detect once.

```
# Detect tooling for the current workspace and save .relurpify/config.json
go run ./cmd/relurpify setup --workspace .
```

`setup` reports which LSP binaries were found, how many matching files live in the repo, and whether it could reach the configured Ollama endpoint. You can inspect/update the config manually, but the interactive shell is usually more convenient:

```
# Start the agenic shell with the autodetected environment
go run ./cmd/relurpify shell --workspace .

# Inside the shell:
models                    # list Ollama models and the current default
use codellama             # switch models (persisted back to the config)
lsps                      # show LSP availability + detected languages
task Summarize README.md  # run a one-off coding/analysis task
write Build a hello world  # generate a new file or scaffolding
apply lang=go main.go :: add logging around foo()
detect                    # re-run detection if you install new tooling
exit
```

The shell wires itself up using the recorded LSP servers, workspace root, and Ollama endpoint/model so every task/`apply` run behaves like the dedicated CLI helpers while keeping the context in one session.

On startup the shell will prompt for a workspace directory (creating it if needed) and ask you to choose which detected Ollama model to use. Behind the scenes a persistent toolchain manager keeps language servers alive across commands, so once a Go/Rust/etc. proxy launches, subsequent tasks reuse the same process instead of incurring startup cost. The active tooling summary is stored inside the agent context so coding workflows know exactly which helpers are available.

The `.relurpify/config.json` file now tracks a `languages` array (autofilled from file extensions it sees). On first launch the shell asks which languages you want active so you can trim/expand the list. If you later run `apply` with a new language (for example by setting `--lang rust`), the shell records it automatically, warms the matching LSP, and persists the change for future sessions.

Tool-calling can also be toggled via the `tool_calling` flag. When it remains disabled, the shell automatically swaps to a manual coder agent that parses JSON (or markdown) edit plans from the model and writes files itself—so models without function calling still create real files even if they don’t emit perfect JSON. You can edit the config manually or rerun the shell to change the setting.

### Generate documentation (HTML site + architecture outline)

```bash
./scripts/gen-docs.sh
open docs/index.html   # or serve docs/ via any static file server
```

---

## Running Agents inside Editors

The [`server/lsp_server.go`](server/lsp_server.go) package adapts the framework to language-server events:

1. Editors trigger custom commands (e.g., “AI fix”, “AI refactor”).
2. The LSP server collects document context and forwards it as a `framework.Task`.
3. The configured agent (default: coding agent with reflection) builds a graph, invokes tools/LLMs, and streams edits back.

Use `go run ./cmd/coder apply --file path --instruction "..."` for a Cursor-like CLI workflow that mirrors the LSP commands when you do not have an editor integration handy.

---

## Tooling Quick Start

Agents discover capabilities through the `framework.ToolRegistry`. The CLI already registers every default tool via `cmd/internal/cliutils.BuildToolRegistry`, but you can experiment manually:

```go
registry := cliutils.BuildToolRegistry(workspace)
ctx := context.Background()
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

---

## Recommended Workflow

1. **Start Ollama** locally and load the model you plan to use.
2. **Run `go run ./cmd/relurpify serve`** so editors and scripts can talk to the HTTP API.
3. **Iterate in your editor** and watch the agent’s interaction history (`framework.Context`) for debugging.
4. **Capture docs** with `./scripts/gen-docs.sh` when the API surface changes so the static site and `ARCHITECTURE.md` stay fresh.
5. **Commit results** using the git tools (either through the agent or manually) and push as usual.
