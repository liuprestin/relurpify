# Architecture Outline

This document supplements the GoDoc pages and explains the moving pieces
behind the agentic runtime shipped in this repository. Use it as a high-level
tour before diving into individual packages.

## Runtime Layers

1. **Entry points (`cmd/server`, `cmd/relurpify`)** – bootstrap the tool registry,
   memory stores, language servers, and HTTP/LSP front-ends. They translate
   editor or CLI requests into `framework.Task` instances.
2. **Server package (`server/*`)** – bridges the entry points and the
   framework. It owns the lifecycle of the LSP/HTTP servers and wires agent
   factories, persistence stores, and observability hooks together.
3. **Framework core (`framework/*`)** – provides graph execution, shared
   context, telemetry, memory, and tool abstractions. Agents build workflows
   out of these primitives.
4. **Agents (`agents/*`)** – implement strategies such as planning,
   reflection, and coding. They orchestrate LLM calls plus tool usage by
   instantiating graphs of `framework.Node`s.
5. **Tools (`tools/*`)** – wrap LSP proxies, filesystem operations, git,
   test runners, and shell utilities. Agents discover them via the
   `ToolRegistry` and call them through a uniform interface.
6. **LLM client (`llm/ollama.go`)** – talks to the configured Ollama endpoint
   and satisfies the `framework.LanguageModel` contract, so every agent can
   swap models without changing business logic.
7. **Persistence (`persistence/*`)** – stores workflow snapshots, message
   logs, and vector memory. These stores make long-running, pause/resume
   workflows possible.

## Execution Flow

```
Editor/CLI --> server.LSPServer / server.APIServer
           --> server.AgentFactory selects an Agent
           --> Agent.BuildGraph wires nodes and tools
           --> framework.Graph.Execute runs nodes
           --> tools + llm clients perform side effects
           --> results streamed back through the server
```

Key points:

- `framework.Context` is cloned per parallel branch and merged after each
  `Graph` edge, keeping concurrent updates deterministic.
- Telemetry (`framework.Telemetry`) emits events around every node start,
  finish, and error, enabling live dashboards or lightweight logging.
- Memories captured via `framework.MemoryStore` travel with the task and are
  persisted outside the graph, so future runs can recall successful plans.

## Package Highlights

- `framework/context.go` – Implements the thread-safe state container agents
  share. It tracks execution phase, scratch variables, and interaction history.
- `framework/graph.go` – Deterministic workflow engine with optional parallel
  branches, snapshots for pause/resume, and telemetry hooks.
- `agents/planner.go` – Demonstrates a full plan → execute → verify loop,
  storing structured plans and execution summaries back into the context.
- `agents/coder.go` – Pairs the coding agent with reflection logic to keep
  iterations tight when modifying real files.
- `tools/lsp_process_client.go` – Launches and multiplexes external language
  servers so the agents can request definitions, references, or formatting.
- `persistence/workflow_store.go` – Persists `GraphSnapshot`s so interrupted
  workflows can resume exactly where they stopped.

## Security & Compliance

- **Sandbox runtime** – `framework/sandbox.go` enforces gVisor (`runsc`) usage and
  validates Docker/containerd integration before any agent can execute.
- **Permission manager** – `framework/permissions.go` implements the default-deny
  policy. Every tool declares a permission manifest, which the manager checks
  against the agent's `agent.manifest.yaml` and the active workspace scope. All
  filesystem, network, IPC, capability, and executable operations pass through
  this middleware.
- **Agent manifest** – `framework/manifest.go` and the root `agent.manifest.yaml`
  require explicit permission declarations, resource limits, and audit settings
  before registration proceeds.
- **HITL approvals** – `framework/hitl.go` introduces structured permission
  requests, grant scopes (one-time/session/persistent/conditional), and approval
  tracking with timeout controls.
- **Audit logging** – `framework/audit.go` emits structured JSON logs for every
  action, permission request, and sandbox decision. The CLI surfaces queries via
  the runtime APIs so operations teams can satisfy compliance requirements.

## Extending the System

- **New agents** – Implement the `framework.Agent` interface, register them in
  the factory, and reuse the context/memory primitives to stay interoperable.
- **New tools** – Implement `framework.Tool`, register it with the shared
  registry, and optionally expose it to LLMs via the planner's plan schema.
- **Different models** – Provide a struct that satisfies
  `framework.LanguageModel` and inject it into the server/CLI configuration.
- **Custom persistence** – Swap `persistence.NewFileWorkflowStore` or the
  memory store with your own implementation to integrate databases or vector
  services.

## Documentation Strategy

`./scripts/gen-docs.sh` uses the `golds` tool to generate Go API docs and then
publishes this architecture outline alongside the package pages. This ensures
newcomers land on both the code-level docs and the big-picture explanation
whenever the static site is regenerated.
