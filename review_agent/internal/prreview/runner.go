package prreview

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	b "review_agent/internal/brain"
	"review_agent/internal/logx"
	"review_agent/internal/streaming"
	t "review_agent/internal/tools"
)

const (
	statusClean       = "clean"
	statusIssues      = "issues_found"
	commentConfirmed  = "confirmed"
	commentUnresolved = "unresolved"
)

// Options configures the PR review workflow.
type Options struct {
	Task           string
	ProjectName    string
	ParentBranchID string
	WorkspaceDir   string
	SkipScout      bool
	SkipTester     bool
}

// Result captures the high-level outcome plus supporting artifacts.
type Result struct {
	Task           string        `json:"task"`
	Status         string        `json:"status"`
	Summary        string        `json:"summary"`
	ReviewerLogs   []ReviewerLog `json:"reviewer_logs"`
	Issues         []IssueReport `json:"issues"`
	StartBranchID  string        `json:"start_branch_id,omitempty"`
	LatestBranchID string        `json:"latest_branch_id,omitempty"`
}

// ReviewerLog records the raw output from each review_code run.
type ReviewerLog struct {
	BranchID string `json:"branch_id"`
	Report   string `json:"report"`
}

// Transcript records a codex agent's reasoning for an issue confirmation attempt.
type Transcript struct {
	Agent         string `json:"agent"`
	Round         int    `json:"round"`
	BranchID      string `json:"branch_id,omitempty"`
	Text          string `json:"text"`
	Verdict       string `json:"verdict,omitempty"`
	VerdictReason string `json:"verdict_reason,omitempty"`
}

// IssueReport stores the consensus outcome for a single ISSUE block.
type IssueReport struct {
	IssueText              string     `json:"issue_text"`
	Status                 string     `json:"status"`
	Alpha                  Transcript `json:"alpha"`
	Beta                   Transcript `json:"beta"`
	ReviewerRound1BranchID string     `json:"reviewer_round1_branch_id,omitempty"`
	TesterRound1BranchID   string     `json:"tester_round1_branch_id,omitempty"`
	ReviewerRound2BranchID string     `json:"reviewer_round2_branch_id,omitempty"`
	TesterRound2BranchID   string     `json:"tester_round2_branch_id,omitempty"`
	ExchangeRounds         int        `json:"exchange_rounds"`
	VerdictExplanation     string     `json:"verdict_explanation,omitempty"`
}

// Runner executes the two-phase PR review workflow.
type Runner struct {
	brain    *b.LLMBrain
	handler  *t.ToolHandler
	opts     Options
	streamer *streaming.JSONStreamer
	events   *eventHelper

	// alignmentOverride is a test hook to avoid network calls while exercising confirmIssue logic.
	alignmentOverride func(issueText string, alpha Transcript, beta Transcript) (alignmentVerdict, error)
	// hasRealIssueOverride is a test hook to avoid network calls in Run().
	hasRealIssueOverride func(reportText string) (bool, error)
	// verdictOverride is a test hook to avoid network calls in determineVerdict().
	verdictOverride func(transcript Transcript) (verdictDecision, error)
}

