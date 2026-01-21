package tools

import (
	"dev_agent/internal/logx"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
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
	reviewCodeAgent            = "review_code"
	reviewArtifactName         = "code_review.log"
	reviewMaxAttempts          = 3
	instructionFinishedWithErr = "FINISHED_WITH_ERROR"
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

// ToolHandlerTiming configures the default polling behavior for branch status checks.
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

func (h *ToolHandler) BranchRange() map[string]string { return h.branchTracker.Range() }

// ToolCall mirrors brain.ToolCall, but we keep it generic here if needed.
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
		return h.errorPayload(ToolExecutionError{Msg: "Missing tool name in call."})
	}
	var args map[string]any
	if call.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return h.errorPayload(ToolExecutionError{Msg: fmt.Sprintf("Invalid JSON arguments: %v", err)})
		}
	} else {
		args = map[string]any{}
	}

	var res map[string]any
	var err error
	switch name {
	case "execute_agent":
		res, err = h.executeAgent(args)
	case "check_status":
		res, err = h.checkStatus(args)
	case "read_artifact":
		res, err = h.readArtifact(args)
	case "branch_output":
		res, err = h.branchOutput(args)
	default:
		err = ToolExecutionError{Msg: fmt.Sprintf("Unsupported tool: %s", name)}
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

	if agent == reviewCodeAgent {
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
		return nil, "", ToolExecutionError{
			Msg:         fmt.Sprintf("ParallelExplore returned error: %v", resp["error"]),
			Instruction: instructionFinishedWithErr,
		}
	}
	branchID := ExtractBranchID(resp)
	if branchID == "" {
		return nil, "", ToolExecutionError{
			Msg:         fmt.Sprintf("Missing branch id in parallel_explore response: %v", resp),
			Instruction: instructionFinishedWithErr,
		}
	}
	// Don't record branch ID yet - wait until checkStatus succeeds

	result := map[string]any{"parallel_explore": resp, "branch_id": branchID}

	logx.Infof("Waiting for branch %s to complete.", branchID)
	statusResp, err := h.checkStatus(map[string]any{"branch_id": branchID})
	if err != nil {
		// checkStatus failed - don't record this branch ID
		if te, ok := err.(ToolExecutionError); ok {
			// If checkStatus already set FINISHED_WITH_ERROR, propagate it
			if te.Instruction != "" {
				return nil, "", te
			}
			// Otherwise, add the instruction to stop workflow
			te.Instruction = instructionFinishedWithErr
			return nil, "", te
		}
		return nil, "", ToolExecutionError{
			Msg:         fmt.Sprintf("Branch status check failed: %v", err),
			Instruction: instructionFinishedWithErr,
		}
	}

	// Only record branch ID after successful status check
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
	if err != nil {
		return nil, "", err
	} else {
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
		result, branchID, err := h.runAgentOnce(reviewCodeAgent, project, parent, prompt)
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

	logx.Infof("Checking status for branch %s (timeout=%ds)", branchID, int(timeout.Seconds()))
	for attempt := 1; ; attempt++ {
		resp, err := h.client.GetBranch(branchID)
		if err != nil {
			return nil, ToolExecutionError{
				Msg: fmt.Sprintf("GetBranch API call failed for branch %s: %v", branchID, err),
			}
		}

		// Check if the response contains an error (e.g., 404 branch not found)
		if errMsg, ok := resp["error"]; ok {
			return nil, ToolExecutionError{
				Msg: fmt.Sprintf("GetBranch returned error for branch %s: %v", branchID, errMsg),
			}
		}

		// Validate branch id in response
		if id := ExtractBranchID(resp); id != "" {
			// Don't record here - let the caller decide when to record
			// h.branchTracker.Record(id)
		} else {
			return nil, ToolExecutionError{
				Msg: fmt.Sprintf("Branch status response missing branch identifier. Response: %v", resp),
			}
		}

		status := stringsLower(resp["status"])
		latest_snap_id := stringsLower(resp["latest_snap_id"])
		hasNewSnapshot := true
		parent_branch_id := stringsLower(resp["parent_id"])
		if parent_branch_id != "" {
			parent_resp, err := h.client.GetBranch(parent_branch_id)
			if err != nil {
				logx.Errorf("Error getting parent branch %s: %v", parent_branch_id, err)
				hasNewSnapshot = false
			} else {
				parent_latest_snap_id := stringsLower(parent_resp["latest_snap_id"])
				// if the parent branch has the same latest snap id, we can continue to wait for the branch to complete or fail
				if parent_latest_snap_id != "" && parent_latest_snap_id == latest_snap_id {
					hasNewSnapshot = false
				}
			}
		}

		logx.Infof("Branch %s response (attempt %d): %s", branchID, attempt, toJSON(resp))
		if hasNewSnapshot && (status == "succeed" || status == "failed") {
			if status == "failed" {
				details := map[string]any{"status": status}
				if branchID := ExtractBranchID(resp); branchID != "" {
					details["branch_id"] = branchID
				}
				excerpt := ""
				if outResp, err := h.client.BranchOutput(branchID, true); err == nil {
					excerpt = strings.TrimSpace(branchOutputString(outResp))
					if len(excerpt) > 400 {
						excerpt = excerpt[:400] + "..."
					}
				}
				msg := fmt.Sprintf("Branch %s reported failed status. Inspect manifest %s in Pantheon.", branchID, branchID)
				if excerpt != "" {
					msg = fmt.Sprintf("Branch %s reported failed status: %s. Inspect manifest %s in Pantheon.", branchID, excerpt, branchID)
				}
				return nil, ToolExecutionError{
					Msg:         msg,
					Instruction: instructionFinishedWithErr,
					Details:     details,
				}
			}
			return resp, nil
		}

		if h.now().After(deadline) {
			return nil, ToolExecutionError{
				Msg:         fmt.Sprintf("Timed out waiting for branch %s (last status=%s)", branchID, status),
				Instruction: instructionFinishedWithErr,
			}
		}
		logx.Infof("Branch %s still active (status=%s). Sleeping %.1fs.", branchID, status, sleep.Seconds())
		h.sleep(sleep)
		// exponential-ish backoff
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
	logx.Infof("Reading artifact %s from branch %s", path, branchID)
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
	logx.Infof("Retrieving branch_output for %s (full_output=%t)", branchID, fullOutput)
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

func stringsLower(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return stringsTrimLower(s)
}

func stringsTrimLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
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
	if h != nil && h.pollTimeout > 0 {
		return h.pollTimeout
	}
	return defaultPollTimeout
}

func (h *ToolHandler) configuredPollInitial() time.Duration {
	if h != nil && h.pollInitial > 0 {
		return h.pollInitial
	}
	return defaultPollInitial
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

// Tool schema to feed the LLM
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
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
