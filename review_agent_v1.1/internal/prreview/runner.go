package prreview

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
	Task             string            `json:"task"`
	Status           string            `json:"status"`
	Summary          string            `json:"summary"`
	ReviewerLogs     []ReviewerLog     `json:"reviewer_logs"`
	Issues           []IssueReport     `json:"issues"`
	StartBranchID    string            `json:"start_branch_id,omitempty"`
	LatestBranchID   string            `json:"latest_branch_id,omitempty"`
	SummaryBranchID  string            `json:"summary_branch_id,omitempty"`
	ReviewStatistics *ReviewStatistics `json:"review_statistics,omitempty"`
}

// ReviewStatistics tracks the review process statistics
type ReviewStatistics struct {
	TotalSteps      int                       `json:"total_steps"`
	AbnormalSteps   []AbnormalStep            `json:"abnormal_steps,omitempty"`
	StepTimings     []StepTiming              `json:"step_timings,omitempty"`
	TotalDuration   string                    `json:"total_duration"`
	IssueStatistics map[string]IssueStatistic `json:"issue_statistics,omitempty"`
}

// AbnormalStep records steps that had errors or unusual behavior
type AbnormalStep struct {
	StepName    string `json:"step_name"`
	Issue       string `json:"issue"`
	Description string `json:"description"`
	Timestamp   string `json:"timestamp"`
}

