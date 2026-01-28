package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"plan_agent/internal/config"
	"plan_agent/internal/logx"
)

type ToolExecutionError struct {
	Msg         string
	Instruction string
	Details     map[string]any
}

func (e ToolExecutionError) Error() string { return e.Msg }

type agentClient interface {
	ParallelExplore(projectName, parentBranchID string, prompts []string, agent string, numBranches int) (map[string]any, error)
	GetBranch(branchID string) (map[string]any, error)
	BranchReadFile(branchID, filePath string) (map[string]any, error)
	BranchOutput(branchID string, fullOutput bool) (map[string]any, error)
}

var _ agentClient = (*MCPClient)(nil)

const (
	instructionFinishedWithErr = "FINISHED_WITH_ERROR"
	reviewArtifactName         = "code_review.log"
	reviewMaxAttempts          = 3
	defaultPollTimeout         = 60 * time.Minute
	defaultPollInitial         = 3 * time.Second
	defaultPollMax             = 30 * time.Second
	defaultPollBackoff         = 1.5
)

type BranchTracker struct {
	start  string
	latest string
}

func NewBranchTracker(start string) *BranchTracker {
	return &BranchTracker{start: start, latest: start}
}

func (t *BranchTracker) Record(id string) {
	if id == "" {
		return
	}
	if t.start == "" {
		t.start = id
	}
	t.latest = id
}

func (t *BranchTracker) Range() map[string]string {
	return map[string]string{"start_branch_id": t.start, "latest_branch_id": t.latest}
}

type ToolHandler struct {
	client        agentClient
	defaultProj   string
	branchTracker *BranchTracker
	workspaceDir  string
	pollTimeout   time.Duration
	pollInitial   time.Duration
	pollMax       time.Duration
	pollBackoff   float64
	nowFunc       func() time.Time
	sleepFunc     func(time.Duration)
}

type ToolHandlerTiming struct {
	PollTimeout time.Duration
	PollInitial time.Duration
	PollMax     time.Duration
	PollBackoff float64
}

func NewToolHandler(client agentClient, defaultProject string, startBranch string, workspaceDir string, timing *ToolHandlerTiming) *ToolHandler {
	handler := &ToolHandler{
		client:        client,
		defaultProj:   defaultProject,
		branchTracker: NewBranchTracker(startBranch),
		workspaceDir:  strings.TrimSpace(workspaceDir),
		pollTimeout:   defaultPollTimeout,
		pollInitial:   defaultPollInitial,
		pollMax:       defaultPollMax,
		pollBackoff:   defaultPollBackoff,
		nowFunc:       time.Now,
		sleepFunc:     time.Sleep,
	}
	if timing != nil {
		if timing.PollTimeout > 0 {
			handler.pollTimeout = timing.PollTimeout
		}
		if timing.PollInitial > 0 {
			handler.pollInitial = timing.PollInitial
		}
		if timing.PollMax > 0 {
			handler.pollMax = timing.PollMax
		}
		if timing.PollBackoff > 1.0 {
			handler.pollBackoff = timing.PollBackoff
		}
	}
	if handler.pollMax < handler.pollInitial {
		handler.pollMax = handler.pollInitial
	}
	return handler
}

func NewToolHandlerWithConfig(client agentClient, cfg *config.AgentConfig, startBranch string) *ToolHandler {
	return &ToolHandler{
		client:        client,
		defaultProj:   cfg.ProjectName,
		branchTracker: NewBranchTracker(startBranch),
		workspaceDir:  strings.TrimSpace(cfg.WorkspaceDir),
		pollTimeout:   cfg.PollTimeout,
		pollInitial:   cfg.PollInitial,
		pollMax:       cfg.PollMax,
		pollBackoff:   cfg.PollBackoffFactor,
		nowFunc:       time.Now,
		sleepFunc:     time.Sleep,
	}
}

func (h *ToolHandler) BranchRange() map[string]string { return h.branchTracker.Range() }

func (h *ToolHandler) StartBranchID() string {
	if h.branchTracker == nil {
		return ""
	}
	return h.branchTracker.start
}

func (h *ToolHandler) ReadRemoteFile(branchID, filePath string) (string, error) {
	if branchID == "" {
		return "", ToolExecutionError{Msg: "branch_id is required for remote file read"}
	}
	if filePath == "" {
		return "", ToolExecutionError{Msg: "file_path is required for remote file read"}
	}
	logx.Infof("Reading remote file %s from branch %s", filePath, branchID)
	resp, err := h.client.BranchReadFile(branchID, filePath)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", ToolExecutionError{Msg: "branch_read_file returned empty response"}
	}
	if errVal, ok := resp["error"]; ok && errVal != nil {
		return "", ToolExecutionError{Msg: fmt.Sprintf("branch_read_file error: %v", errVal)}
	}
	content, _ := resp["content"].(string)
	return content, nil
}

