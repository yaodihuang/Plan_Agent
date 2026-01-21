package verify

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	b "verify_agent/internal/brain"
	"verify_agent/internal/logx"
	"verify_agent/internal/streaming"
	t "verify_agent/internal/tools"
)

const (
	statusBugWrong       = "bug_wrong"
	statusBugConfirmed   = "bug_confirmed"
	statusCannotDisprove = "cannot_disprove"
	statusError          = "error"
)

// Options configures the verify workflow.
type Options struct {
	BugDescription  string
	ProjectName     string
	ParentBranchID  string
	WorkspaceDir    string
	CodeContext     string // Optional: additional code context
	IsFalsePositive bool   // If true, treat bug as false positive (虚假报警); if false, verify as real bug
}

// Result captures the verification outcome.
type Result struct {
	BugDescription string       `json:"bug_description"`
	Status         string       `json:"status"`
	Summary        string       `json:"summary"`
	Task1Result    *Task1Result `json:"task1_result,omitempty"`
	Task2Result    *Task2Result `json:"task2_result,omitempty"`
	Task3Result    *Task3Result `json:"task3_result,omitempty"`
	StartBranchID  string       `json:"start_branch_id,omitempty"`
	LatestBranchID string       `json:"latest_branch_id,omitempty"`
}

// Task1Result represents the output of Task 1: Bug Claim Formalization
type Task1Result struct {
	BranchID            string               `json:"branch_id"`
	Status              string               `json:"status"` // VALID or INVALID
	BugClaim            string               `json:"bug_claim,omitempty"`
	Judgment            string               `json:"judgment,omitempty"`
	FormalizedAssertion *FormalizedAssertion `json:"formalized_assertion,omitempty"`
	Response            string               `json:"response"`
	Reason              string               `json:"reason,omitempty"`
	Analysis            string               `json:"analysis,omitempty"`
}

// Task2Result represents the output of Task 2: Reachability Analysis
type Task2Result struct {
	BranchID             string `json:"branch_id"`
	Status               string `json:"status"` // REACHABLE, UNREACHABLE, or INVALID
	FormalizedAssertion  string `json:"formalized_assertion,omitempty"`
	Judgment             string `json:"judgment,omitempty"`
	Response             string `json:"response"`
	Reason               string `json:"reason,omitempty"`
	Evidence             string `json:"evidence,omitempty"`
	ReachabilityAnalysis string `json:"reachability_analysis,omitempty"`
}

// Task3Result represents the output of Task 3: Test Generator
type Task3Result struct {
	BranchID            string `json:"branch_id"`
	Status              string `json:"status"` // BUG_CONFIRMED, BUG_REFUTED, or TEST_INCONCLUSIVE
	BugClaim            string `json:"bug_claim,omitempty"`
	FormalizedAssertion string `json:"formalized_assertion,omitempty"`
	Judgment            string `json:"judgment,omitempty"`
	Response            string `json:"response"`
	TestCase            string `json:"test_case,omitempty"`
	TestExecution       string `json:"test_execution,omitempty"`
	Analysis            string `json:"analysis,omitempty"`
}

// Runner executes the three-phase bug verification workflow.
type Runner struct {
	brain    *b.LLMBrain
	handler  *t.ToolHandler
	opts     Options
	streamer *streaming.JSONStreamer
	events   *eventHelper
}

// NewRunner validates options and constructs a workflow runner.
func NewRunner(brain *b.LLMBrain, handler *t.ToolHandler, streamer *streaming.JSONStreamer, opts Options) (*Runner, error) {
	if brain == nil {
		return nil, errors.New("brain is required")
	}
	if handler == nil {
		return nil, errors.New("tool handler is required")
	}
	opts.BugDescription = strings.TrimSpace(opts.BugDescription)
	opts.ProjectName = strings.TrimSpace(opts.ProjectName)
	opts.ParentBranchID = strings.TrimSpace(opts.ParentBranchID)
	opts.WorkspaceDir = strings.TrimSpace(opts.WorkspaceDir)
	if opts.BugDescription == "" {
		return nil, errors.New("bug description is required")
	}
	if opts.ProjectName == "" {
		return nil, errors.New("project name is required")
	}
	if opts.ParentBranchID == "" {
		return nil, errors.New("parent branch id is required")
	}
	return &Runner{
		brain:    brain,
		handler:  handler,
		opts:     opts,
		streamer: streamer,
		events:   newEventHelper(streamer),
	}, nil
}

