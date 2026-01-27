package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"plan_agent/internal/brain"
	"plan_agent/internal/logx"
	"plan_agent/internal/streaming"
	t "plan_agent/internal/tools"
	"strings"
)

type Options struct {
	Query          string
	ProjectName    string
	ParentBranchID string
	WorkspaceDir   string
}

type Result struct {
	Query          string     `json:"query"`
	ProjectName    string     `json:"project_name"`
	ParentBranchID string     `json:"parent_branch_id,omitempty"`
	PlanResult     PlanResult `json:"plan_result"`
}

type Runner struct {
	brain    *brain.LLMBrain
	handler  *t.ToolHandler
	opts     Options
	streamer *streaming.JSONStreamer
}

func NewRunner(brain *brain.LLMBrain, handler *t.ToolHandler, streamer *streaming.JSONStreamer, opts Options) (*Runner, error) {
	if brain == nil {
		return nil, errors.New("brain is required")
	}
	if handler == nil {
		return nil, errors.New("tool handler is required")
	}
	opts.Query = strings.TrimSpace(opts.Query)
	opts.ProjectName = strings.TrimSpace(opts.ProjectName)
	opts.ParentBranchID = strings.TrimSpace(opts.ParentBranchID)
	opts.WorkspaceDir = strings.TrimSpace(opts.WorkspaceDir)
	if opts.Query == "" {
		return nil, errors.New("query is required")
	}
	if opts.ProjectName == "" {
		return nil, errors.New("project name is required")
	}
	return &Runner{
		brain:    brain,
		handler:  handler,
		opts:     opts,
		streamer: streamer,
	}, nil
}

func (r *Runner) Run() (*Result, error) {
	logx.Infof("Starting plan workflow")

	// Pre-load review-map.md if it exists in workspace
	reviewMapContent := ""
	logx.Infof("WorkspaceDir: %q", r.opts.WorkspaceDir)
	if r.opts.WorkspaceDir != "" {
		reviewMapPath := filepath.Join(r.opts.WorkspaceDir, "review-map.md")
		logx.Infof("Looking for review-map at: %s", reviewMapPath)
		if data, err := os.ReadFile(reviewMapPath); err == nil {
			reviewMapContent = strings.TrimSpace(string(data))
			logx.Infof("Loaded review-map.md (%d bytes) from workspace", len(reviewMapContent))
		} else if os.IsNotExist(err) {
			logx.Warningf("review-map.md not found at: %s", reviewMapPath)
		} else {
			logx.Warningf("Failed to read review-map.md: %v", err)
		}
	} else {
		logx.Warningf("WorkspaceDir is empty, skipping review-map.md loading")
	}

	prompt := buildPlanPrompt(r.opts.Query, r.opts.ProjectName, r.opts.ParentBranchID, reviewMapContent)
	messages := []brain.ChatMessage{
		{Role: "system", Content: "You are the PLAN Agent for the Master Agent orchestration system. " +
			"Generate high-quality, executable plans that balance speed, thoroughness, and risk. " +
			"Consider code complexity, available resources, time constraints, and potential failure scenarios. " +
			"Ensure each plan has clear trade-offs and realistic confidence scores. " +
			"Reply ONLY with valid JSON matching the specified schema."},
		{Role: "user", Content: prompt},
	}
	tools := t.GetToolDefinitions()
	for i := 0; i < 12; i++ {
		resp, err := r.brain.Complete(messages, tools)
		if err != nil {
			return nil, err
		}
		if resp == nil || len(resp.Choices) == 0 {
			return nil, errors.New("empty completion response")
		}
		choice := resp.Choices[0].Message
		messages = append(messages, choice)
		if len(choice.ToolCalls) > 0 {
			for _, tc := range choice.ToolCalls {
				htc := t.ToolCall{ID: tc.ID, Type: tc.Type}
				htc.Function.Name = tc.Function.Name
				htc.Function.Arguments = tc.Function.Arguments
				result := r.handler.Handle(htc)
				if instr, msg := toolError(result); msg != "" {
					if instr != "" {
						return nil, fmt.Errorf("%s (%s)", msg, instr)
					}
					return nil, errors.New(msg)
				}
				toolMsg := brain.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: toJSON(result)}
				messages = append(messages, toolMsg)
			}
			continue
		}
		content := strings.TrimSpace(choice.Content)
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
	return nil, errors.New("plan workflow reached iteration limit")
}

func toJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func toolError(resp map[string]any) (string, string) {
	if resp == nil {
		return "", ""
	}
	if status, _ := resp["status"].(string); strings.ToLower(strings.TrimSpace(status)) == "error" {
		if errObj, ok := resp["error"].(map[string]any); ok {
			instr, _ := errObj["instruction"].(string)
			if strings.TrimSpace(instr) != "" {
				msg, _ := errObj["message"].(string)
				return strings.TrimSpace(instr), strings.TrimSpace(msg)
			}
		}
	}
	return "", ""
}