// NewRunner validates options and constructs a workflow runner.
func NewRunner(brain *b.LLMBrain, handler *t.ToolHandler, streamer *streaming.JSONStreamer, opts Options) (*Runner, error) {
	if brain == nil {
		return nil, errors.New("brain is required")
	}
	if handler == nil {
		return nil, errors.New("tool handler is required")
	}
	opts.Task = strings.TrimSpace(opts.Task)
	opts.ProjectName = strings.TrimSpace(opts.ProjectName)
	opts.ParentBranchID = strings.TrimSpace(opts.ParentBranchID)
	opts.WorkspaceDir = strings.TrimSpace(opts.WorkspaceDir)
	if opts.Task == "" {
		return nil, errors.New("task description is required")
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

// Run executes the workflow and returns the structured result.
func (r *Runner) Run() (*Result, error) {
	logx.Infof("Starting PR review workflow for parent %s", r.opts.ParentBranchID)
	parent := r.opts.ParentBranchID

	result := &Result{
		Task:         r.opts.Task,
		ReviewerLogs: []ReviewerLog{},
		Issues:       []IssueReport{},
	}

	scoutBranchID := parent
	analysisPath := ""
	if r.opts.SkipScout {
		logx.Infof("Skipping scout stage by request.")
	} else {
		if branchID, path, err := r.runScout(parent); err != nil {
			logx.Warningf("SCOUT soft-failed; continuing without change analysis. err=%v", err)
		} else {
			scoutBranchID = branchID
			analysisPath = path
		}
	}

	reviewLog, err := r.runSingleReview(scoutBranchID, analysisPath)
	if err != nil {
		return nil, err
	}
	result.ReviewerLogs = append(result.ReviewerLogs, reviewLog)

	if strings.TrimSpace(reviewLog.Report) == "" {
		result.Status = statusClean
		result.Summary = "Clean PR: Not found any blocking P0/P1 issues."
		r.attachBranchRange(result)
		return result, nil
	}

	// Check if the report actually describes a real issue
	hasIssue, err := r.hasRealIssue(reviewLog.Report)
	if err != nil {
		return nil, err
	}
	if !hasIssue {
		result.Status = statusClean
		result.Summary = "Clean PR: Not found any blocking P0/P1 issues."
		r.attachBranchRange(result)
		return result, nil
	}

	issueText := reviewLog.Report

	// Pass the reviewer's branch ID to start the verification chain
	report, err := r.confirmIssue(issueText, reviewLog.BranchID, analysisPath)
	if err != nil {
		return nil, err
	}
	result.Issues = append(result.Issues, report)
	confirmed, unresolved := summarizeIssueCounts(result.Issues)
	result.Status = statusIssues
	result.Summary = fmt.Sprintf("Identified %d P0/P1 issue (%d confirmed, %d unresolved).", len(result.Issues), confirmed, unresolved)
	r.attachBranchRange(result)
	return result, nil
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

func (r *Runner) runSingleReview(parentBranchID string, changeAnalysisPath string) (ReviewerLog, error) {
	prompt := buildIssueFinderPrompt(r.opts.Task, changeAnalysisPath)
	data, err := r.executeAgent("review_code", prompt, parentBranchID)
	if err != nil {
		return ReviewerLog{}, err
	}
	branchID := stringField(data, "branch_id")
	reviewLog := strings.TrimSpace(stringField(data, "review_report"))
	if reviewLog == "" {
		return ReviewerLog{}, fmt.Errorf("review_code did not include code_review.log contents")
	}
	return ReviewerLog{
		BranchID: branchID,
		Report:   reviewLog,
	}, nil
}

func (r *Runner) confirmIssue(issueText string, startBranchID string, changeAnalysisPath string) (IssueReport, error) {
	// V2 Flow: Reviewer + Tester with two-round unanimous consensus

	type roleRun struct {
		transcript Transcript
		verdict    verdictDecision
		err        error
	}

	runRoleWithVerdict := func(role string, parent string, out *roleRun) {
		transcript, err := r.runRole(role, issueText, changeAnalysisPath, parent)
		if err != nil {
			out.err = err
			return
		}
		decision, err := r.determineVerdict(transcript)
		if err != nil {
			out.err = fmt.Errorf("%s verdict: %w", role, err)
			return
		}
		transcript.Verdict = decision.Verdict
		transcript.VerdictReason = decision.Reason
		out.transcript = transcript
		out.verdict = decision
	}

	runExchangeWithVerdict := func(role string, selfOpinion string, peerOpinion string, parent string, out *roleRun) {
		transcript, err := r.runExchange(role, issueText, changeAnalysisPath, selfOpinion, peerOpinion, parent)
		if err != nil {
			out.err = err
			return
		}
		decision, err := r.determineVerdict(transcript)
		if err != nil {
			out.err = fmt.Errorf("%s round 2 verdict: %w", role, err)
			return
		}
		transcript.Verdict = decision.Verdict
		transcript.VerdictReason = decision.Reason
		out.transcript = transcript
		out.verdict = decision
	}

	if r.opts.SkipTester {
		var reviewerRun roleRun
		runRoleWithVerdict("reviewer", startBranchID, &reviewerRun)
		if reviewerRun.err != nil {
			return IssueReport{}, reviewerRun.err
		}
		reviewer := reviewerRun.transcript
		reviewerVerdict := reviewerRun.verdict
		report := IssueReport{
			IssueText:              issueText,
			Alpha:                  reviewer,
			ReviewerRound1BranchID: reviewer.BranchID,
			ExchangeRounds:         0,
		}
		if reviewerVerdict.Verdict == "confirmed" {
			report.Status = commentConfirmed
			report.VerdictExplanation = "SkipTester enabled: Reviewer confirmed the issue."
		} else if reviewerVerdict.Verdict == "rejected" {
			report.Status = commentUnresolved
			report.VerdictExplanation = "SkipTester enabled: Reviewer rejected the issue."
		} else {
			report.Status = commentUnresolved
			report.VerdictExplanation = fmt.Sprintf("SkipTester enabled: Reviewer verdict undetermined (%s).", strings.TrimSpace(reviewerVerdict.Reason))
		}
		return report, nil
	}

	// Round 1: Independent review (parallel + double-blind fork)
	var reviewerRun, testerRun roleRun
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		runRoleWithVerdict("reviewer", startBranchID, &reviewerRun)
	}()
	go func() {
		defer wg.Done()
		runRoleWithVerdict("tester", startBranchID, &testerRun)
	}()
	wg.Wait()
	if reviewerRun.err != nil {
		return IssueReport{}, reviewerRun.err
	}
	if testerRun.err != nil {
		return IssueReport{}, testerRun.err
	}
	reviewer := reviewerRun.transcript
	tester := testerRun.transcript
	reviewerVerdict := reviewerRun.verdict
	testerVerdict := testerRun.verdict

	report := IssueReport{
		IssueText:              issueText,
		Alpha:                  reviewer,
		Beta:                   tester,
		ReviewerRound1BranchID: reviewer.BranchID,
		TesterRound1BranchID:   tester.BranchID,
		ExchangeRounds:         0,
	}

	// Round 1 short-circuit:
	// - If both reject: unresolved
	// - If both confirm: require cross-transcript alignment before confirming
	if reviewerVerdict.Verdict == testerVerdict.Verdict && reviewerVerdict.Verdict == "rejected" {
		report.Status = commentUnresolved
		report.VerdictExplanation = "Round 1: Both Reviewer and Tester rejected the issue"
		return report, nil
	}
	if reviewerVerdict.Verdict == testerVerdict.Verdict && reviewerVerdict.Verdict == "confirmed" {
		aligned, err := r.checkAlignment(issueText, reviewer, tester)
		if err != nil {
			return IssueReport{}, err
		}
		if aligned.Agree {
			report.Status = commentConfirmed
			report.VerdictExplanation = fmt.Sprintf("Round 1: Both confirmed and aligned: %s", strings.TrimSpace(aligned.Explanation))
			return report, nil
		}
		// If both confirmed but did not align on the same defect, do NOT confirm; proceed to exchange.
	}

	// Round 2: Exchange opinions
	report.ExchangeRounds = 1

	// Round 2: Sequential exchange.
	// 1. Reviewer sees Tester's R1 opinion and clarifies/rebuts (forking from Reviewer R1 branch).
	var reviewerR2Run, testerR2Run roleRun
	runExchangeWithVerdict("reviewer", reviewer.Text, tester.Text, reviewer.BranchID, &reviewerR2Run)
	if reviewerR2Run.err != nil {
		return IssueReport{}, reviewerR2Run.err
	}

	// 2. Tester sees Reviewer's R2 "updated" opinion (forking from Tester R1 branch).
	runExchangeWithVerdict("tester", tester.Text, reviewerR2Run.transcript.Text, tester.BranchID, &testerR2Run)
	if testerR2Run.err != nil {
		return IssueReport{}, testerR2Run.err
	}
	reviewerR2 := reviewerR2Run.transcript
	testerR2 := testerR2Run.transcript
	reviewerR2Verdict := reviewerR2Run.verdict
	testerR2Verdict := testerR2Run.verdict

	// Update report with Round 2 results
	report.Alpha = reviewerR2
	report.Beta = testerR2
	report.ReviewerRound2BranchID = reviewerR2.BranchID
	report.TesterRound2BranchID = testerR2.BranchID

	if reviewerR2Verdict.Verdict == testerR2Verdict.Verdict && reviewerR2Verdict.Verdict == "confirmed" {
		aligned, err := r.checkAlignment(issueText, reviewerR2, testerR2)
		if err != nil {
			return IssueReport{}, err
		}
		if aligned.Agree {
			report.Status = commentConfirmed
			report.VerdictExplanation = fmt.Sprintf("Round 2: Both confirmed and aligned: %s", strings.TrimSpace(aligned.Explanation))
			return report, nil
		}
		// Both say confirmed, but not aligned => unresolved (存疑不报).
		report.Status = commentUnresolved
		report.VerdictExplanation = fmt.Sprintf("Round 2: Confirmed but misaligned (存疑不报): %s", strings.TrimSpace(aligned.Explanation))
		return report, nil
	}

	// 存疑不报: If still no unanimous confirmation, don't post
	report.Status = commentUnresolved
	report.VerdictExplanation = "Round 2: No unanimous confirmation (存疑不报)"

	return report, nil
}

// runRole executes a role-based verification (Reviewer or Tester).
func (r *Runner) runRole(role string, issueText string, changeAnalysisPath string, parentBranchID string) (Transcript, error) {
	var prompt string
	if role == "reviewer" {
		prompt = buildLogicAnalystPrompt(issueText)
	} else {
		prompt = buildTesterPrompt(r.opts.Task, issueText, changeAnalysisPath)
	}

	agent := "codex"
	data, err := r.executeAgent(agent, prompt, parentBranchID)
	if err != nil {
		return Transcript{}, err
	}
	return Transcript{
		Agent:    role,
		Round:    1,
		BranchID: stringField(data, "branch_id"),
		Text:     strings.TrimSpace(stringField(data, "response")),
	}, nil
}

// runExchange executes Round 2 with both the agent's and peer's opinions.
func (r *Runner) runExchange(role string, issueText string, changeAnalysisPath string, selfOpinion string, peerOpinion string, parentBranchID string) (Transcript, error) {
	prompt := buildExchangePrompt(role, r.opts.Task, issueText, changeAnalysisPath, selfOpinion, peerOpinion)

	agent := "codex"
	data, err := r.executeAgent(agent, prompt, parentBranchID)
	if err != nil {
		return Transcript{}, err
	}
	return Transcript{
		Agent:    role,
		Round:    2,
		BranchID: stringField(data, "branch_id"),
		Text:     strings.TrimSpace(stringField(data, "response")),
	}, nil
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

const changeAnalysisFilename = "change_analysis.md"

func (r *Runner) runScout(parentBranchID string) (string, string, error) {
	if strings.TrimSpace(r.opts.WorkspaceDir) == "" {
		return "", "", errors.New("workspace dir is required for scout output")
	}
	analysisPath := filepath.Join(r.opts.WorkspaceDir, changeAnalysisFilename)
	prompt := buildScoutPrompt(r.opts.Task, analysisPath)

	resp, err := r.executeAgent("codex", prompt, parentBranchID)
	if err != nil {
		return "", "", err
	}
	branchID := stringField(resp, "branch_id")
	artifact, err := r.callTool("read_artifact", map[string]any{
		"branch_id": branchID,
		"path":      analysisPath,
	})
	if err != nil {
		return "", "", err
	}
	content := stringField(artifact, "content")
	if strings.TrimSpace(content) == "" {
		return "", "", fmt.Errorf("scout wrote empty analysis file: %s", analysisPath)
	}
	return branchID, analysisPath, nil
}

func (r *Runner) hasRealIssue(reportText string) (bool, error) {
	if r.hasRealIssueOverride != nil {
		return r.hasRealIssueOverride(reportText)
	}
	prompt := buildHasRealIssuePrompt(reportText)
	resp, err := r.brain.Complete([]b.ChatMessage{
		{Role: "system", Content: "Analyze code review reports. Reply only with JSON."},
		{Role: "user", Content: prompt},
	}, nil)
	if err != nil {
		return false, err
	}
	type issueCheck struct {
		HasIssue bool `json:"has_issue"`
	}
	jsonBlock := extractJSONBlock(resp.Choices[0].Message.Content)
	var check issueCheck
	if err := json.Unmarshal([]byte(jsonBlock), &check); err != nil {
		return false, err
	}
	return check.HasIssue, nil
}

func (r *Runner) determineVerdict(transcript Transcript) (verdictDecision, error) {
	// 1. Try to extract explicit verdict from regex
	if decision, ok := extractTranscriptVerdict(transcript.Text); ok {
		logx.Infof("Parsed explicit verdict for %s (Round %d): %s", transcript.Agent, transcript.Round, decision.Verdict)
		return decision, nil
	}

	// 2. Fallback: use LLM to infer the intended verdict from the transcript text.
	// This is more robust to markdown formatting (e.g. bullet lists, inline code) that can hide the marker.
	if r.verdictOverride != nil {
		decision, err := r.verdictOverride(transcript)
		if err == nil {
			decision.Verdict = strings.ToLower(strings.TrimSpace(decision.Verdict))
			return decision, nil
		}
		logx.Warningf("Verdict override failed for %s (Round %d): %v", transcript.Agent, transcript.Round, err)
	}
	if r.brain == nil {
		return verdictDecision{Verdict: "unknown", Reason: "verdict marker missing and LLM brain unavailable"}, nil
	}
	prompt := buildVerdictExtractionPrompt(transcript)
	resp, err := r.brain.Complete([]b.ChatMessage{
		{Role: "system", Content: "Extract the transcript's final verdict. Reply ONLY with JSON."},
		{Role: "user", Content: prompt},
	}, nil)
	if err != nil {
		logx.Warningf("LLM verdict extraction failed for %s (Round %d): %v", transcript.Agent, transcript.Round, err)
		return verdictDecision{Verdict: "unknown", Reason: fmt.Sprintf("llm verdict extraction failed: %v", err)}, nil
	}
	content := ""
	if resp != nil && len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}
	decision, parseErr := parseVerdictExtractionResponse(content)
	if parseErr != nil {
		logx.Warningf("LLM verdict parse failed for %s (Round %d): %v. Raw=%q", transcript.Agent, transcript.Round, parseErr, truncateForError(content))
		return verdictDecision{Verdict: "unknown", Reason: fmt.Sprintf("llm verdict parse failed: %v", parseErr)}, nil
	}
	logx.Infof("Parsed LLM verdict for %s (Round %d): %s", transcript.Agent, transcript.Round, decision.Verdict)
	return decision, nil
}

func (r *Runner) checkAlignment(issueText string, alpha Transcript, beta Transcript) (alignmentVerdict, error) {
	if r.alignmentOverride != nil {
		return r.alignmentOverride(issueText, alpha, beta)
	}
	if r.brain == nil {
		return alignmentVerdict{}, errors.New("brain is required for alignment check")
	}
	prompt := buildAlignmentPrompt(issueText, alpha, beta)
	resp, err := r.brain.Complete([]b.ChatMessage{
		{Role: "system", Content: "Return JSON alignment verdicts for two transcripts. Reply only with JSON."},
		{Role: "user", Content: prompt},
	}, nil)
	if err != nil {
		return alignmentVerdict{}, err
	}
	content := ""
	if resp != nil && len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}
	if strings.TrimSpace(content) == "" {
		logx.Errorf("Alignment LLM returned empty content (issue=%q)", streaming.PromptPreview(issueText))
		return alignmentVerdict{}, errors.New("alignment returned empty content")
	}
	verdict, err := parseAlignment(content)
	if err != nil {
		logx.Errorf("Alignment parse failed: %v. Raw response=%q", err, content)
		return alignmentVerdict{}, err
	}
	return verdict, nil
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

func summarizeIssueCounts(reports []IssueReport) (confirmed, unresolved int) {
	for _, r := range reports {
		switch r.Status {
		case commentConfirmed:
			confirmed++
		case commentUnresolved:
			unresolved++
		}
	}
	return
}
