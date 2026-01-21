package prreview

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	b "review_agent/internal/brain"
	tools "review_agent/internal/tools"
)

type fakeAgentClient struct {
	mu sync.Mutex

	next int
	byID map[string]string

	calls []agentCall

	reviewerR1 string
	testerR1   string
	reviewerR2 string
	testerR2   string
}

type agentCall struct {
	branchID        string
	parentBranchID  string
	prompt          string
	classifiedRole  string
	classifiedRound int
}

func newFakeAgentClient(reviewerR1, testerR1, reviewerR2, testerR2 string) *fakeAgentClient {
	return &fakeAgentClient{
		byID:       map[string]string{},
		reviewerR1: reviewerR1,
		testerR1:   testerR1,
		reviewerR2: reviewerR2,
		testerR2:   testerR2,
		calls:      []agentCall{},
	}
}

func (c *fakeAgentClient) ParallelExplore(projectName, parentBranchID string, prompts []string, agent string, numBranches int) (map[string]any, error) {
	prompt := ""
	if len(prompts) > 0 {
		prompt = prompts[0]
	}
	role, round := classifyPrompt(prompt)
	out := pickOutput(role, round, c.reviewerR1, c.testerR1, c.reviewerR2, c.testerR2)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.next++
	branchID := fmt.Sprintf("branch_%d", c.next)
	c.byID[branchID] = out
	c.calls = append(c.calls, agentCall{
		branchID:        branchID,
		parentBranchID:  parentBranchID,
		prompt:          prompt,
		classifiedRole:  role,
		classifiedRound: round,
	})
	return map[string]any{
		"branch_id": branchID,
	}, nil
}

func (c *fakeAgentClient) GetBranch(branchID string) (map[string]any, error) {
	return map[string]any{
		"id":             branchID,
		"status":         "succeed",
		"latest_snap_id": fmt.Sprintf("%s_snap", branchID),
	}, nil
}

func (c *fakeAgentClient) BranchReadFile(branchID string, filePath string) (map[string]any, error) {
	return map[string]any{}, fmt.Errorf("not implemented")
}

func (c *fakeAgentClient) BranchOutput(branchID string, fullOutput bool) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{
		"output": c.byID[branchID],
	}, nil
}

func TestConfirmIssueDoesNotConfirmWhenTranscriptsMisaligned(t *testing.T) {
	reviewer := "# VERDICT: CONFIRMED\n\nClaim: issueText describes defect A\nAnchor: alpha.go:10\n\n## Reasoning\nConfirmed Defect A."
	tester := "# VERDICT: CONFIRMED\n\nClaim: issueText describes defect B\nAnchor: beta.go:20\n\n## Reproduction Steps\nConfirmed Defect B."
	reviewerR2 := "# VERDICT: CONFIRMED\n\nClaim: defect A\nAnchor: alpha.go:10\n\n## Response to Peer\nStill A.\n\n## Final Reasoning\nStill A."
	testerR2 := "# VERDICT: CONFIRMED\n\nClaim: defect B\nAnchor: beta.go:20\n\n## Response to Peer\nStill B.\n\n## Final Reasoning\nStill B."
	client := newFakeAgentClient(reviewer, tester, reviewerR2, testerR2)
	handler := tools.NewToolHandler(client, "proj", "start", "")
	runner, err := NewRunner(&b.LLMBrain{}, handler, nil, Options{
		Task:           "task",
		ProjectName:    "proj",
		ParentBranchID: "start",
		WorkspaceDir:   "",
	})
	if err != nil {
		t.Fatalf("NewRunner error: %v", err)
	}
	runner.alignmentOverride = func(issueText string, alpha Transcript, beta Transcript) (alignmentVerdict, error) {
		return alignmentVerdict{Agree: false, Explanation: "test: misaligned"}, nil
	}

	report, err := runner.confirmIssue("ISSUE: example", "start", "")
	if err != nil {
		t.Fatalf("confirmIssue error: %v", err)
	}
	if report.Status == commentConfirmed {
		t.Fatalf("expected unresolved when reviewer/tester confirm different anchors; got status=%q explanation=%q", report.Status, report.VerdictExplanation)
	}
}

