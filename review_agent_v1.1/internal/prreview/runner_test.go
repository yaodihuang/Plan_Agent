package prreview

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	b "review_agent/internal/brain"
	tools "review_agent/internal/tools"
)

type fakeRunnerClient struct {
	mu sync.Mutex

	next             int
	parallelCalls    []parallelCall
	branchReadInputs []branchReadInput
}

type parallelCall struct {
	agent  string
	prompt string
}

type branchReadInput struct {
	branchID string
	path     string
}

func (c *fakeRunnerClient) ParallelExplore(projectName, parentBranchID string, prompts []string, agent string, numBranches int) (map[string]any, error) {
	prompt := ""
	if len(prompts) > 0 {
		prompt = prompts[0]
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	branchID := fmt.Sprintf("branch-%d", c.next)
	c.parallelCalls = append(c.parallelCalls, parallelCall{
		agent:  agent,
		prompt: prompt,
	})
	return map[string]any{
		"branch_id": branchID,
	}, nil
}

func (c *fakeRunnerClient) GetBranch(branchID string) (map[string]any, error) {
	return map[string]any{
		"id":     branchID,
		"status": "succeed",
	}, nil
}

func (c *fakeRunnerClient) BranchReadFile(branchID, filePath string) (map[string]any, error) {
	c.mu.Lock()
	c.branchReadInputs = append(c.branchReadInputs, branchReadInput{branchID: branchID, path: filePath})
	c.mu.Unlock()

	if strings.HasSuffix(filePath, "code_review.log") {
		return map[string]any{
			"content": "No P0/P1 issues found",
		}, nil
	}
	if strings.HasSuffix(filePath, changeAnalysisFilename) {
		return map[string]any{
			"content": "analysis",
		}, nil
	}
	return map[string]any{"content": "ok"}, nil
}

func (c *fakeRunnerClient) BranchOutput(branchID string, fullOutput bool) (map[string]any, error) {
	return map[string]any{
		"output": "ok",
	}, nil
}

func TestRunSkipsScoutWhenFlagSet(t *testing.T) {
	client := &fakeRunnerClient{}
	handler := tools.NewToolHandler(client, "proj", "parent", "/workspace")
	runner, err := NewRunner(&b.LLMBrain{}, handler, nil, Options{
		Task:           "task",
		ProjectName:    "proj",
		ParentBranchID: "parent",
		WorkspaceDir:   "/workspace",
		SkipScout:      true,
	})
	if err != nil {
		t.Fatalf("NewRunner error: %v", err)
	}
	runner.hasRealIssueOverride = func(string) (bool, error) {
		return false, nil
	}

	result, err := runner.Run()
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.Status != statusClean {
		t.Fatalf("expected status %q, got %q", statusClean, result.Status)
	}

	client.mu.Lock()
	defer client.mu.Unlock()

	reviewCalls := 0
	for _, call := range client.parallelCalls {
		if call.agent == "review_code" {
			reviewCalls++
		}
		if call.agent == "codex" || strings.Contains(call.prompt, "Role: SCOUT") {
			t.Fatalf("expected no scout run, saw agent=%q prompt=%q", call.agent, call.prompt)
		}
	}
	if reviewCalls == 0 {
		t.Fatalf("expected review_code to run, got calls: %#v", client.parallelCalls)
	}

	for _, input := range client.branchReadInputs {
		if strings.HasSuffix(input.path, changeAnalysisFilename) {
			t.Fatalf("expected no change analysis reads, saw %q", input.path)
		}
	}
}
