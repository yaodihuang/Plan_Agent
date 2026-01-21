# Contributing to dev_agent

This project is intentionally built for experienced Go developers who are comfortable working with MCP-powered automation and Azure OpenAI. The sections below document how to get a workstation ready, how the orchestrator is structured, and the expectations for code quality, review, and reporting.

## Prerequisites & Environment

- **Toolchain**: Go 1.21.x (the module is tested with 1.21; newer versions should be module-compatible but verify with `go test ./...`). Install via `asdf`, `gimme`, or your preferred manager and confirm with `go version`.
- **Azure OpenAI**: Required environment variables (loaded via `internal/config.FromEnv`) are `AZURE_OPENAI_API_KEY`, `AZURE_OPENAI_BASE_URL` (`https://<resource>.openai.azure.com`), `AZURE_OPENAI_DEPLOYMENT`, and optionally `AZURE_OPENAI_API_VERSION` (defaults to `2024-12-01-preview`).
- **Pantheon MCP**: Point `MCP_BASE_URL` at your Pantheon endpoint (defaults to `http://localhost:8000/mcp/sse`). Polling knobs are available via `MCP_POLL_INITIAL_SECONDS`, `MCP_POLL_MAX_SECONDS`, `MCP_POLL_TIMEOUT_SECONDS`, and `MCP_POLL_BACKOFF_FACTOR`.
- **Workspace metadata**: `PROJECT_NAME` and (optionally) `WORKSPACE_DIR` (defaults to `/home/pan/workspace`). The orchestrator writes `worklog.md` and `code_review.log` under this directory, so ensure it is writable.
- **Git identity and publishing**: Set `GITHUB_TOKEN`, `GIT_AUTHOR_NAME`, and `GIT_AUTHOR_EMAIL`. Publishing fails fast if these are missing, so configure them before running integration tests.
- **.env convenience**: A `.env` file at the repo root (sibling to this document) is parsed before `FromEnv()` reads `os.Environ`. Only unset variables are overridden, so you can safely mix shell exports with `.env`.

Example `.env` skeleton:

```bash
AZURE_OPENAI_API_KEY=...
AZURE_OPENAI_BASE_URL=https://example-resource.openai.azure.com
AZURE_OPENAI_DEPLOYMENT=gpt-4o
MCP_BASE_URL=http://localhost:8000/mcp/sse
PROJECT_NAME=dev-agent-lab
WORKSPACE_DIR=/home/pan/workspace
GITHUB_TOKEN=ghp_...
GIT_AUTHOR_NAME=Jane Dev
GIT_AUTHOR_EMAIL=jane@example.com
```

## Installation & Build

All source code lives in `./dev_agent`. Keep the repo root (with `AGENTS.md`, `SKILL.md`, etc.) clean and run Go commands from the module directory.

```bash
cd dev_agent
go mod tidy            # pull dependencies / verify go.sum
go build -o bin/dev-agent ./cmd/dev-agent
```

- **Headless vs. chat**: `cmd/dev-agent` optionally prompts interactively. Pass `--headless` for CI/headless tasks; omit it to step through prompts.
- **Streaming / logging**: `--stream-json` enables NDJSON emission (documented in `docs/stream-json.md`) and forces headless mode while suppressing noisy logs (`logx.SetLevel(logx.Error)`). When debugging low-level MCP calls, temporarily set `logx.SetLevel(logx.Debug)` inside `main.go` or insert targeted `logx.Debugf` statements.
- **Quick smoke test**:
  ```bash
  ./bin/dev-agent \
    --task "Smoke-test implement review loop" \
    --parent-branch-id 123e4567-e89b-12d3-a456-426614174000 \
    --project-name dev-agent-lab \
    --headless
  ```
  Use `--stream-json` when building dashboards or verifying instrumentation.

## Development Workflow

The orchestrator enforces an Implement → Review → Fix loop using Pantheon MCP:

1. **Implement (codex)**: `internal/orchestrator` crafts the implement prompt (see `systemPromptTemplate`). `internal/tools.MCPClient.ParallelExplore` drives `execute_agent` to create a new branch lineage.
2. **Review (review_code)**: The review agent inspects the branch. `ToolHandler.executeAgent` retries `read_artifact` against `code_review.log` until it exists, then surfaces P0/P1 issues.
3. **Fix (codex)**: The implementer re-enters with the review log context. The loop runs up to 8 iterations (`maxIterations` in `internal/orchestrator`).
4. **Publish**: After a clean review, `finalizeBranchPush` instructs the agent to commit/push using the GitHub token, and the CLI prints the JSON report plus lineage.

### Working on specific packages

