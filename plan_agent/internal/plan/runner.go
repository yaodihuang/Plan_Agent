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
	Query              string
	ProjectName        string
	ParentBranchID     string
	WorkspaceDir       string
	RemoteWorkspaceDir string
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
	opts.RemoteWorkspaceDir = strings.TrimSpace(opts.RemoteWorkspaceDir)
	if opts.Query == "" {
		return nil, errors.New("query is required")
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
	}, nil
}

func (r *Runner) Run() (*Result, error) {
	logx.Infof("Starting plan workflow")

	reviewMapContent := ""

	if r.opts.ParentBranchID != "" && r.opts.RemoteWorkspaceDir != "" {
		remoteReviewMapPath := filepath.Join(r.opts.RemoteWorkspaceDir, "review-map.md")
		logx.Infof("Attempting to read review-map.md from remote branch %s at path %s", r.opts.ParentBranchID, remoteReviewMapPath)
		content, err := r.handler.ReadRemoteFile(r.opts.ParentBranchID, remoteReviewMapPath)
		if err != nil {
			logx.Warningf("Failed to read review-map.md from remote branch: %v. Will try local fallback.", err)
		} else if strings.TrimSpace(content) != "" {
			reviewMapContent = strings.TrimSpace(content)
			logx.Infof("Loaded review-map.md (%d bytes) from remote branch %s", len(reviewMapContent), r.opts.ParentBranchID)
		} else {
			logx.Warningf("Remote review-map.md is empty, will try local fallback.")
		}
	}

	if reviewMapContent == "" && r.opts.WorkspaceDir != "" {
		localPath := filepath.Join(r.opts.WorkspaceDir, "review-map.md")
		logx.Infof("Looking for review-map at local path: %s", localPath)
		if data, err := os.ReadFile(localPath); err == nil {
			reviewMapContent = strings.TrimSpace(string(data))
			logx.Infof("Loaded review-map.md (%d bytes) from local workspace", len(reviewMapContent))
		} else if os.IsNotExist(err) {
			logx.Warningf("review-map.md not found locally at: %s", localPath)
		} else {
			logx.Warningf("Failed to read local review-map.md: %v", err)
		}
	}

	codeAnalysisContext := ""

	// Check if user explicitly wants to skip review-map/analysis based on query
	skipAnalysis := strings.Contains(strings.ToLower(r.opts.Query), "不需要review map") ||
		strings.Contains(strings.ToLower(r.opts.Query), "不需要 review map") ||
		strings.Contains(strings.ToLower(r.opts.Query), "skip review map") ||
		strings.Contains(strings.ToLower(r.opts.Query), "without review map")

	if reviewMapContent == "" && !skipAnalysis {
		logx.Warningf("No review-map.md found (remote or local). Will invoke remote agent to analyze codebase structure.")

		analysisPrompt := `You are a senior software architect. Analyze the codebase structure and provide a concise summary including:
1. Main modules/packages and their responsibilities
2. Key entry points (main files, API endpoints)
3. Important patterns or conventions used
4. Dependencies between modules
5. Any areas that might need special attention for the task

Keep the analysis under 2000 words. Focus on information that would help with task planning.`

		response, newBranchID, err := r.handler.ExecuteAgent("codex", analysisPrompt, r.opts.ParentBranchID)
		if err != nil {
			logx.Warningf("Failed to invoke codex for code analysis: %v. Proceeding without analysis context.", err)
		} else if strings.TrimSpace(response) != "" {
			codeAnalysisContext = strings.TrimSpace(response)
			logx.Infof("Code analysis completed successfully (%d bytes) from branch %s", len(codeAnalysisContext), newBranchID)
		} else {
			logx.Warningf("Code analysis returned empty response from branch %s", newBranchID)
		}
	} else if skipAnalysis {
		logx.Infof("Skipping code analysis as requested by user in query")
	}

	prompt := buildPlanPrompt(r.opts.Query, r.opts.ProjectName, r.opts.ParentBranchID, reviewMapContent, codeAnalysisContext)
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
			logx.Errorf("LLM returned empty content. ToolCalls=%d, Role=%s", len(choice.ToolCalls), choice.Role)
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
