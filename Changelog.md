# 0.7

Language-Aware Analysis

Added an exported CodingAnalysis struct plus language/file-aware prompting so the analyze node now gathers file hints from the task/state, selects a language, and parses the LLM output into structured plan/files/risks data that get stored in the shared context for later consumers (agents/coder.go (line 22), agents/coder.go (line 104), agents/coder.go (line 144)).
Introduced helpers to collect file paths from task/context, infer languages from extensions, and normalize the JSON payload so even mixed string/object lists are converted into deterministic string slices (agents/coder.go (line 210), agents/coder.go (line 248), agents/coder.go (line 276)).
The CLI entry points now feed both the detected language and explicit file lists into the task context, ensuring the coding agent always has the necessary metadata even when run outside the LSP (cmd/coder/main.go (line 114), cmd/relurpify/main.go (line 726)).
LSP-originated requests also pass their document language and URI as part of the task context, keeping editor flows in sync with the new analysis expectations (server/lsp_server.go (line 131), server/lsp_server.go (line 182)).

# 0.6

- documentation pass
- interactive coder-cli
- setup automation

# 0.5

What’s New

Split the command-line tooling into a dedicated framework CLI (cmd/relurpify) and a new coding-focused CLI (cmd/coder, i.e. relurpify-coder). Both use Cobra, share workspace/model flags, and can be run in-place via go run.
Added cmd/internal/cliutils to centralize common helpers: building the default tool registry, wiring LSP-backed tools, inferring languages by extension, and spinning up/tearing down process-based LSP proxies.
Extended the LSP client wrappers with a Close() method so CLIs (and future tooling) can cleanly shut down language servers.
Updated README.md with the expanded CLI tooling instructions, including examples for the relurpify CLI and the new cursor-style relurpify-coder apply command.
relurpify-coder’s apply subcommand reads a target file, auto-detects/optionally overrides the language, registers the appropriate LSP client + tools, and runs the coding agent stack to apply the instruction—mirroring a cursor-cli workflow.
The relurpify CLI now leverages the shared cliutils package for registry/LSP handling, keeping the framework utilities and coding utilities decoupled.

Environment Autodetect & Shell

Introduced cmd/internal/setup along with a new `relurpify setup` command that probes for supported LSP binaries, checks the local Ollama endpoint/models, and writes a reusable `.relurpify/config.json` for the rest of the tooling.
Extended the LSP descriptor metadata with command hints so the detector can report which servers are installed and which languages exist in the workspace.
Added `relurpify shell`, an interactive agenic console that loads the shared config, lets you re-run detection, list/switch Ollama models, inspect LSP availability, and launch coding/analysis/apply flows without leaving the REPL.


# 0.4

LSP Client Wrappers

Added tools/lsp_process_client.go, a generic stdio JSON-RPC bridge that launches an external language server process, performs the LSP handshake, keeps documents in sync, collects diagnostics, and implements every method of the existing LSPClient interface (definition, references, hover, diagnostics, search, document symbols, formatting).
Built convenience constructors for the requested servers: NewRustAnalyzerClient, NewGoplsClient, NewClangdClient, NewHaskellClient, NewTypeScriptClient, NewLuaClient, and NewPythonLSPClient. Each helper sets the right command/args and language ID, so you can register them with tools.Proxy and rely on the shared implementation.
Protocol & JSON-RPC Integration

Chose go.lsp.dev/protocol for strongly typed LSP data structures and github.com/sourcegraph/jsonrpc2 for VSCode-style header streams. Added helpers for URI conversion, snippet extraction, and recursive symbol flattening to make the wrappers reusable.
Diagnostics arrive via textDocument/publishDiagnostics, and the client caches the most recent set per URI while GetDiagnostics waits briefly for a notification. Other requests are dispatched directly via conn.Call.
Docs & Build

Highlighted the new multi-language LSP clients in README.md.
Ran go test ./... (with repo-local module/cache dirs) to ensure the new code compiles cleanly.

