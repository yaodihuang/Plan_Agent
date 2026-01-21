package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	b "dev_agent/internal/brain"
	cfg "dev_agent/internal/config"
	"dev_agent/internal/logx"
	o "dev_agent/internal/orchestrator"
	"dev_agent/internal/streaming"
	t "dev_agent/internal/tools"
)

func main() {
	task := flag.String("task", "", "User task description")
	parent := flag.String("parent-branch-id", "", "Parent branch UUID (required)")
	project := flag.String("project-name", "", "Optional project name override")
	headless := flag.Bool("headless", false, "Run in headless mode (no chat prints)")
	streamJSON := flag.Bool("stream-json", false, "Emit orchestration events as NDJSON to stdout (forces headless mode)")
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
		fmt.Fprintln(os.Stderr, "Project name must be provided via PROJECT_NAME or --project-name")
		os.Exit(1)
	}
	if *parent == "" {
		fmt.Fprintln(os.Stderr, "--parent-branch-id is required")
		os.Exit(1)
	}

	tsk := *task
	if tsk == "" {
		promptWriter := os.Stdout
		if streamEnabled {
			promptWriter = os.Stderr
		}
		fmt.Fprintf(promptWriter, "you> Enter task description: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		tsk = strings.TrimSpace(line)
		if tsk == "" {
			fmt.Fprintln(os.Stderr, "error: task is required")
			os.Exit(1)
		}
	}

	brain := b.NewLLMBrain(conf.AzureAPIKey, conf.AzureEndpoint, conf.AzureDeployment, conf.AzureAPIVersion, 3)
	mcp := t.NewMCPClient(conf.MCPBaseURL)
	handler := t.NewToolHandler(mcp, conf.ProjectName, *parent, conf.WorkspaceDir, &t.ToolHandlerTiming{
		PollTimeout: conf.PollTimeout,
		PollInitial: conf.PollInitial,
		PollMax:     conf.PollMax,
		PollBackoff: conf.PollBackoffFactor,
	})

	msgs := o.BuildInitialMessages(tsk, conf.ProjectName, conf.WorkspaceDir, *parent)
	publish := o.PublishOptions{
		GitHubToken:    conf.GitHubToken,
		WorkspaceDir:   conf.WorkspaceDir,
		ParentBranchID: *parent,
		ProjectName:    conf.ProjectName,
		Task:           tsk,
		GitUserName:    conf.GitUserName,
		GitUserEmail:   conf.GitUserEmail,
	}

	var streamer *streaming.JSONStreamer
	if streamEnabled {
		streamer = streaming.NewJSONStreamer(true, os.Stdout)
		streamer.EmitThreadStarted(tsk, conf.ProjectName, *parent, *headless)
	}

	opts := o.RunOptions{
		Publish:  publish,
		Streamer: streamer,
	}

	var report map[string]any
	if *headless {
		report, err = o.Orchestrate(brain, handler, msgs, opts)
	} else {
		report, err = o.ChatLoop(brain, handler, msgs, 0, opts)
	}
	if err != nil {
		if streamer != nil && streamer.Enabled() {
			streamer.EmitError("cli", err.Error(), nil)
			streamer.EmitThreadCompleted("error", err.Error(), nil)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	// Attach observed branch range and instructions
	br := handler.BranchRange()
	if report == nil {
		report = map[string]any{}
	}
	if start, ok := br["start_branch_id"]; ok {
		report["start_branch_id"] = start
	}
	if latest, ok := br["latest_branch_id"]; ok {
		report["latest_branch_id"] = latest
	}
	if _, ok := report["task"]; !ok {
		report["task"] = tsk
	}
	if instr := o.BuildInstructions(report); instr != "" {
		report["instructions"] = instr
	}

	if streamer != nil && streamer.Enabled() {
		status, _ := report["status"].(string)
		summary, _ := report["summary"].(string)
		streamer.EmitThreadCompleted(status, summary, report)
	}

	out, _ := json.MarshalIndent(report, "", "  ")
	fmt.Fprintln(os.Stderr, string(out))
}