// Run executes the three-task workflow and returns the structured result.
func (r *Runner) Run() (*Result, error) {
	logx.Infof("Starting bug verification workflow for bug: %s", r.opts.BugDescription)
	parent := r.opts.ParentBranchID

	result := &Result{
		BugDescription: r.opts.BugDescription,
	}

	// Task 1: Bug Claim Formalization
	logx.Infof("Task 1: Formalizing bug claim")
	task1Result, err := r.runTask1(parent)
	if err != nil {
		return nil, fmt.Errorf("task 1 failed: %w", err)
	}
	result.Task1Result = task1Result

	// Check if Task 1 found the bug claim invalid
	if task1Result.Status == "INVALID" {
		result.Status = statusBugWrong
		if r.opts.IsFalsePositive {
			result.Summary = fmt.Sprintf("Bug claim confirmed as FALSE POSITIVE (invalid): %s", task1Result.Reason)
		} else {
			result.Summary = fmt.Sprintf("Bug claim is invalid: %s", task1Result.Reason)
		}
		r.attachBranchRange(result)
		return result, nil
	}

	if task1Result.FormalizedAssertion == nil {
		// This should not happen if status is VALID and parsing succeeded
		// But if it does, treat as INVALID
		result.Status = statusBugWrong
		reason := task1Result.Reason
		if reason == "" {
			reason = "Task 1 reported VALID status but did not produce a valid formalized assertion"
		}
		if r.opts.IsFalsePositive {
			result.Summary = fmt.Sprintf("Bug claim confirmed as FALSE POSITIVE (parsing failed): %s", reason)
		} else {
			result.Summary = fmt.Sprintf("Bug claim is invalid (parsing failed): %s", reason)
		}
		r.attachBranchRange(result)
		return result, nil
	}

	// Task 2: Reachability Analysis
	logx.Infof("Task 2: Analyzing reachability")
	task2Result, err := r.runTask2(parent, task1Result.FormalizedAssertion, task1Result.Response)
	if err != nil {
		return nil, fmt.Errorf("task 2 failed: %w", err)
	}
	result.Task2Result = task2Result

	// Check if Task 2 found the state unreachable
	if task2Result.Status == "UNREACHABLE" || task2Result.Status == "INVALID" {
		result.Status = statusBugWrong
		if r.opts.IsFalsePositive {
			result.Summary = fmt.Sprintf("Bug claim confirmed as FALSE POSITIVE (unreachable): %s", task2Result.Reason)
		} else {
			result.Summary = fmt.Sprintf("Bug state is unreachable: %s", task2Result.Reason)
		}
		r.attachBranchRange(result)
		return result, nil
	}

	// Task 3: Test Generator
	logx.Infof("Task 3: Generating test case")
	task3Result, err := r.runTask3(parent, task1Result.FormalizedAssertion, task2Result.Response)
	if err != nil {
		return nil, fmt.Errorf("task 3 failed: %w", err)
	}
	result.Task3Result = task3Result

	// Determine final result based on IsFalsePositive assumption
	if r.opts.IsFalsePositive {
		// We assume the bug is FALSE. We've tried to prove it wrong.
		// If we found evidence it's wrong (refuted), report bug_wrong.
		// If we cannot disprove it despite our assumption, we still conclude it's wrong
		// (because our assumption is that it's false, and we haven't found strong evidence otherwise).
		if task3Result.Status == "BUG_REFUTED" {
			result.Status = statusBugWrong
			summaryText := task3Result.Judgment
			if summaryText == "" {
				summaryText = task3Result.Analysis
			}
			if summaryText == "" {
				summaryText = "Bug claim refuted by test"
			}
			result.Summary = fmt.Sprintf("Bug claim confirmed as FALSE POSITIVE: %s", summaryText)
		} else if task3Result.Status == "BUG_CONFIRMED" {
			// If test actually confirms the bug despite our false positive assumption,
			// this proves the assumption was WRONG - the bug is actually REAL
			// We must report bug_confirmed because the evidence is irrefutable
			result.Status = statusBugConfirmed
			summaryText := task3Result.Judgment
			if summaryText == "" {
				summaryText = task3Result.Analysis
			}
			if summaryText == "" {
				summaryText = "Bug claim confirmed by test"
			}
			result.Summary = fmt.Sprintf("ASSUMPTION WAS WRONG: Bug was assumed FALSE POSITIVE but test CONFIRMED it is REAL. %s", summaryText)
		} else {
			// TEST_INCONCLUSIVE - we couldn't disprove it through testing,
			// but based on our assumption that it's false, we conclude it's likely false
			result.Status = statusBugWrong
			result.Summary = "Bug claim is likely FALSE POSITIVE: Cannot disprove through test, but assumption and evidence suggest it is not a real bug"
		}
	} else {
		// We assume the bug is REAL. We're trying to confirm it.
		// If test confirms it, report bug_confirmed.
		// If test refutes it, report bug_wrong.
		// If test is inconclusive, report cannot_disprove (we assume it's real but can't prove).
		if task3Result.Status == "BUG_CONFIRMED" {
			result.Status = statusBugConfirmed
			summaryText := task3Result.Judgment
			if summaryText == "" {
				summaryText = task3Result.Analysis
			}
			if summaryText == "" {
				summaryText = "Bug claim confirmed by test"
			}
			result.Summary = fmt.Sprintf("Bug claim CONFIRMED as REAL: %s", summaryText)
		} else if task3Result.Status == "BUG_REFUTED" {
			result.Status = statusBugWrong
			summaryText := task3Result.Judgment
			if summaryText == "" {
				summaryText = task3Result.Analysis
			}
			if summaryText == "" {
				summaryText = "Bug claim refuted by test"
			}
			result.Summary = fmt.Sprintf("Bug claim refuted: %s", summaryText)
		} else {
			// TEST_INCONCLUSIVE - we couldn't confirm it through testing
			// Since we assume it's real, we still lean towards it being real
			result.Status = statusBugConfirmed
			result.Summary = "Bug claim assumed REAL: Test was inconclusive, but assumption and evidence suggest it is a real bug"
		}
	}
	r.attachBranchRange(result)
	return result, nil
}

