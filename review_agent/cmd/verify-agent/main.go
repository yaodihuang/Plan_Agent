package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	cfg "review_agent/internal/config"
	"review_agent/internal/logx"
	"review_agent/internal/prreview"
	"review_agent/internal/streaming"
	t "review_agent/internal/tools"
)

func main() {
	bugDesc := flag.String("bug", "", "Bug / issue text to analyze")
	parent := flag.String("parent-branch-id", "", "Branch UUID to fork from (required)")
	project := flag.String("project-name", "", "Override project name")
	headless := flag.Bool("headless", false, "Headless mode (no interactive prompt)")
	streamJSON := flag.Bool("stream-json", false, "Emit workflow events as NDJSON (implies headless)")
	flag.String("code-context", "", "Optional: additional code context")
	flag.Bool("false-positive", false, "Treat bug as false positive (虚假报警) - agent will try to refute it")
	flag.Parse()

	streamEnabled := streamJSON != nil && *streamJSON
	if streamEnabled {
		*headless = true
		logx.SetLevel(logx.Error)
	}

	conf, err := cfg.FromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	if *project != "" {
		conf.ProjectName = *project
	}
	if conf.ProjectName == "" {
		fmt.Fprintln(os.Stderr, "Project name required via PROJECT_NAME or --project-name")
		os.Exit(1)
	}
	if *parent == "" {
		fmt.Fprintln(os.Stderr, "--parent-branch-id is required")
		os.Exit(1)
	}

	bug := strings.TrimSpace(*bugDesc)
	if bug == "" && !*headless {
		fmt.Printf("you> Enter bug / issue text to analyze: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		bug = strings.TrimSpace(line)
	}
	if bug == "" {
		fmt.Fprintln(os.Stderr, "bug text is required")
		os.Exit(1)
	}

	var streamer *streaming.JSONStreamer
	if streamEnabled {
		streamer = streaming.NewJSONStreamer(true, os.Stdout)
		streamer.EmitThreadStarted(bug, conf.ProjectName, *parent, *headless)
	}

	prompt := prreview.BuildLogicAnalystPrompt(bug)

	mcp := t.NewMCPClient(conf.MCPBaseURL)
	handler := t.NewToolHandlerWithConfig(mcp, &conf, *parent)

	branchID, analysis, err := executeOnce(handler, "codex", prompt, conf.ProjectName, *parent)
	if err != nil {
		if streamer != nil && streamer.Enabled() {
			streamer.EmitError("workflow", err.Error(), nil)
			streamer.EmitThreadCompleted("error", err.Error(), nil)
		}
		fmt.Fprintf(os.Stderr, "workflow error: %v\n", err)
		os.Exit(1)
	}

	if streamer != nil && streamer.Enabled() {
		streamer.EmitThreadCompleted("completed", "ok", map[string]any{
			"bug":       strings.TrimSpace(bug),
			"branch_id": strings.TrimSpace(branchID),
		})
	}

	out := os.Stdout
	if streamer != nil && streamer.Enabled() {
		out = os.Stderr
	}
	fmt.Fprintln(out, strings.TrimSpace(analysis))
}

func executeOnce(handler *t.ToolHandler, agent, prompt, project, parentBranchID string) (string, string, error) {
	args := map[string]any{
		"agent":            agent,
		"prompt":           prompt,
		"project_name":     project,
		"parent_branch_id": parentBranchID,
	}
	payload, _ := json.Marshal(args)
	tc := t.ToolCall{Type: "function"}
	tc.Function.Name = "execute_agent"
	tc.Function.Arguments = string(payload)

	raw := handler.Handle(tc)
	if raw == nil {
		return "", "", fmt.Errorf("tool handler returned nil response")
	}
	if strings.TrimSpace(stringFromAny(raw["status"])) != "success" {
		return "", "", fmt.Errorf("tool error: %v", raw["error"])
	}
	data, _ := raw["data"].(map[string]any)
	return strings.TrimSpace(stringFromAny(data["branch_id"])), strings.TrimSpace(stringFromAny(data["response"])), nil
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}
