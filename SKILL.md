# dev_agent Skill Reference

`dev_agent` exposes a minimal tool surface so the orchestrator can drive Pantheon branches through MCP. A Go `ToolHandler` translates each skill into the appropriate MCP RPC, enforces branch lineage rules, and normalizes responses/error payloads.

## Core Skill: `execute_agent`

Launches a specialist agent (`codex` or `review_code`) via `parallel_explore`. Every invocation creates a new branch rooted at `parent_branch_id`.

### Arguments
| Field | Required | Description |
|-------|----------|-------------|
| `agent` | ✓ | `codex` (builder), `claude_code` (builder), or `review_code` (critic). |
| `prompt` | ✓ | Complete single-turn instruction (task, phase goals, local context). |
| `project_name` | ✓ | Pantheon project to operate in. Defaults to CLI `--project-name` if omitted. |
| `parent_branch_id` | ✓ | Branch UUID to fork from. Must be the previous step’s `branch_id`. |

### Execution Flow
1. Call MCP `parallel_explore` with the supplied `agent` and prompt.
2. Poll `get_branch` (`check_status`) until the branch reports `succeed` or `failed`, with exponential sleep bounded by CLI-configured durations.
3. Record the resulting `branch_id` in the lineage tracker only after the branch succeeds.
4. Fetch the textual response via MCP `branch_output` (full log). The string is returned as `data.response`.
5. For `review_code`, attempt up to **three** `branch_read_file` calls to read `<WORKSPACE_DIR>/code_review.log`. If successful the contents are returned as `data.review_report`. After three misses the handler raises `FINISHED_WITH_ERROR` and includes diagnostic details.

### Response Payload
`{"status":"success","data":{ ... }}` with:
- `branch_id`: UUID of the newly created branch (also echoed inside `data.branch.id`).
- `status`: branch status (e.g., `"succeed"`).
- `branch`: Full branch metadata from MCP.
- `parallel_explore`: Original `parallel_explore` response for debugging.
- `response`: Human-readable log assembled from `branch_output`.
- `review_report`: Only when `agent == "review_code"`; contents of `code_review.log`.

Failures surface as `{"status":"error","error":{"message": "...", "instruction": "FINISHED_WITH_ERROR", "details": {...}}}` so the orchestrator can stop immediately.

### Usage Notes
- Always supply the full current context in `prompt`; there is no shared agent memory.
- Because every call spawns a branch, respecting the `branch_id → parent_branch_id` chain is mandatory.
- Treat `review_report` as the canonical review log; the fix-phase prompt is constructed from this artifact.

## Helper Skills

### `read_artifact`
- **Purpose**: Fetch text artifacts created on a specific branch (e.g., `code_review.log`, custom reports, generated docs).
- **Args**: `branch_id` (required), `path` (required). The path must match what the agent wrote—typically absolute inside `WORKSPACE_DIR`.
- **Behavior**: Proxies MCP `branch_read_file`. Any MCP 404 or error string is passed back verbatim so the orchestrator/LLM knows whether to retry or fail the workflow.

### `branch_output`
- **Purpose**: Retrieve the textual STDOUT/stderr transcript from a branch run.
- **Args**: `branch_id` (required), `full_output` (optional bool, defaults to `false`). When `true`, MCP returns the entire log; otherwise remote defaults may truncate.
- **Behavior**: Thin wrapper over MCP `branch_output`. Responses are returned verbatim so the orchestrator can surface previews or feed context into subsequent prompts.

## Error Handling & Best Practices
- **Single call per turn**: The orchestrator intentionally exposes only `execute_agent`, `read_artifact`, and `branch_output`. Do not parallelize `execute_agent` because later steps must inherit the lineage.
- **ToolExecutionError** payloads**: Fatal errors (missing context, branch failure, absent `code_review.log`) include `error.instruction = "FINISHED_WITH_ERROR"` so the orchestrator halts. Non-fatal issues rely solely on the `error.message`.
- **Artifacts drive fixes**: Use `read_artifact` on the critic’s branch whenever you need the exact wording of `code_review.log` rather than relying on summaries. This guarantees Codex sees the reviewer’s precise findings.