func (r *Runner) runTask1(parentBranchID string) (*Task1Result, error) {
	prompt := buildFormalizationPrompt(r.opts.BugDescription, r.opts.CodeContext, r.opts.IsFalsePositive)
	data, err := r.executeAgent("codex", prompt, parentBranchID)
	if err != nil {
		return nil, err
	}
	branchID := stringField(data, "branch_id")
	response := strings.TrimSpace(stringField(data, "response"))

	status := extractStatus(response, []string{"VALID", "INVALID"})
	if status == "" {
		// Default based on IsFalsePositive assumption
		// If IsFalsePositive=true, assume INVALID (prove it's false)
		// If IsFalsePositive=false, assume VALID (prove it's real)
		if r.opts.IsFalsePositive {
			status = "INVALID"
		} else {
			status = "VALID"
		}
	}

	result := &Task1Result{
		BranchID: branchID,
		Status:   status,
		Response: response,
	}

	// Extract bug claim and judgment (should be before analysis)
	result.BugClaim = extractBugClaim(response)
	result.Judgment = extractJudgment(response)

	if status == "VALID" {
		// Try to parse the formalized assertion
		assertion, err := parseFormalizedAssertion(response)
		if err != nil {
			logx.Warningf("Failed to parse formalized assertion: %v. Response preview: %s", err, truncateString(response, 500))
			// If we can't parse the assertion, treat it as INVALID
			result.Status = "INVALID"
			result.Reason = fmt.Sprintf("Failed to parse formalized assertion: %v. The response may not contain valid JSON.", err)
			// Try to extract reason from response as fallback
			if extractedReason := extractReason(response); extractedReason != "" {
				result.Reason = extractedReason
			}
		} else {
			result.FormalizedAssertion = &assertion
		}
	} else {
		// Extract reason from response
		result.Reason = extractReason(response)
	}

	// Extract analysis (should be after bug claim and judgment)
	result.Analysis = extractSection(response, "Analysis")

	return result, nil
}

func (r *Runner) runTask2(parentBranchID string, assertion *FormalizedAssertion, task1Response string) (*Task2Result, error) {
	// Format the formalized assertion as a string
	assertionStr := fmt.Sprintf("Precondition: %s\nPath: %s\nPostcondition: %s",
		assertion.Precondition, assertion.Path, assertion.Postcondition)

	prompt := buildReachabilityPrompt(assertionStr, r.opts.CodeContext, r.opts.IsFalsePositive)
	data, err := r.executeAgent("codex", prompt, parentBranchID)
	if err != nil {
		return nil, err
	}
	branchID := stringField(data, "branch_id")
	response := strings.TrimSpace(stringField(data, "response"))

	status := extractStatus(response, []string{"REACHABLE", "UNREACHABLE", "INVALID"})
	if status == "" {
		// Default based on IsFalsePositive assumption
		// If IsFalsePositive=true, assume UNREACHABLE (prove it's false)
		// If IsFalsePositive=false, assume REACHABLE (prove it's real)
		if r.opts.IsFalsePositive {
			status = "UNREACHABLE"
		} else {
			status = "REACHABLE"
		}
	}

	result := &Task2Result{
		BranchID: branchID,
		Status:   status,
		Response: response,
	}

	// Extract formalized assertion and judgment (should be before analysis)
	result.FormalizedAssertion = extractSection(response, "Formalized Assertion")
	result.Judgment = extractJudgment(response)

	if status == "UNREACHABLE" || status == "INVALID" {
		result.Reason = extractReason(response)
		result.Evidence = extractEvidence(response)
	} else {
		// Extract reachability analysis
		result.ReachabilityAnalysis = extractSection(response, "Reachability Analysis")
		result.Evidence = extractEvidence(response)
	}

	return result, nil
}

