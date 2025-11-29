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
export OLLAMA_MODEL=deepseek-r1:7b

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
