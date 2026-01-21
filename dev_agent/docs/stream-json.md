# Streaming JSON Output Design

## Goal
Expose a Codex-style live event stream so external consumers can mirror `dev-agent`
progress in real time (e.g., dashboards, IDE panes) instead of parsing human logs.
The stream must:

- Encode each event as **newline-delimited JSON (NDJSON)**, matching Codex CLI
  conventions (one JSON object per line, no surrounding `[]`).
- Cover orchestration lifecycle (thread start/end, each LLM iteration, tool calls,
  publish attempts, errors).
- Remain opt-in; default behaviour (human-readable logs + final JSON report) stays
  unchanged.

## Activation

| Mechanism | Description |
|-----------|-------------|
| CLI flag  | `--stream-json` (bool). When set the CLI writes NDJSON events to stdout as they happen and forces headless mode to keep stdout machine-parsable. |

If streaming is disabled we keep the existing text logs plus the final pretty JSON summary.

## Transport / Encoding

- Output channel: **stdout** (same as the existing final JSON). Each line is a standalone JSON object.
- Encoding: UTF-8, ASCII-safe.
- General envelope:

```json
{
  "type": "event.type",
  "timestamp": "2024-06-01T12:00:00Z",
  "sequence": 12,
  "thread_id": "uuid-v4",
  "...type specific fields..."
}
```

- `timestamp` uses RFC 3339 UTC. `sequence` is a monotonically increasing integer within a process run. `thread_id` is emitted once and reused in all events.

## Event Catalog

| Event | When emitted | Required fields |
|-------|--------------|-----------------|
| `thread.started` | After CLI config/inputs resolved, before first LLM turn. | `task`, `project_name`, `parent_branch_id`, `headless` |
| `turn.started` | Before each call to Azure OpenAI (`LLMBrain.Complete`). | `turn_id`, `iteration`, `message_count`, `tool_count` |
| `assistant.message` | Immediately after the LLM responds. Includes a short preview so dashboards can show reasoning text. | `turn_id`, `preview`, `tool_call_count` |
| `turn.completed` | After each iteration finishes handling any tool calls/final report. | `turn_id`, `iteration`, `tool_call_count`, `has_final_report` |
| `item.started` | Immediately before dispatching a tool call (e.g., `execute_agent`, `read_artifact`, `parallel_explore`, `publish`). | `item_id`, `kind` (`"tool_call"`, `"branch_poll"` …), `name`, `args` |
| `item.completed` | After the tool call (including publish) finishes. | `item_id`, `status` (`"success"`, `"error"`), `duration_ms`, `branch_id` (if available), `summary` |
| `thread.completed` | After orchestration stops (either success, iteration limit, or fatal error) but **before** printing the final pretty JSON. | `status`, `summary`, `final_report` |
| `error` | Whenever orchestration returns an error (LLM failure, MCP failure, publish failure). | `scope`, `message`, optional `iteration`/`item_id` |

Notes:
- `assistant.message` truncates long responses (currently 500 chars) to keep logs readable.
- `item.*` events mirror Codex’ `command_execution` concept. `args` include safe metadata plus a short `prompt_preview` (max ~240 chars) for `execute_agent` calls; secrets such as tokens are never emitted. Publish shows up as `item.*` with `name":"publish"` so there are no extra alias events.
- Additional helper events can be added later (e.g., `log`, `review.iteration`).

## Sample NDJSON Flow

```
{"type":"thread.started","thread_id":"019b...","timestamp":"2024-06-01T12:00:00Z","task":"Fix foo","project_name":"acme","parent_branch_id":"123","headless":true}
{"type":"turn.started","thread_id":"019b...","turn_id":"turn_1","iteration":1,"timestamp":"...","message_count":2}
{"type":"turn.completed","thread_id":"019b...","turn_id":"turn_1","iteration":1,"timestamp":"...","tool_call_count":1}
{"type":"item.started","thread_id":"019b...","item_id":"item_1","kind":"tool_call","name":"execute_agent","args":{"agent":"codex","phase":"implement"}}
{"type":"item.completed","thread_id":"019b...","item_id":"item_1","status":"success","timestamp":"...","duration_ms":4523,"branch_id":"branch_456"}
...
{"type":"thread.completed","thread_id":"019b...","status":"completed","summary":"Workflow completed successfully.","final_report":{"task":"Fix foo","status":"completed",...}}
```

## Implementation Hooks

1. **Config/CLI**: extend `cmd/dev-agent/main.go` to parse `--stream-json` and set
   a `StreamingEmitter` on a new orchestration options struct.
2. **Emitter utility** (`internal/logx` sibling or new pkg):
   - Maintains `thread_id`, `sequence`, optional buffering for disabled mode.
   - Provides helpers (`EmitThreadStarted`, `EmitTurnStarted`, …) used by
     orchestrator/tool handler.
3. **Orchestrator integration**:
   - `Orchestrate`/`ChatLoop`: emit `turn.*` events around `brain.Complete`.
   - Wrap tool loop to emit `item.*`.
   - Emit `thread.completed` right before calling `finalizeBranchPush`.
   - Emit `error` events when returning early with errors.
4. **Publish flow**:
   - `finalizeBranchPush` emits dedicated `publish.*` events (or simply use `item.*` and annotate `kind:"publish"`).
5. **Final JSON**:
   - Even in streaming mode we still print the final pretty JSON summary to keep parity with existing automation.

This design keeps the streaming protocol simple (NDJSON) and aligned with Codex,
while documenting every event type the implementation must cover. Implementation
will follow in subsequent changesets.

## Instrumentation Guidance

All streaming events should originate from the orchestration loops so the event
order mirrors the real workflow. Keep the design simple (Linus style):

1. **Single emitter**: `cmd/dev-agent/main.go` creates the `JSONStreamer` when
   `--stream-json` is set, silences non-error logs, emits `thread.started`, then
   hands the streamer into `Orchestrate` or `ChatLoop`. No other package prints
   to stdout.
2. **Orchestrator-only hooks**: `internal/orchestrator` owns every event:
   - Emit `turn.started` / `assistant.message` / `turn.completed` around each
     `LLMBrain.Complete`.
   - Wrap every tool dispatch (including publish) with `item.started` /
     `item.completed`. The publish step is just another tool (`name:"publish"`)
     emitted from this layer before invoking `finalizeBranchPush`.
   - Emit `error` immediately on failures; let the surrounding loop decide
     whether to continue or exit.
3. **Thread completion**: after orchestration finishes, `cmd/dev-agent` attaches
   lineage and emits a single `thread.completed` before printing the final JSON
   report. Even in streaming mode the final summary stays for backward
   compatibility—consumers that only need NDJSON can ignore it.

Centralizing all emit calls in the orchestrator keeps the ordering predictable,
avoids duplicated event logic in helpers, and makes future changes (new event
types, additional metadata) trivial to reason about.
