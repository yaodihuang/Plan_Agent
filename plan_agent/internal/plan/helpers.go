package plan

import (
	"encoding/json"
	"fmt"
	"strings"
)

const planningStudyLine = "Read as much as you can, you have unlimited read quotas and available contexts. When you are not sure about something, you must study the code until you figure out.\n\n"

const planAgentCore = "PLAN Agent - Generates multiple execution plans with parallel control.\n\n" +
	"This agent analyzes user queries and generates multiple alternative execution plans, each containing a sequence of steps that can be executed in parallel or sequentially. The master agent can then select the most appropriate plan to execute.\n\n" +
	"MULTI-ROUND INTERACTION SUPPORT:\n" +
	"This agent supports iterative planning through structured query input. The query can be either:\n" +
	"1. Plain text: Initial planning request (e.g., 'Add authentication feature')\n" +
	"2. Structured JSON: Follow-up requests with context from previous rounds\n\n" +
	"Structured query format:\n" +
	"{\n" +
	"  \"mode\": \"initial|refine|replan|incremental|merge\",\n" +
	"  \"original_query\": \"The original task description\",\n" +
	"  \"previous_plans\": [{plan_id, name, strategy, steps, ...}],\n" +
	"  \"execution_feedback\": {\n" +
	"    \"executed_plan_id\": 1,\n" +
	"    \"completed_steps\": [1, 2],\n" +
	"    \"failed_step_id\": 3,\n" +
	"    \"error_message\": \"Detailed error from step 3\",\n" +
	"    \"discovered_context\": \"New information found during execution\"\n" +
	"  },\n" +
	"  \"refinement_request\": \"Specific feedback or requirements from Master Agent\",\n" +
	"  \"constraints\": {\"max_time\": \"10 minutes\", \"prefer_parallel\": true}\n" +
	"}\n\n" +
	"Mode definitions:\n" +
	"- initial: First-time planning, generate 2-4 diverse plans\n" +
	"- refine: Modify specific plan(s) based on refinement_request (e.g., 'Make Plan 1 faster')\n" +
	"- replan: Generate recovery plans after execution failure using execution_feedback\n" +
	"- incremental: Add new alternative plans to existing previous_plans\n" +
	"- merge: Combine strengths of multiple plans into new hybrid plan(s)\n\n" +
	"MASTER AGENT INTERACTION RULES:\n" +
	"- Treat every structured JSON query as a turn from the Master Agent\n" +
	"- Use previous_plans and execution_feedback as authoritative context\n" +
	"- Preserve existing plan_id values when refining or replanning; assign new ids only for new plans\n" +
	"- For refine/replan/merge/incremental, focus on deltas instead of restating unchanged content\n" +
	"- If required context is missing, include questions_for_master in the JSON response and keep plans conservative\n\n" +
	"IMPORTANT: If the query is plain text (not JSON), treat it as mode='initial'.\n" +
	"If the query is valid JSON with a 'mode' field, follow the mode-specific instructions below.\n\n"