func TestConfirmIssueSkipsTesterWhenFlagSet(t *testing.T) {
	reviewer := "# VERDICT: CONFIRMED\n\nClaim: issueText describes defect A\nAnchor: alpha.go:10\n\n## Reasoning\nConfirmed Defect A."
	client := newFakeAgentClient(reviewer, "", "", "")
	handler := tools.NewToolHandler(client, "proj", "start", "")
	runner, err := NewRunner(&b.LLMBrain{}, handler, nil, Options{
		Task:           "task",
		ProjectName:    "proj",
		ParentBranchID: "start",
		WorkspaceDir:   "",
		SkipTester:     true,
	})
	if err != nil {
		t.Fatalf("NewRunner error: %v", err)
	}

	report, err := runner.confirmIssue("ISSUE: example", "start", "")
	if err != nil {
		t.Fatalf("confirmIssue error: %v", err)
	}
	if report.Status != commentConfirmed {
		t.Fatalf("expected confirmed when reviewer confirms with skip tester, got %q", report.Status)
	}
	if report.ExchangeRounds != 0 {
		t.Fatalf("expected 0 exchange rounds, got %d", report.ExchangeRounds)
	}
	if report.TesterRound1BranchID != "" || report.TesterRound2BranchID != "" {
		t.Fatalf("expected no tester branch ids, got r1=%q r2=%q", report.TesterRound1BranchID, report.TesterRound2BranchID)
	}

	client.mu.Lock()
	calls := append([]agentCall(nil), client.calls...)
	client.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 agent call (reviewer only), got %d: %#v", len(calls), calls)
	}
	if calls[0].classifiedRole != "reviewer" || calls[0].classifiedRound != 1 {
		t.Fatalf("expected reviewer round1 call, got role=%q round=%d", calls[0].classifiedRole, calls[0].classifiedRound)
	}
}

