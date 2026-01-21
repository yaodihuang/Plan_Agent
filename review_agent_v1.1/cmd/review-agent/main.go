package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	b "review_agent/internal/brain"
	cfg "review_agent/internal/config"
	"review_agent/internal/logx"
	"review_agent/internal/prreview"
	"review_agent/internal/streaming"
	t "review_agent/internal/tools"
)

func main() {
	task := flag.String("task", "", "PR context / task description")
	parent := flag.String("parent-branch-id", "", "Branch UUID to fork from (required)")
	project := flag.String("project-name", "", "Override project name")
	headless := flag.Bool("headless", false, "Headless mode (no interactive prompt)")
	streamJSON := flag.Bool("stream-json", false, "Emit workflow events as NDJSON (implies headless)")
	skipScout := flag.Bool("skip-scout", true, "Skip the scout change analysis stage")
	skipTester := flag.Bool("skip-tester", true, "Skip the tester and exchange verification stages")
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

	tsk := strings.TrimSpace(*task)
	if tsk == "" && !*headless {
		fmt.Printf("you> Enter PR review context: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		tsk = strings.TrimSpace(line)
	}
	if tsk == "" {
		fmt.Fprintln(os.Stderr, "task description is required")
		os.Exit(1)
	}

	brain := b.NewLLMBrain(conf.AzureAPIKey, conf.AzureEndpoint, conf.AzureDeployment, conf.AzureAPIVersion, 3)
	mcp := t.NewMCPClient(conf.MCPBaseURL)
	handler := t.NewToolHandlerWithConfig(mcp, &conf, *parent)

	var streamer *streaming.JSONStreamer
	if streamEnabled {
		streamer = streaming.NewJSONStreamer(true, os.Stdout)
		streamer.EmitThreadStarted(tsk, conf.ProjectName, *parent, *headless)
	}

	opts := prreview.Options{
		Task:           tsk,
		ProjectName:    conf.ProjectName,
		ParentBranchID: *parent,
		WorkspaceDir:   conf.WorkspaceDir,
		SkipScout:      *skipScout,
		SkipTester:     *skipTester,
	}
	runner, err := prreview.NewRunner(brain, handler, streamer, opts)
	if err != nil {
		if streamer != nil && streamer.Enabled() {
			streamer.EmitError("config", err.Error(), nil)
			streamer.EmitThreadCompleted("error", err.Error(), nil)
		}
		fmt.Fprintf(os.Stderr, "init error: %v\n", err)
		os.Exit(1)
	}

	result, err := runner.Run()
	if err != nil {
		if streamer != nil && streamer.Enabled() {
			streamer.EmitError("workflow", err.Error(), nil)
			streamer.EmitThreadCompleted("error", err.Error(), nil)
		}
		fmt.Fprintf(os.Stderr, "workflow error: %v\n", err)
		os.Exit(1)
	}

	status := "completed"
	if result != nil && result.Status == "clean" {
		status = "clean"
	}
	if streamer != nil && streamer.Enabled() && result != nil {
		streamer.EmitThreadCompleted(status, result.Summary, map[string]any{
			"task":    result.Task,
			"status":  result.Status,
			"summary": result.Summary,
			"issues":  result.Issues,
		})
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Fprintln(os.Stderr, string(out))
}