// ExecuteAgent runs a remote agent via MCP and returns the response text and the new branch ID.
// This is a public method that allows the Runner to directly invoke remote agents
// (e.g., codex for code analysis) without going through the tool call loop.
func (h *ToolHandler) ExecuteAgent(agent, prompt, parentBranchID string) (response string, branchID string, err error) {
	if agent == "" {
		return "", "", ToolExecutionError{Msg: "agent name is required"}
	}
	if prompt == "" {
		return "", "", ToolExecutionError{Msg: "prompt is required"}
	}
	if parentBranchID == "" {
		return "", "", ToolExecutionError{Msg: "parent_branch_id is required"}
	}
	project := h.defaultProj
	if project == "" {
		return "", "", ToolExecutionError{Msg: "project name is required but not configured"}
	}

	logx.Infof("ExecuteAgent: invoking %s on project %s from parent %s", agent, project, parentBranchID)

	result, branchID, err := h.runAgentOnce(agent, project, parentBranchID, prompt)
	if err != nil {
		return "", "", err
	}

	responseText := ""
	if resp, ok := result["response"].(string); ok {
		responseText = strings.TrimSpace(resp)
	}

	return responseText, branchID, nil
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (h *ToolHandler) Handle(call ToolCall) map[string]any {
	name := call.Function.Name
	if name == "" {
		return h.errorPayload(ToolExecutionError{Msg: "missing tool name in call"})
	}
	var args map[string]any
	if call.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return h.errorPayload(ToolExecutionError{Msg: fmt.Sprintf("invalid JSON arguments: %v", err)})
		}
	} else {
		args = map[string]any{}
	}

	var res map[string]any
	var err error
	switch name {
	case "execute_agent":
		res, err = h.executeAgent(args)
	case "read_artifact":
		res, err = h.readArtifact(args)
	case "branch_output":
		res, err = h.branchOutput(args)
	case "read_file":
		res, err = h.readLocalFile(args)
	default:
		err = ToolExecutionError{Msg: fmt.Sprintf("unsupported tool: %s", name)}
	}
	if err != nil {
		return h.errorPayload(err)
	}
	return map[string]any{"status": "success", "data": res}
}

func (h *ToolHandler) executeAgent(arguments map[string]any) (map[string]any, error) {
	agent, _ := arguments["agent"].(string)
	prompt, _ := arguments["prompt"].(string)
	project := h.defaultProj
	if v, ok := arguments["project_name"].(string); ok && v != "" {
		project = v
	}
	parent, _ := arguments["parent_branch_id"].(string)

	if agent == "" || prompt == "" || parent == "" || project == "" {
		return nil, ToolExecutionError{Msg: "missing required arguments"}
	}

	if agent == "review_code" {
		return h.executeReviewAgent(project, parent, prompt)
	}
	result, _, err := h.runAgentOnce(agent, project, parent, prompt)
	return result, err
}

func (h *ToolHandler) runAgentOnce(agent, project, parent, prompt string) (map[string]any, string, error) {
	logx.Infof("Executing agent %s on project %s from parent %s", agent, project, parent)
	resp, err := h.client.ParallelExplore(project, parent, []string{prompt}, agent, 1)
	if err != nil {
		return nil, "", ToolExecutionError{
			Msg:         fmt.Sprintf("ParallelExplore failed: %v - %v", err, resp),
			Instruction: instructionFinishedWithErr,
		}
	}
	if isErr, ok := resp["isError"].(bool); ok && isErr {
		errMsg := resp["error"]
		if errMsg == nil {
			return nil, "", ToolExecutionError{
				Msg:         fmt.Sprintf("ParallelExplore returned error (details: %v)", resp),
				Instruction: instructionFinishedWithErr,
			}
		}
		return nil, "", ToolExecutionError{
			Msg:         fmt.Sprintf("ParallelExplore returned error: %v", errMsg),
			Instruction: instructionFinishedWithErr,
		}
	}
	branchID := ExtractBranchID(resp)
	if branchID == "" {
		return nil, "", ToolExecutionError{
			Msg:         fmt.Sprintf("missing branch id in parallel_explore response: %v", resp),
			Instruction: instructionFinishedWithErr,
		}
	}

	result := map[string]any{"parallel_explore": resp, "branch_id": branchID}

	statusResp, err := h.checkStatus(map[string]any{"branch_id": branchID})
	if err != nil {
		if te, ok := err.(ToolExecutionError); ok {
			return nil, "", te
		}
		return nil, "", ToolExecutionError{
			Msg:         fmt.Sprintf("branch status check failed: %v", err),
			Instruction: instructionFinishedWithErr,
		}
	}
	h.branchTracker.Record(branchID)

	result["branch"] = statusResp
	if status, ok := statusResp["status"]; ok {
		result["status"] = status
	}

	responseText := ""
	if out, ok := statusResp["output"].(string); ok && strings.TrimSpace(out) != "" {
		responseText = strings.TrimSpace(out)
	} else if manifest, ok := statusResp["manifest"].(map[string]any); ok {
		if summary, ok := manifest["summary"].(string); ok && strings.TrimSpace(summary) != "" {
			responseText = strings.TrimSpace(summary)
		}
	}

	branchOutputResponse, err := h.client.BranchOutput(branchID, true)
	if err == nil {
		branchOutput := branchOutputString(branchOutputResponse)
		if branchOutput != "" {
			responseText = branchOutput
		}
	}
	if strings.TrimSpace(responseText) == "" {
		return nil, "", ToolExecutionError{Msg: "branch_output returned no textual output"}
	}
	result["response"] = strings.TrimSpace(responseText)

	return result, branchID, nil
}

