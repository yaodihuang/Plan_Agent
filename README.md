# Code Agent

This repository contains MCP-powered Go CLIs for orchestrated agent workflows.

## Plan Agent (`plan_agent/`)

### Overview

Plan Agent is an intelligent planning system that generates multiple alternative execution plans for complex tasks. It serves as the strategic planner in the Master Agent orchestration system, balancing speed, risk, and thoroughness.

### What It Does

- Generates 2-4 alternative execution plans for a given task
- Supports parallel and sequential step orchestration using explicit dependencies and parallel groups
- Provides trade-offs, confidence scores (0.0-1.0), and recommendations for each plan
- Supports multi-round planning with refinement, replanning, incremental, and merge strategies
- Automatically loads and utilizes existing review maps (remote first, then local) for contextual planning
- Falls back to a remote codebase analysis when no review map is available (unless the query requests skipping it)

### Architecture

```
plan_agent/
├── cmd/plan-agent/main.go           # CLI entry point
├── internal/
│   ├── brain/brain.go               # LLM Brain (Azure OpenAI integration)
│   ├── config/config.go             # Configuration management
│   ├── logx/logx.go                 # Structured logging
│   ├── plan/
│   │   ├── runner.go                # Main planning workflow
│   │   └── helpers.go               # Prompt building & result parsing
│   ├── streaming/json_streamer.go   # NDJSON streaming output
│   └── tools/
│       ├── handler.go               # Tool execution handler
│       └── mcp.go                   # MCP SSE client
└── go.mod
```

### CLI Usage

```bash
# Basic usage
plan-agent --query "Your task description" --project-name "ProjectName" --parent-branch-id "branch-uuid"

# With local workspace directory for context files
plan-agent --query "Your task" --project-name "ProjectName" --workspace-dir "/path/to/workspace"

# With remote workspace directory for context files on Pantheon branches
plan-agent --query "Your task" --project-name "ProjectName" --remote-workspace-dir "/remote/workspace"

# Headless mode (no interactive prompt)
plan-agent --query "Your task" --project-name "ProjectName" --headless

# NDJSON streaming output (implies headless)
plan-agent --query "Your task" --project-name "ProjectName" --stream-json
```

### CLI Arguments

| Argument | Description | Required |
|----------|-------------|----------|
| `--query` | User query/task to plan for | Yes (or interactive) |
| `--project-name` | Pantheon project name | Yes |
| `--parent-branch-id` | Parent branch UUID to fork from | Yes |
| `--workspace-dir` | Local workspace directory for context files (e.g., `review-map.md`) | No |
| `--remote-workspace-dir` | Remote workspace directory on Pantheon branch | No |
| `--headless` | Run without interactive prompt | No |
| `--stream-json` | Emit workflow events as NDJSON (implies headless) | No |

### Configuration

Plan Agent reads configuration from environment variables (supports `.env` file):

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `AZURE_OPENAI_API_KEY` | Azure OpenAI API key | Yes | - |
| `AZURE_OPENAI_BASE_URL` | Azure OpenAI endpoint URL | Yes | - |
| `AZURE_OPENAI_DEPLOYMENT` | Azure OpenAI deployment name | Yes | - |
| `AZURE_OPENAI_API_VERSION` | API version | No | `2024-12-01-preview` |
| `MCP_BASE_URL` | MCP SSE endpoint | No | `http://localhost:8000/mcp/sse` |
| `MCP_POLL_INITIAL_SECONDS` | Initial poll interval for branch status | No | `3` |
| `MCP_POLL_MAX_SECONDS` | Max poll interval for branch status | No | `30` |
| `MCP_POLL_TIMEOUT_SECONDS` | Max total poll time (min 3600s enforced) | No | `3600` |
| `MCP_POLL_BACKOFF_FACTOR` | Poll backoff multiplier (> 1.0) | No | `1.5` |
| `PROJECT_NAME` | Default project name | No | - |
| `WORKSPACE_DIR` | Default workspace directory | No | Current working directory |
| `REMOTE_WORKSPACE_DIR` | Default remote workspace directory | No | `/home/pan/workspace` |

### Input Modes

The query can be plain text or a structured JSON payload for multi-round interactions:

| Mode | Description |
|------|-------------|
| `initial` | First-time planning, generate 2-4 diverse plans |
| `refine` | Modify existing plan(s) based on feedback |
| `replan` | Generate recovery plans after execution failures |
| `incremental` | Add new alternative plans to existing ones |
| `merge` | Combine strengths of multiple plans into hybrid plan(s) |