| Package | Key files | Notes |
|---------|-----------|-------|
| `cmd/dev-agent` | `cmd/dev-agent/main.go` | CLI entry point, flag parsing, env hydration, JSON streaming wire-up. |
| `internal/config` | `config.go` | Validates env, enforces polling bounds, loads `.env`. |
| `internal/brain` | `brain.go` | Azure OpenAI client with retries/backoff (set `MaxCompletionTokens`, `Attempts`). |
| `internal/orchestrator` | `orchestrator.go` | System prompt, workflow loop, publish hand-off, instruction builder. |
| `internal/tools` | `mcp.go`, `handler.go` | Pantheon MCP client, tool dispatch, branch tracker, artifact helpers. |
| `internal/streaming` | `json_streamer.go` | NDJSON emitter used when `--stream-json` is on. |

When extending behavior (e.g., new MCP tools or logging), keep the single-call-per-turn rule intact and update both the orchestrator prompts and `ToolHandler` so branch lineage stays consistent.

### Running sample tasks

- **Headless**: `go run ./cmd/dev-agent --task "..." --parent-branch-id <uuid> --headless`.
- **Interactive**: omit `--headless` to let the CLI prompt for the task.
- **Chat loop**: `o.ChatLoop` is still wired for experimentation; pass `--headless=false` and consider instrumenting `internal/orchestrator` for additional telemetry in this mode.

Use realistic Pantheon tasks whenever possible; mocked runs should still respect the worklog/review log contract so downstream tooling (publishing, reporting) functions correctly.

## Testing & Validation

- **Unit tests**: run `go test ./...` from `dev_agent/`. The existing suite focuses on orchestrator instruction handling and tool handler retries; add coverage near any code you touch (e.g., when adding new MCP calls).
- **Focused tests**: `go test ./internal/tools -run TestExecuteAgentReviewCodeRetriesMissingLog` demonstrates how to fake MCP responses via `fakeMCPClient`. Follow that pattern to exercise edge cases without needing a live Pantheon endpoint.
- **Integration against mock MCP**:
  - Spin up an `httptest.Server` (or a lightweight Python/Go stub) that implements the `parallel_explore`, `branch_output`, and `branch_read_file` RPCs expected by `MCPClient`.
  - Point `MCP_BASE_URL` to the stub and run `go run ./cmd/dev-agent ...`.
  - Record the emitted `worklog.md`/`code_review.log` artifacts to verify that the Implement → Review → Fix loop completes.
- **Linters / static analysis**: At minimum run `go fmt ./...`, `go vet ./...`, and (if installed) `staticcheck ./...`. Submit lint fixes in the same PR unless they would drown out the functional change.
- **Streaming validation**: With `--stream-json`, pipe stdout to `jq` or `rg` to ensure `thread.*`, `turn.*`, and `item.*` events are emitted for every iteration. This is critical when modifying `internal/streaming` or the orchestrator.

## Branching & Pull Requests

- **Branches**: Use descriptive Pantheon-style names, e.g. `pantheon/feat-shortslug`, `pantheon/fix-tool-handler`, or `pantheon/chore-ci`. Keep slugs short and unique so automation can reference them in lineage reports.
- **Commits**: Write imperative, 72-char summaries (`Add CONTRIBUTING guide`, `Tighten MCP retry logging`). Squash locally if you need multiple WIP commits, but avoid force-pushing after review unless coordinated.
- **PR checklist**:
  - [ ] Tests (`go test ./...`) pass locally.
  - [ ] Headless CLI run succeeds against either Pantheon staging or a mock MCP.
  - [ ] `worklog.md` / `code_review.log` artifacts were inspected (do not commit them).
  - [ ] `CONTRIBUTING.md` / docs updated when behavior changes.
  - [ ] Final JSON report (see below) looks correct.
- **Description expectations**: Include the user task or Pantheon issue ID, summarize architecture changes, call out testing evidence (commands + output), and mention any follow-up work.

## Reporting & Publishing

- **JSON report**: Every run emits a pretty JSON payload to stderr with `task`, `summary`, `status`, `is_finished`, `start_branch_id`, `latest_branch_id`, `instructions`, and (when applicable) `publish_report`. When adding new fields, update `BuildInstructions` so downstream automations know how to act.
- **Branch lineage**: `internal/tools.BranchTracker` stores the first/last branch IDs touched. Document lineage in PRs so reviewers can retrieve the Pantheon branch if needed.
- **Publish metadata**: `finalizeBranchPush` instructs the implementer agent to include repository URL, branch, commit hash, and artifact pointers in its publish report. When adjusting publish prompts, keep these requirements intact and verify that automation still refuses to commit `worklog.md` or `code_review.log`.
- **Operational runbooks**:
  - If publishing fails, the CLI returns `FINISHED_WITH_ERROR`. Capture the emitted `instructions` and the latest branch ID in your PR description so someone can resume the workflow.
  - When iterating on streaming or reporting, record the NDJSON feed and the final JSON to help downstream consumers validate schema changes.

Document every branch lineage and publish outcome in your PR body so the Pantheon operators can trace collateral effects across environments.