func (h *ToolHandler) executeReviewAgent(project, parent, prompt string) (map[string]any, error) {
	artifactPath := h.reviewLogPath()
	if artifactPath == "" {
		return nil, ToolExecutionError{Msg: "workspace directory not configured for review_code validation"}
	}
	var lastBranch string
	for attempt := 1; attempt <= reviewMaxAttempts; attempt++ {
		result, branchID, err := h.runAgentOnce("review_code", project, parent, prompt)
		if err != nil {
			return nil, err
		}
		lastBranch = branchID
		if artifact, err := h.client.BranchReadFile(branchID, artifactPath); err == nil {
			if content, ok := artifact["content"].(string); ok && strings.TrimSpace(content) != "" {
				result["review_report"] = content
			}
			return result, nil
		} else if !isNotFoundError(err) {
			return nil, err
		}
		logx.Warningf("review_code attempt %d/%d did not produce %s (branch=%s)", attempt, reviewMaxAttempts, artifactPath, branchID)
	}
	details := map[string]any{
		"attempts":      reviewMaxAttempts,
		"artifact_path": artifactPath,
	}
	if lastBranch != "" {
		details["last_branch_id"] = lastBranch
	}
	msg := fmt.Sprintf("review_code failed to produce %s after %d attempts", artifactPath, reviewMaxAttempts)
	if lastBranch != "" {
		msg = fmt.Sprintf("%s (last_branch_id=%s). Inspect manifest %s in Pantheon.", msg, lastBranch, lastBranch)
	}
	return nil, ToolExecutionError{
		Msg:         msg,
		Instruction: instructionFinishedWithErr,
		Details:     details,
	}
}

func (h *ToolHandler) reviewLogPath() string {
	if strings.TrimSpace(h.workspaceDir) == "" {
		return ""
	}
	return filepath.Join(h.workspaceDir, reviewArtifactName)
}

