# Plan_Agent

## Plan Agent

### What it does
- Generates 2–4 alternative execution plans for a given task, balancing speed and risk.
- Supports parallel and sequential step orchestration using explicit dependencies and parallel groups.
- Provides trade-offs and confidence scores to help a controller choose the best plan.
- Supports multi-round planning with refinement, replanning, and merging strategies.

### How it works
- Builds a structured planning prompt that includes project name and optional parent branch context.
- Sends the prompt to Azure OpenAI and expects a strict JSON response matching the plan schema.
- Parses and validates the JSON into a plan result for downstream execution.

### CLI usage
```bash
plan-agent --query "Your task" --project-name "ProjectName" --parent-branch-id "optional-branch" --headless
```

### Configuration
The Plan Agent reads configuration from environment variables (supports .env):
- AZURE_OPENAI_API_KEY
- AZURE_OPENAI_BASE_URL
- AZURE_OPENAI_DEPLOYMENT
- AZURE_OPENAI_API_VERSION (optional, default: 2024-12-01-preview)
- PROJECT_NAME (optional if provided by --project-name)

### Input modes
The query can be plain text or a structured JSON payload:
- initial: first-time planning, generate multiple diverse plans
- refine: adjust an existing plan based on feedback
- replan: generate recovery plans after failures
- incremental: add new alternatives
- merge: combine strengths of multiple plans

### Output shape
The output is a JSON object containing:
- plans: a list of plan candidates with steps, pros/cons, and confidence scores
- recommended_plan_id: the suggested plan to execute
- reasoning: why the plan is recommended

### Project reading behavior
The Plan Agent itself does not read repository files directly. It relies on:
- The user query and provided project name
- Optional parent branch context
Actual codebase exploration is expected to happen in the execution phase by other agents referenced in the plan steps.
