package plan

import (
	"errors"
	"fmt"
	"plan_agent/internal/brain"
	"plan_agent/internal/logx"
	"plan_agent/internal/streaming"
	"strings"
)

type Options struct {
	Query          string
	ProjectName    string
	ParentBranchID string
}

type Result struct {
	Query          string     `json:"query"`
	ProjectName    string     `json:"project_name"`
	ParentBranchID string     `json:"parent_branch_id,omitempty"`
	PlanResult     PlanResult `json:"plan_result"`
}

type Runner struct {
	brain    *brain.LLMBrain
	opts     Options
	streamer *streaming.JSONStreamer
}

func NewRunner(brain *brain.LLMBrain, streamer *streaming.JSONStreamer, opts Options) (*Runner, error) {
	if brain == nil {
		return nil, errors.New("brain is required")
	}
	opts.Query = strings.TrimSpace(opts.Query)
	opts.ProjectName = strings.TrimSpace(opts.ProjectName)
	opts.ParentBranchID = strings.TrimSpace(opts.ParentBranchID)
	if opts.Query == "" {
		return nil, errors.New("query is required")
	}
	if opts.ProjectName == "" {
		return nil, errors.New("project name is required")
	}
	return &Runner{
		brain:    brain,
		opts:     opts,
		streamer: streamer,
	}, nil
}

func (r *Runner) Run() (*Result, error) {
	logx.Infof("Starting plan workflow")
	prompt := buildPlanPrompt(r.opts.Query, r.opts.ProjectName, r.opts.ParentBranchID)
	messages := []brain.ChatMessage{
		{Role: "system", Content: "You are the PLAN Agent for the Master Agent orchestration system. " +
			"Generate high-quality, executable plans that balance speed, thoroughness, and risk. " +
			"Consider code complexity, available resources, time constraints, and potential failure scenarios. " +
			"Ensure each plan has clear trade-offs and realistic confidence scores. " +
			"Reply ONLY with valid JSON matching the specified schema."},
		{Role: "user", Content: prompt},
	}
	resp, err := r.brain.Complete(messages, nil)
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return nil, errors.New("empty completion response")
	}
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return nil, errors.New("empty completion content")
	}
	planResult, err := parsePlanResult(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan result: %w", err)
	}
	return &Result{
		Query:          r.opts.Query,
		ProjectName:    r.opts.ProjectName,
		ParentBranchID: r.opts.ParentBranchID,
		PlanResult:     planResult,
	}, nil
}