func TestConfirmIssueUsesDoubleBlindBranchTopology(t *testing.T) {
	reviewerR1 := "# VERDICT: REJECTED\n\nClaim: something\nAnchor: unknown\n\n## Reasoning\nNo."
	testerR1 := "# VERDICT: CONFIRMED\n\nClaim: something\nAnchor: cmd\n\n## Reproduction Steps\nYes."
	reviewerR2 := "# VERDICT: REJECTED\n\nClaim: something\nAnchor: unknown\n\n## Response to Peer\nStill no.\n\n## Final Reasoning\nStill no."
	testerR2 := "# VERDICT: CONFIRMED\n\nClaim: something\nAnchor: cmd\n\n## Response to Peer\nStill yes.\n\n## Final Reasoning\nStill yes."

	client := newFakeAgentClient(reviewerR1, testerR1, reviewerR2, testerR2)
	handler := tools.NewToolHandler(client, "proj", "start", "")
	runner, err := NewRunner(&b.LLMBrain{}, handler, nil, Options{
		Task:           "task",
		ProjectName:    "proj",
		ParentBranchID: "start",
		WorkspaceDir:   "",
	})
	if err != nil {
		t.Fatalf("NewRunner error: %v", err)
	}

	startBranchID := "discovery_branch"
	report, err := runner.confirmIssue("ISSUE: example", startBranchID, "")
	if err != nil {
		t.Fatalf("confirmIssue error: %v", err)
	}

	client.mu.Lock()
	calls := append([]agentCall(nil), client.calls...)
	client.mu.Unlock()

	if len(calls) != 4 {
		t.Fatalf("expected 4 agent calls (2x round1 + 2x round2), got %d: %#v", len(calls), calls)
	}

	var reviewerR1Branch, testerR1Branch string
	for _, call := range calls {
		if call.classifiedRound == 1 && call.classifiedRole == "reviewer" {
			reviewerR1Branch = call.branchID
			if call.parentBranchID != startBranchID {
				t.Fatalf("reviewer round1 should fork from startBranchID=%q, got %q", startBranchID, call.parentBranchID)
			}
		}
		if call.classifiedRound == 1 && call.classifiedRole == "tester" {
			testerR1Branch = call.branchID
			if call.parentBranchID != startBranchID {
				t.Fatalf("tester round1 should fork from startBranchID=%q, got %q", startBranchID, call.parentBranchID)
			}
		}
	}
	if reviewerR1Branch == "" || testerR1Branch == "" {
		t.Fatalf("missing round1 branches (reviewer=%q tester=%q). Calls: %#v", reviewerR1Branch, testerR1Branch, calls)
	}
	if report.ReviewerRound1BranchID != reviewerR1Branch {
		t.Fatalf("expected report reviewer_round1_branch_id=%q, got %q", reviewerR1Branch, report.ReviewerRound1BranchID)
	}
	if report.TesterRound1BranchID != testerR1Branch {
		t.Fatalf("expected report tester_round1_branch_id=%q, got %q", testerR1Branch, report.TesterRound1BranchID)
	}

	for _, call := range calls {
		if call.classifiedRound == 2 && call.classifiedRole == "reviewer" {
			if call.parentBranchID != reviewerR1Branch {
				t.Fatalf("reviewer round2 should fork from its own round1 branch %q, got %q", reviewerR1Branch, call.parentBranchID)
			}
			if call.parentBranchID == testerR1Branch {
				t.Fatalf("reviewer round2 must not fork from tester branch %q", testerR1Branch)
			}
		}
		if call.classifiedRound == 2 && call.classifiedRole == "tester" {
			if call.parentBranchID != testerR1Branch {
				t.Fatalf("tester round2 should fork from its own round1 branch %q, got %q", testerR1Branch, call.parentBranchID)
			}
			if call.parentBranchID == reviewerR1Branch {
				t.Fatalf("tester round2 must not fork from reviewer branch %q", reviewerR1Branch)
			}
		}
	}

	reviewerR2Branch := ""
	testerR2Branch := ""
	for _, call := range calls {
		if call.classifiedRound == 2 && call.classifiedRole == "reviewer" {
			reviewerR2Branch = call.branchID
		}
		if call.classifiedRound == 2 && call.classifiedRole == "tester" {
			testerR2Branch = call.branchID
		}
	}
	if reviewerR2Branch == "" || testerR2Branch == "" {
		t.Fatalf("missing round2 branches (reviewer=%q tester=%q). Calls: %#v", reviewerR2Branch, testerR2Branch, calls)
	}
	if report.ReviewerRound2BranchID != reviewerR2Branch {
		t.Fatalf("expected report reviewer_round2_branch_id=%q, got %q", reviewerR2Branch, report.ReviewerRound2BranchID)
	}
	if report.TesterRound2BranchID != testerR2Branch {
		t.Fatalf("expected report tester_round2_branch_id=%q, got %q", testerR2Branch, report.TesterRound2BranchID)
	}
}

func classifyPrompt(prompt string) (role string, round int) {
	prompt = strings.TrimSpace(prompt)
	if strings.Contains(prompt, "Round 2 - Exchange") {
		round = 2
	} else {
		round = 1
	}
	switch {
	case strings.Contains(prompt, "Verification Role: REVIEWER"):
		return "reviewer", round
	case strings.Contains(prompt, "Verification Role: TESTER"):
		return "tester", round
	default:
		return "unknown", round
	}
}

func pickOutput(role string, round int, reviewerR1, testerR1, reviewerR2, testerR2 string) string {
	switch {
	case role == "reviewer" && round == 1:
		return reviewerR1
	case role == "tester" && round == 1:
		return testerR1
	case role == "reviewer" && round == 2:
		return reviewerR2
	case role == "tester" && round == 2:
		return testerR2
	default:
		return "# VERDICT: REJECTED\n\nClaim: unknown\nAnchor: unknown\n\n## Reasoning\nUnknown role."
	}
}