# 0.3

Test Suite Enhancements

Added Testify (github.com/stretchr/testify) to go.mod/go.sum and built comprehensive unit tests across major packages:
agents/react_test.go: covers the ReAct nodes end-to-end via stub LLM/tools, asserting final state handling.
tools/files_test.go: exercises read/write/list/search tooling on temp directories.
llm/ollama_test.go: uses a custom RoundTripper to validate Generate/Chat request shaping and response parsing without network sockets.
server/api_test.go: verifies the HTTP handler path for /api/task using a stub agent.
Existing framework/persistence tests remain and run alongside the new suites.
Result

go test ./... now executes meaningful coverage across agents, framework, persistence, server, tools, and llm packages (cmd/server still intentionally empty).
Local module/test caches are maintained via .gomodcache/.gocache when running the command.

# 0.2

State & Storage

Added durable workflow/message/vector stores under persistence/ (workflow_store.go (line 1), message_store.go (line 1), vector_store.go (line 1)) so executions can be snapshotted, conversations replayed, and semantic recall performed via the new in-memory vector index.
Documented how to wire these stores (and the helper script/tests) in README.md (line 3).
Observability

Introduced framework/telemetry.go (line 1) and instrumented the graph runner (framework/graph.go (line 1)) to emit start/finish/error events per graph/node. Plug in LoggerTelemetry (or a custom emitter) for tracing/monitoring.
Testability & Debugging

Added focused unit tests for context snapshots and graph execution flow (framework/context_test.go (line 1), framework/graph_test.go (line 1)) plus coverage for the vector store (persistence/vector_store_test.go (line 1)) to make regression checks easy.
go test ./... now exercises these suites (see below).
Tests

GOMODCACHE=$PWD/.gomodcache GOCACHE=$PWD/.gocache go test ./...
Next ideas: persist telemetry to a log aggregator, plug the workflow/message stores into cmd/server endpoints, or add more agent-level tests once you decide on priority workflows.

# 0.1

Built the core orchestration layer with a validated graph engine, resumable snapshots, node implementations, and tool/LLM abstractions (framework/graph.go (line 10), framework/tools.go (line 10)). The shared context and hybrid memory now provide thread-safe state tracking, history snapshots, and persistence hooks for each scope (framework/context.go (line 12), framework/memory.go (line 14)).

Added concrete tooling that fulfills the required capabilities: multi-language LSP proxy actions with caching (tools/lsp.go (line 1)), file operations with safety guards (tools/files.go (line 1)), git/lint/build/test execution helpers (tools/git.go (line 1), tools/execution.go (line 1)), and code-search utilities (tools/search.go (line 1)).

Implemented the complete agent stack: ReAct loops with JSON tool selection (agents/react.go (line 13)), planner/verification pipelines (agents/planner.go (line 11)), reflection/self-correction wrapper (agents/reflection.go (line 11)), and a coding-specialized workflow that chains analysis, modification, and validation on top of ReAct (agents/coder.go (line 10), helper UUID generator in agents/util.go (line 1)).

Delivered integration layers so the framework can talk to a local Ollama server and editors: the Ollama client now satisfies framework.LanguageModel for completions, chat, streaming, and tool calls (llm/ollama.go (line 15)), the LSP server/API wire editor events to agents and expose HTTP endpoints for testing (server/lsp_server.go (line 16), server/api.go (line 13)).
Rewrote the README with repository layout, onboarding steps, command references, and the full architecture flow so contributors can stand up the system quickly (README.md (line 3)).
Tests

GOMODCACHE=$PWD/.gomodcache GOCACHE=$PWD/.gocache go test ./...
Next Steps

Implement real JSON-RPC LSP client adapters inside tools.Proxy so the proxies talk to gopls, pyright, etc., instead of the current interface stubs.

Add a cmd/server entrypoint that wires tool registrations, Ollama client creation, and the API/LSP servers into an executable for manual or editor-driven runs.