func buildPlanPrompt(query, projectName, parentBranchID string) string {
	var sb strings.Builder
	sb.WriteString("Role: PLAN Agent\n\n")
	sb.WriteString(planningStudyLine)
	sb.WriteString(planAgentCore)
	sb.WriteString("Core responsibilities:\n")
	sb.WriteString("1. Analyze the user's query to understand the desired outcome\n")
	sb.WriteString("2. Generate 2-4 alternative plans with different strategies\n")
	sb.WriteString("3. Design executable step sequences with proper parallel/sequential control\n")
	sb.WriteString("4. Provide clear trade-offs for each plan to enable informed selection\n\n")
	sb.WriteString("Review task workflow requirements:\n")
	sb.WriteString("When the query requests codebase or repository review:\n")
	sb.WriteString("1. The first step must generate a complete review map before any review execution\n")
	sb.WriteString("2. The review map must include:\n")
	sb.WriteString("   - Overall architecture and module boundaries\n")
	sb.WriteString("   - Key code files and directory structure\n")
	sb.WriteString("   - Potential high-risk areas and technical debt\n")
	sb.WriteString("   - Core business logic that requires focus\n")
	sb.WriteString("3. After the review map, the plan must:\n")
	sb.WriteString("   - Define a systematic review strategy based on the review map\n")
	sb.WriteString("   - Decompose review work into multiple subtasks\n")
	sb.WriteString("   - For each subtask, specify review scope and focus areas\n")
	sb.WriteString("   - Allocate appropriate resources and priorities\n")
	sb.WriteString("4. Review map quality standards:\n")
	sb.WriteString("   - Cover all important modules and files in the repository\n")
	sb.WriteString("   - Identify key dependencies and interface definitions\n")
	sb.WriteString("   - Mark known issues and historical changes when available in the repo\n")
	sb.WriteString("   - Assess code complexity and test coverage signals\n")
	sb.WriteString("5. Task dispatch requirements:\n")
	sb.WriteString("   - Each subtask must have clear acceptance criteria\n")
	sb.WriteString("   - Avoid overlap or omission across review scopes\n")
	sb.WriteString("   - Distribute review workload reasonably\n")
	sb.WriteString("   - Manage dependencies between review subtasks\n\n")
	sb.WriteString("Available MCP tools (for plan steps):\n\n")
	sb.WriteString("1. parallel_explore - Execute multiple branches concurrently\n")
	sb.WriteString("   Args: {prompt: string, num_branches: int, parent_branch_id: string|null, agent: string}\n")
	sb.WriteString("   - prompt: Task description for the agent to execute\n")
	sb.WriteString("   - num_branches: Number of parallel branches (1-10 recommended)\n")
	sb.WriteString("   - parent_branch_id: Previous branch to build upon (null for initial step)\n")
	sb.WriteString("   - agent: Agent type (MUST use EXACT names below):\n")
	sb.WriteString("       * 'codex' - Senior engineer for implementation, TDD, and fixes\n")
	sb.WriteString("       * 'claude_code' - Alternative implementation agent\n")
	sb.WriteString("       * 'tdd' - Test-driven development workflow\n")
	sb.WriteString("       * 'review_code' - Standard code review and QA validation\n")
	sb.WriteString("       * 'critical_review' - Deep critical code review\n")
	sb.WriteString("       * 'review_agent_v1.1' - Enhanced PR-style review agent\n")
	sb.WriteString("       * 'verify_agent' - Bug verification and reproduction\n")
	sb.WriteString("   Use for: Exploring multiple approaches simultaneously or parallel subtasks\n\n")
	sb.WriteString("2. get_branch - Retrieve branch execution results\n")
	sb.WriteString("   Args: {branch_id: string, file_path: string|null}\n")
	sb.WriteString("   - branch_id: ID of completed branch\n")
	sb.WriteString("   - file_path: Optional specific file to retrieve (null for all results)\n")
	sb.WriteString("   Use for: Collecting results from parallel_explore or checking branch status\n\n")
	sb.WriteString("3. branch_read_file - Read specific file from a branch\n")
	sb.WriteString("   Args: {branch_id: string, file_path: string}\n")
	sb.WriteString("   - branch_id: Target branch ID\n")
	sb.WriteString("   - file_path: Path to file within branch\n")
	sb.WriteString("   Use for: Inspecting specific artifacts or code from a branch\n\n")
	sb.WriteString("4. delete_branch - Clean up completed or failed branches\n")
	sb.WriteString("   Args: {branch_id: string}\n")
	sb.WriteString("   - branch_id: Branch to delete\n")
	sb.WriteString("   Use for: Resource cleanup after merging results or abandoning failed attempts\n\n")
	sb.WriteString("Parallel control rules:\n\n")
	sb.WriteString("1. Parallel Groups:\n")
	sb.WriteString("   - Steps with the same parallel_group number (e.g., 1) execute concurrently\n")
	sb.WriteString("   - Groups execute in ascending order: group 1 completes before group 2 starts\n")
	sb.WriteString("   - Example: steps {parallel_group: 1} run together, then {parallel_group: 2} run together\n\n")
	sb.WriteString("2. Sequential Steps:\n")
	sb.WriteString("   - Use parallel_group: null for sequential execution\n")
	sb.WriteString("   - Must specify dependencies: [step_id, ...] to define execution order\n")
	sb.WriteString("   - Example: {parallel_group: null, dependencies: [1, 2]} waits for steps 1 and 2\n\n")
	sb.WriteString("3. Dependencies Override:\n")
	sb.WriteString("   - If a step has dependencies, it waits for ALL listed steps regardless of parallel_group\n")
	sb.WriteString("   - Example: step 5 with dependencies: [3, 4] waits even if parallel_group is 1\n")
	sb.WriteString("   - Use dependencies to enforce ordering within or across parallel groups\n\n")
	sb.WriteString("4. Best Practices:\n")
	sb.WriteString("   - Parallel groups for independent tasks (e.g., testing different modules)\n")
	sb.WriteString("   - Dependencies for data flow (e.g., analyze results from previous step)\n")
	sb.WriteString("   - Avoid circular dependencies (step A depends on B, B depends on A)\n\n")
	sb.WriteString("Constraints and quality standards:\n\n")
	sb.WriteString("1. Plan Count:\n")
	sb.WriteString("   - Generate exactly 2-4 plans (no more, no less)\n")
	sb.WriteString("   - Each plan must represent a meaningfully different strategy\n\n")
	sb.WriteString("2. Step Count per Plan:\n")
	sb.WriteString("   - Minimum: 2 steps (avoid trivial plans)\n")
	sb.WriteString("   - Maximum: 10 steps (avoid overly complex plans)\n")
	sb.WriteString("   - Sweet spot: 4-7 steps for most queries\n\n")
	sb.WriteString("3. Confidence Score (0.0-1.0):\n")
	sb.WriteString("   - Code understanding completeness: 30% weight\n")
	sb.WriteString("   - Tool availability and fitness: 30% weight\n")
	sb.WriteString("   - Risk assessment (failure probability): 20% weight\n")
	sb.WriteString("   - Estimated success rate: 20% weight\n")
	sb.WriteString("   - Score 0.8-1.0: High confidence (clear requirements, proven approach)\n")
	sb.WriteString("   - Score 0.6-0.8: Medium confidence (some unknowns, standard approach)\n")
	sb.WriteString("   - Score 0.4-0.6: Low confidence (many unknowns, experimental approach)\n")
	sb.WriteString("   - Score below 0.4: Not recommended (too risky or unclear)\n\n")
	sb.WriteString("Plan diversity requirement:\n")
	sb.WriteString("- At least one aggressive parallel plan\n")
	sb.WriteString("- At least one conservative sequential plan\n")
	sb.WriteString("- Optional hybrid/minimal plan if useful\n\n")
	sb.WriteString("Output schema (reply ONLY with JSON):\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"plans\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"plan_id\": 1,\n")
	sb.WriteString("      \"name\": \"Short descriptive name\",\n")
	sb.WriteString("      \"strategy\": \"High-level strategy\",\n")
	sb.WriteString("      \"steps\": [\n")
	sb.WriteString("        {\n")
	sb.WriteString("          \"step_id\": 1,\n")
	sb.WriteString("          \"description\": \"What this step does\",\n")
	sb.WriteString("          \"tool_name\": \"parallel_explore\",\n")
	sb.WriteString("          \"tool_args\": {\"prompt\": \"...\", \"num_branches\": 2, \"parent_branch_id\": null, \"agent\": \"tdd\"},\n")
	sb.WriteString("          \"dependencies\": [],\n")
	sb.WriteString("          \"parallel_group\": 1,\n")
	sb.WriteString("          \"expected_outcome\": \"What this step should achieve\"\n")
	sb.WriteString("        }\n")
	sb.WriteString("      ],\n")
	sb.WriteString("      \"estimated_time\": \"5-10 minutes\",\n")
	sb.WriteString("      \"pros\": [\"...\"],\n")
	sb.WriteString("      \"cons\": [\"...\"],\n")
	sb.WriteString("      \"confidence_score\": 0.72\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"recommended_plan_id\": 1,\n")
	sb.WriteString("  \"reasoning\": \"Why this plan is recommended\",\n")
	sb.WriteString("  \"response_context\": {\n")
	sb.WriteString("    \"mode\": \"initial|refine|replan|incremental|merge\",\n")
	sb.WriteString("    \"changes\": [\"Short summary of edits or new information\"],\n")
	sb.WriteString("    \"assumptions\": [\"Assumptions made due to missing context\"]\n")
	sb.WriteString("  },\n")
	sb.WriteString("  \"questions_for_master\": [\"Clarifying questions for next turn\"]\n")
	sb.WriteString("}\\n\\n")
	sb.WriteString("Concrete plan examples:\n\n")
	sb.WriteString("Example 1 - Aggressive Parallel Plan (for 'Add authentication feature'):\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"plan_id\": 1,\n")
	sb.WriteString("  \"name\": \"Parallel Multi-Module Implementation\",\n")
	sb.WriteString("  \"strategy\": \"Implement auth components in parallel, then integrate\",\n")
	sb.WriteString("  \"steps\": [\n")
	sb.WriteString("    {\"step_id\": 1, \"tool_name\": \"parallel_explore\", \"tool_args\": {\"prompt\": \"Implement login API\", \"num_branches\": 1, \"agent\": \"tdd\"}, \"parallel_group\": 1, \"dependencies\": []},\n")
	sb.WriteString("    {\"step_id\": 2, \"tool_name\": \"parallel_explore\", \"tool_args\": {\"prompt\": \"Implement user model\", \"num_branches\": 1, \"agent\": \"tdd\"}, \"parallel_group\": 1, \"dependencies\": []},\n")
	sb.WriteString("    {\"step_id\": 3, \"tool_name\": \"parallel_explore\", \"tool_args\": {\"prompt\": \"Implement JWT middleware\", \"num_branches\": 1, \"agent\": \"tdd\"}, \"parallel_group\": 1, \"dependencies\": []},\n")
	sb.WriteString("    {\"step_id\": 4, \"tool_name\": \"parallel_explore\", \"tool_args\": {\"prompt\": \"Integrate auth components\", \"num_branches\": 1, \"agent\": \"tdd\"}, \"parallel_group\": 2, \"dependencies\": [1, 2, 3]}\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"pros\": [\"Fast execution (3 parallel tasks)\", \"Clear separation of concerns\"],\n")
	sb.WriteString("  \"cons\": [\"May have integration issues\", \"Higher resource usage\"],\n")
	sb.WriteString("  \"confidence_score\": 0.75\n")
	sb.WriteString("}\n\n")
	sb.WriteString("Example 2 - Conservative Sequential Plan (for 'Add authentication feature'):\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"plan_id\": 2,\n")
	sb.WriteString("  \"name\": \"Incremental Sequential Implementation\",\n")
	sb.WriteString("  \"strategy\": \"Build and test each component before proceeding\",\n")
	sb.WriteString("  \"steps\": [\n")
	sb.WriteString("    {\"step_id\": 1, \"tool_name\": \"parallel_explore\", \"tool_args\": {\"prompt\": \"Implement and test user model\", \"num_branches\": 1, \"agent\": \"tdd\"}, \"parallel_group\": null, \"dependencies\": []},\n")
	sb.WriteString("    {\"step_id\": 2, \"tool_name\": \"parallel_explore\", \"tool_args\": {\"prompt\": \"Implement login API using user model\", \"num_branches\": 1, \"agent\": \"tdd\"}, \"parallel_group\": null, \"dependencies\": [1]},\n")
	sb.WriteString("    {\"step_id\": 3, \"tool_name\": \"parallel_explore\", \"tool_args\": {\"prompt\": \"Add JWT middleware\", \"num_branches\": 1, \"agent\": \"tdd\"}, \"parallel_group\": null, \"dependencies\": [2]},\n")
	sb.WriteString("    {\"step_id\": 4, \"tool_name\": \"parallel_explore\", \"tool_args\": {\"prompt\": \"Integration testing\", \"num_branches\": 1, \"agent\": \"review_code\"}, \"parallel_group\": null, \"dependencies\": [3]}\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"pros\": [\"Lower risk of integration bugs\", \"Each step validates previous work\"],\n")
	sb.WriteString("  \"cons\": [\"Slower execution (fully sequential)\", \"May discover design issues late\"],\n")
	sb.WriteString("  \"confidence_score\": 0.85\n")
	sb.WriteString("}\n\n")
	sb.WriteString("Note: These examples show the key differences - parallel plans use same parallel_group for concurrent steps,\n")
	sb.WriteString("while sequential plans use parallel_group: null with dependencies to enforce order.\n\n")
	sb.WriteString("Project context:\n")
	sb.WriteString("ProjectName: ")
	sb.WriteString(strings.TrimSpace(projectName))
	sb.WriteString("\n")
	if strings.TrimSpace(parentBranchID) != "" {
		sb.WriteString("ParentBranchID: ")
		sb.WriteString(strings.TrimSpace(parentBranchID))
		sb.WriteString("\n")
	}
	sb.WriteString("\nUser query:\n")
	sb.WriteString(strings.TrimSpace(query))
	sb.WriteString("\n")
	return sb.String()
}