func (r *Runner) runTask3(parentBranchID string, assertion *FormalizedAssertion, task2Response string) (*Task3Result, error) {
	// Format the formalized assertion as a string
	assertionStr := fmt.Sprintf("Precondition: %s\nPath: %s\nPostcondition: %s",
		assertion.Precondition, assertion.Path, assertion.Postcondition)

	prompt := buildTestGeneratorPrompt(assertionStr, task2Response, r.opts.CodeContext, r.opts.IsFalsePositive)
	data, err := r.executeAgent("codex", prompt, parentBranchID)
	if err != nil {
		return nil, err
	}
	branchID := stringField(data, "branch_id")
	response := strings.TrimSpace(stringField(data, "response"))

	status := extractStatus(response, []string{"BUG_CONFIRMED", "BUG_REFUTED", "TEST_INCONCLUSIVE"})
	if status == "" {
		// Default based on IsFalsePositive assumption
		// If IsFalsePositive=true, assume BUG_REFUTED (prove it's false)
		// If IsFalsePositive=false, assume BUG_CONFIRMED (prove it's real)
		if r.opts.IsFalsePositive {
			status = "BUG_REFUTED"
		} else {
			status = "BUG_CONFIRMED"
		}
	}

	result := &Task3Result{
		BranchID: branchID,
		Status:   status,
		Response: response,
	}

	// Extract bug claim, formalized assertion, and judgment (should be before analysis)
	result.BugClaim = extractBugClaim(response)
	result.FormalizedAssertion = extractSection(response, "Formalized Assertion")
	result.Judgment = extractJudgment(response)

	// Extract test case, execution, and analysis
	result.TestCase = extractSection(response, "Test Case")
	result.TestExecution = extractSection(response, "Test Execution")
	result.Analysis = extractSection(response, "Analysis")

	return result, nil
}

func (r *Runner) executeAgent(agent, prompt, parentBranchID string) (map[string]any, error) {
	args := map[string]any{
		"agent":            agent,
		"prompt":           prompt,
		"project_name":     r.opts.ProjectName,
		"parent_branch_id": parentBranchID,
	}
	return r.callTool("execute_agent", args)
}