func (h *ToolHandler) checkStatus(arguments map[string]any) (map[string]any, error) {
	branchID, _ := arguments["branch_id"].(string)
	if branchID == "" {
		return nil, ToolExecutionError{Msg: "`branch_id` is required"}
	}
	timeout := h.configuredTimeout()
	if v, ok := arguments["timeout_seconds"].(float64); ok && v > 0 {
		timeout = durationFromSeconds(v)
	}
	poll := h.configuredPollInitial()
	if v, ok := arguments["poll_interval_seconds"].(float64); ok && v > 0 {
		poll = durationFromSeconds(v)
	}
	maxPoll := h.configuredPollMax(poll)
	if v, ok := arguments["max_poll_interval_seconds"].(float64); ok && v >= poll.Seconds() {
		maxPoll = durationFromSeconds(v)
	}
	backoff := h.configuredPollBackoff()
	deadline := h.now().Add(timeout)
	sleep := poll

	attemptNum := 0

	for {
		attemptNum++
		logx.Infof("Polling branch %s status (attempt %d, next check in %.1fs)...", branchID, attemptNum, sleep.Seconds())
		resp, err := h.client.GetBranch(branchID)
		if err != nil {
			return nil, ToolExecutionError{
				Msg: fmt.Sprintf("GetBranch API call failed for branch %s: %v", branchID, err),
			}
		}
		if errMsg, ok := resp["error"]; ok {
			return nil, ToolExecutionError{
				Msg: fmt.Sprintf("GetBranch returned error for branch %s: %v", branchID, errMsg),
			}
		}
		if id := ExtractBranchID(resp); id == "" {
			return nil, ToolExecutionError{
				Msg: fmt.Sprintf("branch status response missing branch identifier. Response: %v", resp),
			}
		}

		status := stringsLower(resp["status"])
		logx.Infof("Branch %s current status: %s", branchID, status)
		if status == "succeed" {
			logx.Infof("Branch %s completed successfully", branchID)
			return resp, nil
		}
		if status == "failed" {
			details := map[string]any{"status": status, "branch_id": branchID}
			excerpt := ""
			if outResp, err := h.client.BranchOutput(branchID, true); err == nil {
				excerpt = strings.TrimSpace(branchOutputString(outResp))
				if len(excerpt) > 400 {
					excerpt = excerpt[:400] + "..."
				}
			}
			msg := fmt.Sprintf("branch %s reported failed status. Inspect manifest %s in Pantheon.", branchID, branchID)
			if excerpt != "" {
				msg = fmt.Sprintf("branch %s reported failed status: %s. Inspect manifest %s in Pantheon.", branchID, excerpt, branchID)
			}
			return nil, ToolExecutionError{
				Msg:         msg,
				Instruction: instructionFinishedWithErr,
				Details:     details,
			}
		}

		if h.now().After(deadline) {
			return nil, ToolExecutionError{
				Msg:         fmt.Sprintf("timed out waiting for branch %s after %d attempts (last status=%s, timeout=%s)", branchID, attemptNum, status, timeout),
				Instruction: instructionFinishedWithErr,
			}
		}
		logx.Debugf("Branch %s still %s, sleeping for %.1fs before next check", branchID, status, sleep.Seconds())
		h.sleep(sleep)
		next := minFloat(sleep.Seconds()*backoff, maxPoll.Seconds())
		sleep = durationFromSeconds(next)
	}
}

func (h *ToolHandler) readArtifact(arguments map[string]any) (map[string]any, error) {
	branchID, _ := arguments["branch_id"].(string)
	path, _ := arguments["path"].(string)
	if branchID == "" || path == "" {
		return nil, ToolExecutionError{Msg: "`branch_id` and `path` are required"}
	}
	return h.client.BranchReadFile(branchID, path)
}

func (h *ToolHandler) branchOutput(arguments map[string]any) (map[string]any, error) {
	rawBranchID, _ := arguments["branch_id"].(string)
	branchID := strings.TrimSpace(rawBranchID)
	if branchID == "" {
		return nil, ToolExecutionError{Msg: "`branch_id` is required"}
	}
	fullOutput := false
	if v, ok := arguments["full_output"]; ok {
		flag, ok := v.(bool)
		if !ok {
			return nil, ToolExecutionError{Msg: "`full_output` must be a boolean"}
		}
		fullOutput = flag
	}
	return h.client.BranchOutput(branchID, fullOutput)
}