**Structured Query Format:**
```json
{
  "mode": "refine",
  "original_query": "Original task description",
  "previous_plans": [{"plan_id": 1, "name": "...", "steps": [...]}],
  "execution_feedback": {
    "executed_plan_id": 1,
    "completed_steps": [1, 2],
    "failed_step_id": 3,
    "error_message": "Error details",
    "discovered_context": "New information found"
  },
  "refinement_request": "Make Plan 1 faster",
  "constraints": {"max_time": "10 minutes", "prefer_parallel": true}
}
```

### Available MCP Tools

The Plan Agent has access to these tools during planning:

| Tool | Description |
|------|-------------|
| `execute_agent` | Launch an MCP parallel_explore job for a specialist agent |
| `read_artifact` | Read a text artifact produced by a branch |
| `branch_output` | Retrieve the text output from a branch |
| `read_file` | Read a local file from the workspace directory |

### Available Agents (for plan steps)

| Agent | Description |
|-------|-------------|
| `codex` | Senior engineer for implementation, TDD, and fixes |
| `claude_code` | Alternative implementation agent |
| `tdd` | Test-driven development workflow |
| `review_code` | Standard code review and QA validation |
| `critical_review` | Deep critical code review |
| `review_agent_v1.1` | Enhanced PR-style review agent |
| `verify_agent` | Bug verification and reproduction |

### Parallel Control

Steps can be executed in parallel or sequentially using two mechanisms:

**1. Parallel Groups:**
- Steps with the same `parallel_group` number execute concurrently
- Groups execute in ascending order (group 1 completes before group 2 starts)

**2. Dependencies:**
- Use `dependencies: [step_id, ...]` to enforce execution order
- Sequential steps use `parallel_group: null` with explicit dependencies

**Example:**
```json
{
  "steps": [
    {"step_id": 1, "parallel_group": 1, "dependencies": []},
    {"step_id": 2, "parallel_group": 1, "dependencies": []},
    {"step_id": 3, "parallel_group": 1, "dependencies": []},
    {"step_id": 4, "parallel_group": null, "dependencies": [1, 2, 3]}
  ]
}
```
Steps 1-3 run in parallel, then step 4 runs after all complete.

### Output Format

```json
{
  "plans": [
    {
      "plan_id": 1,
      "name": "Parallel Multi-Module Implementation",
      "strategy": "Implement components in parallel, then integrate",
      "steps": [
        {
          "step_id": 1,
          "description": "What this step does",
          "tool_name": "parallel_explore",
          "tool_args": {"prompt": "...", "num_branches": 1, "agent": "tdd"},
          "dependencies": [],
          "parallel_group": 1,
          "expected_outcome": "What this step should achieve"
        }
      ],
      "estimated_time": "5-10 minutes",
      "pros": ["Fast execution", "Clear separation"],
      "cons": ["May have integration issues"],
      "confidence_score": 0.75
    }
  ],
  "recommended_plan_id": 1,
  "reasoning": "Why this plan is recommended",
  "response_context": {
    "mode": "initial",
    "changes": ["Summary of edits"],
    "assumptions": ["Assumptions made"]
  },
  "questions_for_master": ["Clarifying questions for next turn"]
}
```

### Confidence Score Components

| Component | Weight | Description |
|-----------|--------|-------------|
| Code understanding | 30% | How well the codebase is understood |
| Tool availability | 30% | Fitness of available tools for the task |
| Risk assessment | 20% | Probability of failure |
| Success rate | 20% | Estimated success probability |

**Score Interpretation:**
- 0.8-1.0: High confidence (clear requirements, proven approach)
- 0.6-0.8: Medium confidence (some unknowns, standard approach)
- 0.4-0.6: Low confidence (many unknowns, experimental approach)
- Below 0.4: Not recommended (too risky or unclear)

### Review Map Support

Plan Agent loads review context in this order:
1. `review-map.md` from the remote workspace on the parent branch (when available)
2. `review-map.md` from the local workspace directory

If no review map is found and the query does not request skipping it, Plan Agent will invoke a remote codebase analysis to seed the plan. To skip review-map and analysis generation, include a phrase like `不需要review map` or `skip review map` in the query.

Note: review-map content is passed through without truncation; ensure the file size fits your token budget.

### Build & Run

```bash
cd plan_agent
go build -o plan-agent ./cmd/plan-agent

# Run with environment variables
export AZURE_OPENAI_API_KEY="your-api-key"
export AZURE_OPENAI_BASE_URL="https://your-endpoint.openai.azure.com"
export AZURE_OPENAI_DEPLOYMENT="your-deployment"

./plan-agent --query "Add authentication feature" --project-name "MyProject" --parent-branch-id "branch-uuid"
```

---

## Related Agents

See `AGENTS.md` for documentation on:
- **dev_agent**: TDD Implement -> Review -> Fix loop orchestrator
- **review_agent**: PR-style review with P0/P1 issue verification