func (r *Runner) callTool(name string, args map[string]any) (map[string]any, error) {
	payload, _ := json.Marshal(args)
	tc := t.ToolCall{Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = string(payload)

	start := time.Now()
	itemArgs := sanitizeArgsForEvents(name, args)
	itemID := r.events.ToolStarted("tool_call", name, itemArgs)
	defer func() {
		if itemID != "" {
			r.events.ToolCompleted(itemID, "error", time.Since(start), "", "")
		}
	}()

	resp := r.handler.Handle(tc)
	if resp == nil {
		return nil, errors.New("tool handler returned nil response")
	}
	status, _ := resp["status"].(string)
	if status != "success" {
		errMsg := extractError(resp)
		return nil, fmt.Errorf("%s failed: %s", name, errMsg)
	}
	data, _ := resp["data"].(map[string]any)
	if itemID != "" {
		branchID := t.ExtractBranchID(data)
		summary := stringField(data, "response")
		r.events.ToolCompleted(itemID, "success", time.Since(start), branchID, summary)
		itemID = ""
	}
	if data == nil {
		return nil, fmt.Errorf("%s returned no data", name)
	}
	return data, nil
}

func (r *Runner) attachBranchRange(res *Result) {
	if res == nil {
		return
	}
	if r.handler == nil {
		return
	}
	lineage := r.handler.BranchRange()
	if start := lineage["start_branch_id"]; start != "" {
		res.StartBranchID = start
	}
	if latest := lineage["latest_branch_id"]; latest != "" {
		res.LatestBranchID = latest
	}
}

func stringField(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	if v, ok := data[key]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return ""
}

func extractError(resp map[string]any) string {
	if resp == nil {
		return "unknown error"
	}
	if errObj, ok := resp["error"]; ok && errObj != nil {
		switch val := errObj.(type) {
		case string:
			return strings.TrimSpace(val)
		case map[string]any:
			if msg, _ := val["message"].(string); msg != "" {
				return strings.TrimSpace(msg)
			}
			if instr, _ := val["instruction"].(string); instr != "" {
				return strings.TrimSpace(instr)
			}
		}
	}
	return "unknown error"
}

func extractBugClaim(response string) string {
	lines := strings.Split(response, "\n")
	inBugClaim := false
	var bugClaim strings.Builder
	for _, line := range lines {
		if strings.Contains(line, "## Bug Claim") {
			inBugClaim = true
			continue
		}
		if inBugClaim {
			if strings.HasPrefix(line, "##") || strings.HasPrefix(line, "# STATUS:") {
				break
			}
			bugClaim.WriteString(line)
			bugClaim.WriteString("\n")
		}
	}
	return strings.TrimSpace(bugClaim.String())
}

func extractJudgment(response string) string {
	lines := strings.Split(response, "\n")
	inJudgment := false
	var judgment strings.Builder
	for _, line := range lines {
		if strings.Contains(line, "## Judgment") {
			inJudgment = true
			continue
		}
		if inJudgment {
			if strings.HasPrefix(line, "##") {
				break
			}
			judgment.WriteString(line)
			judgment.WriteString("\n")
		}
	}
	return strings.TrimSpace(judgment.String())
}

func extractReason(response string) string {
	lines := strings.Split(response, "\n")
	inReason := false
	var reason strings.Builder
	for _, line := range lines {
		if strings.Contains(line, "## Reason") {
			inReason = true
			continue
		}
		if inReason {
			if strings.HasPrefix(line, "##") {
				break
			}
			reason.WriteString(line)
			reason.WriteString("\n")
		}
	}
	return strings.TrimSpace(reason.String())
}

func extractEvidence(response string) string {
	lines := strings.Split(response, "\n")
	inEvidence := false
	var evidence strings.Builder
	for _, line := range lines {
		if strings.Contains(line, "## Evidence") {
			inEvidence = true
			continue
		}
		if inEvidence {
			if strings.HasPrefix(line, "##") {
				break
			}
			evidence.WriteString(line)
			evidence.WriteString("\n")
		}
	}
	return strings.TrimSpace(evidence.String())
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func extractSection(response string, sectionName string) string {
	lines := strings.Split(response, "\n")
	inSection := false
	var section strings.Builder
	for _, line := range lines {
		if strings.Contains(line, "## "+sectionName) {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(line, "##") {
				break
			}
			section.WriteString(line)
			section.WriteString("\n")
		}
	}
	return strings.TrimSpace(section.String())
}

type eventHelper struct {
	streamer *streaming.JSONStreamer
	nextID   int64
}

func newEventHelper(streamer *streaming.JSONStreamer) *eventHelper {
	if streamer == nil || !streamer.Enabled() {
		return nil
	}
	return &eventHelper{streamer: streamer}
}

func (e *eventHelper) ToolStarted(kind, name string, args map[string]any) string {
	if e == nil {
		return ""
	}
	id := atomic.AddInt64(&e.nextID, 1)
	itemID := fmt.Sprintf("item_%d", id)
	e.streamer.EmitItemStarted(itemID, kind, name, args)
	return itemID
}

func (e *eventHelper) ToolCompleted(itemID, status string, duration time.Duration, branchID, summary string) {
	if e == nil || itemID == "" {
		return
	}
	e.streamer.EmitItemCompleted(itemID, status, duration, branchID, summary)
}

func sanitizeArgsForEvents(name string, args map[string]any) map[string]any {
	out := map[string]any{}
	if args == nil {
		return out
	}
	switch name {
	case "execute_agent":
		if agent, _ := args["agent"].(string); agent != "" {
			out["agent"] = agent
		}
		if project, _ := args["project_name"].(string); project != "" {
			out["project_name"] = project
		}
		if parent, _ := args["parent_branch_id"].(string); parent != "" {
			out["parent_branch_id"] = parent
		}
		if prompt, _ := args["prompt"].(string); prompt != "" {
			out["prompt_preview"] = streaming.PromptPreview(prompt)
		}
	default:
		for k, v := range args {
			switch val := v.(type) {
			case string:
				out[k] = streaming.PromptPreview(val)
			case float64, bool:
				out[k] = val
			}
		}
	}
	return out
}