func ExtractBranchID(m map[string]any) string {
	if m == nil {
		return ""
	}
	if pe, ok := m["parallel_explore"].(map[string]any); ok {
		if branches, ok := pe["branches"].([]any); ok {
			for _, item := range branches {
				if nested, _ := item.(map[string]any); nested != nil {
					if id := ExtractBranchID(nested); id != "" {
						return id
					}
				}
			}
		}
	}
	if branches, ok := m["branches"].([]any); ok {
		for _, item := range branches {
			if nested, _ := item.(map[string]any); nested != nil {
				if id := ExtractBranchID(nested); id != "" {
					return id
				}
			}
		}
	}
	if b, ok := m["branch"].(map[string]any); ok {
		if id := ExtractBranchID(b); id != "" {
			return id
		}
	}
	for _, k := range []string{"branch_id", "id"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func branchOutputString(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if out, ok := payload["output"].(string); ok {
		return strings.TrimSpace(out)
	}
	return ""
}

func (h *ToolHandler) errorPayload(err error) map[string]any {
	if err == nil {
		return map[string]any{"status": "error", "error": "unknown error"}
	}
	if te, ok := err.(ToolExecutionError); ok {
		payload := map[string]any{}
		if strings.TrimSpace(te.Msg) != "" {
			payload["message"] = strings.TrimSpace(te.Msg)
		}
		if te.Instruction != "" {
			payload["instruction"] = te.Instruction
		}
		if len(te.Details) > 0 {
			payload["details"] = te.Details
		}
		if len(payload) == 0 {
			payload["message"] = "tool execution error"
		}
		return map[string]any{"status": "error", "error": payload}
	}
	return map[string]any{"status": "error", "error": err.Error()}
}

func (h *ToolHandler) now() time.Time {
	if h != nil && h.nowFunc != nil {
		return h.nowFunc()
	}
	return time.Now()
}

func (h *ToolHandler) sleep(d time.Duration) {
	if h != nil && h.sleepFunc != nil {
		h.sleepFunc(d)
		return
	}
	time.Sleep(d)
}

func (h *ToolHandler) configuredTimeout() time.Duration {
	timeout := defaultPollTimeout
	if h != nil && h.pollTimeout > 0 {
		timeout = h.pollTimeout
	}
	return timeout
}

func (h *ToolHandler) configuredPollInitial() time.Duration {
	poll := defaultPollInitial
	if h != nil && h.pollInitial > 0 {
		poll = h.pollInitial
	}
	return poll
}

func (h *ToolHandler) configuredPollMax(poll time.Duration) time.Duration {
	max := defaultPollMax
	if h != nil && h.pollMax > 0 {
		max = h.pollMax
	}
	if max < poll {
		return poll
	}
	return max
}

func (h *ToolHandler) configuredPollBackoff() float64 {
	if h != nil && h.pollBackoff > 1.0 {
		return h.pollBackoff
	}
	return defaultPollBackoff
}

func durationFromSeconds(v float64) time.Duration {
	return time.Duration(v * float64(time.Second))
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404")
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func stringsLower(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return strings.ToLower(strings.TrimSpace(s))
}

const maxLocalFileSize = 1 << 20 // 1 MB

func (h *ToolHandler) readLocalFile(arguments map[string]any) (map[string]any, error) {
	rawPath, _ := arguments["path"].(string)
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return nil, ToolExecutionError{Msg: "`path` is required"}
	}

	// Security: resolve to absolute and ensure within workspaceDir
	absPath := path
	if !filepath.IsAbs(path) {
		if h.workspaceDir == "" {
			return nil, ToolExecutionError{Msg: "relative path not allowed without workspace directory"}
		}
		absPath = filepath.Join(h.workspaceDir, path)
	}
	absPath = filepath.Clean(absPath)

	if h.workspaceDir != "" {
		wsAbs := filepath.Clean(h.workspaceDir)
		if !strings.HasPrefix(absPath, wsAbs+string(filepath.Separator)) && absPath != wsAbs {
			return nil, ToolExecutionError{Msg: fmt.Sprintf("path %q is outside workspace directory", path)}
		}
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ToolExecutionError{Msg: fmt.Sprintf("file not found: %s", path)}
		}
		return nil, ToolExecutionError{Msg: fmt.Sprintf("cannot stat file: %v", err)}
	}
	if info.IsDir() {
		return nil, ToolExecutionError{Msg: fmt.Sprintf("path is a directory: %s", path)}
	}
	if info.Size() > maxLocalFileSize {
		return nil, ToolExecutionError{Msg: fmt.Sprintf("file too large (%d bytes, max %d)", info.Size(), maxLocalFileSize)}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, ToolExecutionError{Msg: fmt.Sprintf("failed to read file: %v", err)}
	}

	return map[string]any{
		"path":    path,
		"content": string(data),
		"size":    info.Size(),
	}, nil
}

func GetToolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "execute_agent",
				"description": "Launch an MCP parallel_explore job for a specialist agent.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"agent":            map[string]any{"type": "string", "description": "Target specialist agent name."},
						"prompt":           map[string]any{"type": "string", "description": "Prompt for the agent."},
						"project_name":     map[string]any{"type": "string", "description": "Pantheon project name."},
						"parent_branch_id": map[string]any{"type": "string", "description": "Branch UUID to branch from."},
					},
					"required": []any{"agent", "prompt", "project_name", "parent_branch_id"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "read_artifact",
				"description": "Read a text artifact produced by a branch.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"branch_id": map[string]any{"type": "string", "description": "Branch that produced the artifact."},
						"path":      map[string]any{"type": "string", "description": "Artifact path or filename."},
					},
					"required": []any{"branch_id", "path"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "branch_output",
				"description": "Retrieve the text output that a branch produced.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"branch_id":   map[string]any{"type": "string", "description": "Branch that produced the output."},
						"full_output": map[string]any{"type": "boolean", "description": "Return the complete output log instead of any default truncation."},
					},
					"required": []any{"branch_id"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "read_file",
				"description": "Read a local file from the workspace directory. Use this to load context files like review-map.md before planning.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string", "description": "File path relative to workspace directory, or absolute path within workspace."},
					},
					"required": []any{"path"},
				},
			},
		},
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