// StepTiming records timing information for each step
type StepTiming struct {
	StepName  string `json:"step_name"`
	Duration  string `json:"duration"`
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`
}

// IssueStatistic tracks statistics for each issue verification
type IssueStatistic struct {
	IssueText         string   `json:"issue_text"`
	Steps             int      `json:"steps"`
	Duration          string   `json:"duration"`
	ReviewerRounds    int      `json:"reviewer_rounds"`
	TesterRounds      int      `json:"tester_rounds"`
	VerifyAgentRounds int      `json:"verify_agent_rounds,omitempty"`
	AbnormalSteps     []string `json:"abnormal_steps,omitempty"`
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
	IssueText                 string     `json:"issue_text"`
	Status                    string     `json:"status,omitempty"`
	Alpha                     Transcript `json:"alpha,omitempty"` // Reviewer transcript
	Beta                      Transcript `json:"beta,omitempty"`  // VerifyAgent transcript (adversarial review)
	ReviewerRound1BranchID    string     `json:"reviewer_round1_branch_id,omitempty"`
	VerifyAgentRound1BranchID string     `json:"verify_agent_round1_branch_id,omitempty"`
	ReviewerRound2BranchID    string     `json:"reviewer_round2_branch_id,omitempty"`
	VerifyAgentRound2BranchID string     `json:"verify_agent_round2_branch_id,omitempty"`
	ReviewerRound3BranchID    string     `json:"reviewer_round3_branch_id,omitempty"`
	VerifyAgentRound3BranchID string     `json:"verify_agent_round3_branch_id,omitempty"`
	ExchangeRounds            int        `json:"exchange_rounds,omitempty"`
	VerdictExplanation        string     `json:"verdict_explanation,omitempty"`
	// Keep Tester fields for backward compatibility
	TesterRound1BranchID string `json:"tester_round1_branch_id,omitempty"`
	TesterRound2BranchID string `json:"tester_round2_branch_id,omitempty"`
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

	// Statistics tracking
	statistics *ReviewStatistics
	startTime  time.Time
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
		statistics: &ReviewStatistics{
			TotalSteps:      0,
			AbnormalSteps:   []AbnormalStep{},
			StepTimings:     []StepTiming{},
			IssueStatistics: make(map[string]IssueStatistic),
		},
		startTime: time.Now(),
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
		r.recordStepStart("scout")
		startTime := time.Now()
		if branchID, path, err := r.runScout(parent); err != nil {
			logx.Warningf("SCOUT soft-failed; continuing without change analysis. err=%v", err)
			r.recordAbnormalStep("scout", fmt.Sprintf("SCOUT soft-failed: %v", err))
			r.recordStepEnd("scout", time.Since(startTime))
		} else {
			scoutBranchID = branchID
			analysisPath = path
			r.recordStepEnd("scout", time.Since(startTime))
		}
	}

	r.recordStepStart("review")
	reviewStartTime := time.Now()
	reviewLog, err := r.runSingleReview(scoutBranchID, analysisPath)
	if err != nil {
		r.recordAbnormalStep("review", fmt.Sprintf("Review failed: %v", err))
		r.recordStepEnd("review", time.Since(reviewStartTime))
		return nil, err
	}
	r.recordStepEnd("review", time.Since(reviewStartTime))
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

	// Parse and split issues from the review report
	issues, err := r.parseIssuesFromReport(reviewLog.Report)
	if err != nil {
		logx.Warningf("Failed to parse issues from report, treating as single issue: %v", err)
		issues = []string{reviewLog.Report}
	}

	if len(issues) == 0 {
		result.Status = statusClean
		result.Summary = "Clean PR: Not found any blocking P0/P1 issues."
		r.attachBranchRange(result)
		return result, nil
	}

	numIssues := len(issues)
	logx.Infof("Parsed %d issues from review report", numIssues)

	// Convert issues to IssueReport without verification
	for _, issueText := range issues {
		result.Issues = append(result.Issues, IssueReport{
			IssueText:              issueText,
			Status:                 statusIssues, // Mark as unresolved since we skip verification
			ReviewerRound1BranchID: reviewLog.BranchID,
		})
	}

	// Limit to maximum 5 issues
	if len(result.Issues) > 5 {
		logx.Infof("Limiting issues from %d to 5", len(result.Issues))
		result.Issues = result.Issues[:5]
	}

	result.Status = statusIssues
	result.Summary = fmt.Sprintf("Identified %d P0/P1 issues.", len(result.Issues))
	r.attachBranchRange(result)

	// Finalize statistics
	r.finalizeStatistics(result)
	result.ReviewStatistics = r.statistics

	// Generate summary report in parent branch
	// Always try to generate summary report, even if there are no issues
	if summaryBranchID, err := r.generateSummaryReport(parent, result); err != nil {
		logx.Errorf("Failed to generate summary report: %v. This is a critical error.", err)
		// Don't fail the entire review, but log the error prominently
		result.SummaryBranchID = ""
	} else if summaryBranchID == "" {
		logx.Warningf("Summary report generation returned empty branch_id")
		result.SummaryBranchID = ""
	} else {
		result.SummaryBranchID = summaryBranchID
		logx.Infof("Summary report successfully generated in branch: %s", summaryBranchID)
	}

	return result, nil
}

// recordStepStart records the start of a step
func (r *Runner) recordStepStart(stepName string) {
	if r.statistics == nil {
		return
	}
	r.statistics.TotalSteps++
	r.statistics.StepTimings = append(r.statistics.StepTimings, StepTiming{
		StepName:  stepName,
		StartTime: time.Now().Format(time.RFC3339),
	})
}

// recordStepEnd records the end of a step
func (r *Runner) recordStepEnd(stepName string, duration time.Duration) {
	if r.statistics == nil {
		return
	}
	// Find the last step with this name and update it
	for i := len(r.statistics.StepTimings) - 1; i >= 0; i-- {
		if r.statistics.StepTimings[i].StepName == stepName && r.statistics.StepTimings[i].EndTime == "" {
			r.statistics.StepTimings[i].Duration = duration.String()
			r.statistics.StepTimings[i].EndTime = time.Now().Format(time.RFC3339)
			break
		}
	}
}

// recordAbnormalStep records an abnormal step
func (r *Runner) recordAbnormalStep(stepName string, description string) {
	if r.statistics == nil {
		return
	}
	r.statistics.AbnormalSteps = append(r.statistics.AbnormalSteps, AbnormalStep{
		StepName:    stepName,
		Issue:       "Error or unusual behavior",
		Description: description,
		Timestamp:   time.Now().Format(time.RFC3339),
	})
}

// finalizeStatistics finalizes the statistics
func (r *Runner) finalizeStatistics(result *Result) {
	if r.statistics == nil {
		return
	}
	totalDuration := time.Since(r.startTime)
	r.statistics.TotalDuration = totalDuration.String()

	// Calculate issue statistics
	for _, issue := range result.Issues {
		stat := IssueStatistic{
			IssueText:      issue.IssueText,
			ReviewerRounds: 1,
			TesterRounds:   1,
		}
		if issue.ExchangeRounds > 0 {
			if issue.ExchangeRounds == 1 {
				stat.ReviewerRounds = 2
			} else if issue.ExchangeRounds == 2 {
				stat.ReviewerRounds = 3
			}
		}
		if issue.Beta.BranchID != "" {
			stat.VerifyAgentRounds = issue.ExchangeRounds + 1 // Round 1 + exchange rounds
		}
		stat.Steps = stat.ReviewerRounds + stat.VerifyAgentRounds
		r.statistics.IssueStatistics[issue.IssueText] = stat
	}
}

// generateSummaryReport creates a summary report in the parent branch
func (r *Runner) generateSummaryReport(parentBranchID string, result *Result) (string, error) {
	if strings.TrimSpace(r.opts.WorkspaceDir) == "" {
		return "", errors.New("workspace dir is required for summary report output")
	}

	reportPath := filepath.Join(r.opts.WorkspaceDir, "review_summary.md")
	prompt := buildSummaryReportPrompt(r.opts.Task, result, reportPath)

	data, err := r.executeAgent("codex", prompt, parentBranchID)
	if err != nil {
		return "", fmt.Errorf("failed to execute agent for summary report: %w", err)
	}

	branchID := stringField(data, "branch_id")
	if branchID == "" {
		return "", errors.New("summary report branch did not return branch_id")
	}

	logx.Infof("Summary report generated successfully in branch %s", branchID)
	return branchID, nil
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

// runVerifyAgentReview runs an adversarial review using the same review mechanism
func (r *Runner) runVerifyAgentReview(issueText string, changeAnalysisPath string, parentBranchID string, reviewerOpinion string) (Transcript, error) {
	prompt := buildVerifyAgentPrompt(r.opts.Task, issueText, changeAnalysisPath, reviewerOpinion)

	agent := "codex"
	data, err := r.executeAgent(agent, prompt, parentBranchID)
	if err != nil {
		return Transcript{}, err
	}
	return Transcript{
		Agent:    "verify_agent",
		Round:    1,
		BranchID: stringField(data, "branch_id"),
		Text:     strings.TrimSpace(stringField(data, "response")),
	}, nil
}

// runRole executes a role-based verification (Reviewer or Tester).
func (r *Runner) runRole(role string, issueText string, changeAnalysisPath string, parentBranchID string) (Transcript, error) {
	var prompt string
	if role == "reviewer" {
		prompt = buildLogicAnalystPrompt(r.opts.Task, issueText, changeAnalysisPath)
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
	if resp == nil || len(resp.Choices) == 0 {
		return false, fmt.Errorf("LLM response is empty or has no choices")
	}
	type issueCheck struct {
		HasIssue bool `json:"has_issue"`
	}
	jsonBlock := extractJSONBlock(resp.Choices[0].Message.Content)
	var check issueCheck
	if err := json.Unmarshal([]byte(jsonBlock), &check); err != nil {
		return false, fmt.Errorf("failed to parse has_issue JSON: %w", err)
	}
	return check.HasIssue, nil
}

func (r *Runner) determineVerdict(transcript Transcript) (verdictDecision, error) {
	// 1. Try to extract explicit verdict from regex
	if decision, ok := extractTranscriptVerdict(transcript.Text); ok {
		logx.Infof("Parsed explicit verdict for %s (Round %d): %s", transcript.Agent, transcript.Round, decision.Verdict)
		return decision, nil
	}

	// No LLM fallback: missing explicit marker => reject (conservative).
	return verdictDecision{
		Verdict: "rejected",
		Reason:  "missing explicit transcript verdict marker",
	}, nil
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

// parseIssuesFromReport parses the review report to extract individual issues.
// It uses LLM to identify and separate distinct P0/P1 issues from the report text.
func (r *Runner) parseIssuesFromReport(reportText string) ([]string, error) {
	// Use LLM to parse issues from the report
	prompt := buildIssueParserPrompt(reportText)
	resp, err := r.brain.Complete([]b.ChatMessage{
		{Role: "system", Content: "Parse code review reports and extract individual P0/P1 issues. Reply only with JSON."},
		{Role: "user", Content: prompt},
	}, nil)
	if err != nil {
		return nil, err
	}

	if resp == nil || len(resp.Choices) == 0 {
		return nil, fmt.Errorf("LLM response is empty or has no choices")
	}

	type issueList struct {
		Issues []struct {
			Text     string `json:"text"`
			Priority string `json:"priority"` // P0, P1, etc.
		} `json:"issues"`
	}

	jsonBlock := extractJSONBlock(resp.Choices[0].Message.Content)
	var list issueList
	if err := json.Unmarshal([]byte(jsonBlock), &list); err != nil {
		// Fallback: treat entire report as single issue
		logx.Warningf("Failed to parse JSON from LLM response, treating entire report as single issue: %v", err)
		return []string{reportText}, nil
	}

	// If LLM explicitly returned empty array (e.g., "No P0/P1 issues found"), return empty
	// Note: This should be rare since hasRealIssue already filtered these out
	if len(list.Issues) == 0 {
		// Check if report explicitly says no issues
		lowerReport := strings.ToLower(reportText)
		if strings.Contains(lowerReport, "no p0/p1 issues found") ||
			strings.Contains(lowerReport, "no p0/p1 issue") {
			return []string{}, nil
		}
		// Otherwise, fallback to treating entire report as single issue
		logx.Warningf("LLM returned empty issues array, treating entire report as single issue")
		return []string{reportText}, nil
	}

	issues := make([]string, 0, len(list.Issues))
	for _, issue := range list.Issues {
		if strings.TrimSpace(issue.Text) != "" {
			issues = append(issues, strings.TrimSpace(issue.Text))
		}
	}

	if len(issues) == 0 {
		// All issues had empty text, fallback to entire report
		logx.Warningf("All parsed issues had empty text, treating entire report as single issue")
		return []string{reportText}, nil
	}

	return issues, nil
}

// filterDuplicateVerifyBranches filters out issues that have verify branch IDs
// that have already appeared in previous issues.
func (r *Runner) filterDuplicateVerifyBranches(issues []IssueReport) []IssueReport {
	seenBranchIDs := make(map[string]bool)
	filtered := make([]IssueReport, 0, len(issues))

	for _, issue := range issues {
		// Collect all verify branch IDs for this issue
		verifyBranchIDs := []string{}
		if issue.VerifyAgentRound1BranchID != "" {
			verifyBranchIDs = append(verifyBranchIDs, issue.VerifyAgentRound1BranchID)
		}
		if issue.VerifyAgentRound2BranchID != "" {
			verifyBranchIDs = append(verifyBranchIDs, issue.VerifyAgentRound2BranchID)
		}
		if issue.VerifyAgentRound3BranchID != "" {
			verifyBranchIDs = append(verifyBranchIDs, issue.VerifyAgentRound3BranchID)
		}

		// Check if any verify branch ID has been seen before
		hasDuplicate := false
		for _, branchID := range verifyBranchIDs {
			if seenBranchIDs[branchID] {
				hasDuplicate = true
				logx.Infof("Filtering out issue with duplicate verify branch ID: %s", branchID)
				break
			}
		}

		// If no duplicates, add the issue and mark its branch IDs as seen
		if !hasDuplicate {
			filtered = append(filtered, issue)
			for _, branchID := range verifyBranchIDs {
				seenBranchIDs[branchID] = true
			}
		}
	}

	if len(filtered) < len(issues) {
		logx.Infof("Filtered out %d issues with duplicate verify branch IDs", len(issues)-len(filtered))
	}

	return filtered
}