type PlanStep struct {
	StepID          int            `json:"step_id"`
	Description     string         `json:"description"`
	ToolName        string         `json:"tool_name"`
	ToolArgs        map[string]any `json:"tool_args"`
	Dependencies    []int          `json:"dependencies"`
	ParallelGroup   *int           `json:"parallel_group"`
	ExpectedOutcome string         `json:"expected_outcome"`
}

type Plan struct {
	PlanID          int        `json:"plan_id"`
	Name            string     `json:"name"`
	Strategy        string     `json:"strategy"`
	Steps           []PlanStep `json:"steps"`
	EstimatedTime   string     `json:"estimated_time"`
	Pros            []string   `json:"pros"`
	Cons            []string   `json:"cons"`
	ConfidenceScore float64    `json:"confidence_score"`
}

type PlanResult struct {
	Plans             []Plan `json:"plans"`
	RecommendedPlanID int    `json:"recommended_plan_id"`
	Reasoning         string `json:"reasoning"`
}

func parsePlanResult(response string) (PlanResult, error) {
	var result PlanResult
	jsonBlock := extractJSONBlock(response)
	if jsonBlock == "" {
		return result, fmt.Errorf("no JSON block found in response")
	}
	if err := json.Unmarshal([]byte(jsonBlock), &result); err != nil {
		return result, fmt.Errorf("failed to parse JSON: %w", err)
	}
	if len(result.Plans) == 0 {
		return result, fmt.Errorf("plans list is empty")
	}
	if result.RecommendedPlanID == 0 {
		return result, fmt.Errorf("recommended_plan_id is missing")
	}
	return result, nil
}

