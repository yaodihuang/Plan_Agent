# Code Agent Workflows

This repository contains two MCP-powered Go CLIs:
- `dev_agent` (`dev_agent/cmd/dev-agent`): orchestrates a TDD Implement → Review → Fix loop and (optionally) a final publish.
- `review_agent` (`review_agent/cmd/review-agent`): runs a PR-style review and then verifies whether any reported P0/P1 issue is real (no publish).

Both CLIs rely on Pantheon branch lineage: every `execute_agent` call creates a new branch and must chain via `parent_branch_id`.

## dev_agent: The Builder & The Critic

`dev_agent` orchestrates work through two single-turn specialists— **Codex** or **Claude Code** (builder) and **review_code** (critic)—to enforce an auditable TDD loop. Every turn produces a new Pantheon branch, so branch lineage is strictly linear and each agent receives a complete prompt (task, phase goals, context).

## Workflow Reference
- **Turn order**: Codex or Claude Code (implement) → review_code → Codex or Claude Code (fix) → … until review_code reports success. The orchestrator stops after 8 review cycles or when the critic signs off.
- **Builder agents**: `codex` and `claude_code` are both valid builder agent names; select based on task requirements.
- **Single-call discipline**: Only one `execute_agent` runs per assistant turn because the next call must inherit the previous `branch_id`.
- **Artifacts drive the loop**:
  - `worklog.md` (at `WORKSPACE_DIR/worklog.md`) stores Phase 0 notes, designs, implementation summaries, fix summaries, and test results.
  - `code_review.log` (at `WORKSPACE_DIR/code_review.log`) records P0/P1 issues. If the file is missing after three attempts the workflow halts with `FINISHED_WITH_ERROR`.
- **Git / publish rules**: Agents work locally. Commit/push happens only when the orchestrator invokes the final publish prompt, and that step explicitly forbids staging `worklog.md` or `code_review.log`.

## Codex (The Builder)
**Role**: Senior engineer responsible for analysis, design, implementation, testing, and fixes.

### Phase 0 – Context Verification (critical gate)
- Confirm every reference in the user task exists (issue IDs, files, endpoints, etc.).
- If anything is missing, stop immediately, write a "Context Failure Report" section in `worklog.md`, and surface the failure instead of guessing.

### Phase 1 – Analysis & Design
- Perform root-cause analysis for bugs or impact analysis for features.
- Outline the plan in `worklog.md` (affected files, test strategy, risks). This log is later used by the publish step, so keep it concise but explicit.

### Phase 2 – TDD Implementation
- Write or update tests **before** implementing fixes/features; regression coverage is mandatory for bug work.
- Implement the code, run the relevant local checks, and append a short summary + test command/results to `worklog.md`.
- Work only on the repository specified by the current branch lineage; never push.

### Phase 3 – Fix Iterations
- Consume the critic’s feedback from `code_review.log` (the orchestrator fetches it and includes the issues in the prompt).
- Resolve every P0/P1 finding, add/adjust tests if coverage was missing, and document a "Fix Summary" plus new test results in `worklog.md`.

### Final Publish Role
- After a clean review, Codex is prompted one last time to choose a branch name, commit the code, and push. Follow the publish instructions, but keep `worklog.md` and `code_review.log` untracked.

## Reviewer (The Critic)
**Role**: QA-focused reviewer that validates the latest branch.

- Operates on the branch produced by Codex.
- Reviews only the new work; legacy issues are ignored unless the change regresses them.
- Logs each P0/P1 issue to `code_review.log` with enough reproduction detail for Codex to act. When no blocking issues remain, write “No P0/P1 issues found” so the orchestrator can exit the loop.
- The orchestrator retries up to three times to collect `code_review.log`; missing logs stop the workflow with an explicit instruction to the user.

## General Guidance
- **Ultrathink**: pause to reason before editing; every step should be intentional.
- **Tracing**: `worklog.md` plus `code_review.log` form the audit trail referenced in the final JSON report and publish summary.
- **Iteration safety**: If a tool call returns `FINISHED_WITH_ERROR`, the orchestrator surfaces that instruction and stops—ensure the artifacts accurately describe what went wrong.

## review_agent: PR Review Verifier

`review_agent` runs a single PR-style review and then tries to confirm whether the reported P0/P1 issue is real before surfacing it:
- **Step 1 (review_code)**: run `review_code`, then read `WORKSPACE_DIR/code_review.log` (up to 3 attempts; missing log halts with `FINISHED_WITH_ERROR`).
- **Step 2 (triage)**: if the report is empty or deemed not a real P0/P1 issue, the run is treated as clean.
- **Step 3 (confirm, codex)**: if the report looks real, run `codex` as two verification roles—`reviewer` (logic) and `tester` (reproduction)—followed by one “exchange” round.
- **Stop condition**: only mark an issue *confirmed* when both roles converge on `CONFIRMED` and align on the same underlying defect; otherwise mark it *unresolved* (“存疑不报”).
- **Output**: emits a structured JSON result (review logs, transcripts, verdicts) and does not commit/push branches.
