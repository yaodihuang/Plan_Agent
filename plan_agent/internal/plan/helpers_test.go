package plan

import (
	"strings"
	"testing"
)

func TestBuildPlanPromptIncludesCoreSections(t *testing.T) {
	prompt := buildPlanPrompt("请拆解任务", "demo-project", "parent-123", "", "")
	required := []string{
		"Role: PLAN Agent",
		planningStudyLine,
		planAgentCore,
		"Core responsibilities",
		"Available MCP tools",
		"Parallel control rules",
		"Plan diversity requirement",
		"Output schema",
		"ProjectName: demo-project",
		"ParentBranchID: parent-123",
		"User query:",
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("plan prompt missing %q", needle)
		}
	}
}

func TestParsePlanResultFromJSONBlock(t *testing.T) {
	raw := "```json\n{\n  \"plans\": [\n    {\n      \"plan_id\": 1,\n      \"name\": \"Plan A\",\n      \"strategy\": \"parallel\",\n      \"steps\": [\n        {\n          \"step_id\": 1,\n          \"description\": \"Explore\",\n          \"tool_name\": \"parallel_explore\",\n          \"tool_args\": {\"prompt\": \"x\", \"num_branches\": 2, \"parent_branch_id\": null, \"agent\": \"tdd\"},\n          \"dependencies\": [],\n          \"parallel_group\": 1,\n          \"expected_outcome\": \"Insights\"\n        }\n      ],\n      \"estimated_time\": \"quick\",\n      \"pros\": [\"fast\"],\n      \"cons\": [\"risky\"],\n      \"confidence_score\": 0.6\n    }\n  ],\n  \"recommended_plan_id\": 1,\n  \"reasoning\": \"Best balance\"\n}\n```"
	result, err := parsePlanResult(raw)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(result.Plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(result.Plans))
	}
	if result.RecommendedPlanID != 1 {
		t.Fatalf("expected recommended_plan_id=1, got %d", result.RecommendedPlanID)
	}
}