func extractJSONBlock(raw string) string {
	trimmed := strings.TrimSpace(raw)
	jsonBlockStart := strings.Index(trimmed, "```json")
	if jsonBlockStart >= 0 {
		jsonStart := jsonBlockStart + 7
		for jsonStart < len(trimmed) && (trimmed[jsonStart] == ' ' || trimmed[jsonStart] == '\n' || trimmed[jsonStart] == '\r') {
			jsonStart++
		}
		jsonBlockEnd := strings.Index(trimmed[jsonStart:], "```")
		if jsonBlockEnd >= 0 {
			jsonEnd := jsonStart + jsonBlockEnd
			jsonContent := strings.TrimSpace(trimmed[jsonStart:jsonEnd])
			if jsonContent != "" {
				return jsonContent
			}
		}
	}

	codeBlockStart := strings.Index(trimmed, "```")
	if codeBlockStart >= 0 {
		contentStart := codeBlockStart + 3
		for contentStart < len(trimmed) && trimmed[contentStart] != '\n' {
			contentStart++
		}
		contentStart++
		codeBlockEnd := strings.Index(trimmed[contentStart:], "```")
		if codeBlockEnd >= 0 {
			contentEnd := contentStart + codeBlockEnd
			jsonContent := strings.TrimSpace(trimmed[contentStart:contentEnd])
			if strings.HasPrefix(jsonContent, "{") || strings.HasPrefix(jsonContent, "[") {
				return jsonContent
			}
		}
	}

	startObj := strings.Index(trimmed, "{")
	startArr := strings.Index(trimmed, "[")
	start := -1
	end := -1
	if startArr >= 0 && (startObj == -1 || startArr < startObj) {
		start = startArr
		end = strings.LastIndex(trimmed, "]")
	} else if startObj >= 0 {
		start = startObj
		end = strings.LastIndex(trimmed, "}")
	}
	if start >= 0 && end >= start {
		return trimmed[start : end+1]
	}
	return trimmed
}
