package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	b "verify_agent/internal/brain"
	cfg "verify_agent/internal/config"
	"verify_agent/internal/logx"
	"verify_agent/internal/streaming"
	"verify_agent/internal/tools"
	"verify_agent/internal/verify"
)

func main() {
	bugDesc := flag.String("bug", "", "Bug description to verify")
	parent := flag.String("parent-branch-id", "", "Branch UUID to fork from (required)")
	project := flag.String("project-name", "", "Override project name")
	headless := flag.Bool("headless", false, "Headless mode (no interactive prompt)")
	streamJSON := flag.Bool("stream-json", false, "Emit workflow events as NDJSON (implies headless)")
	codeContext := flag.String("code-context", "", "Optional: additional code context")
	isFalsePositive := flag.Bool("false-positive", false, "Treat bug as false positive (虚假报警) - agent will try to refute it")
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
		fmt.Printf("you> Enter bug description to verify: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		bug = strings.TrimSpace(line)
	}
	if bug == "" {
		fmt.Fprintln(os.Stderr, "bug description is required")
		os.Exit(1)
	}

	brain := b.NewLLMBrain(conf.AzureAPIKey, conf.AzureEndpoint, conf.AzureDeployment, conf.AzureAPIVersion, 3)
	mcp := tools.NewMCPClient(conf.MCPBaseURL)
	handler := tools.NewToolHandlerWithConfig(mcp, &conf, *parent)

	var streamer *streaming.JSONStreamer
	if streamEnabled {
		streamer = streaming.NewJSONStreamer(true, os.Stdout)
		streamer.EmitThreadStarted(bug, conf.ProjectName, *parent, *headless)
	}

	opts := verify.Options{
		BugDescription:  bug,
		ProjectName:     conf.ProjectName,
		ParentBranchID:  *parent,
		WorkspaceDir:    conf.WorkspaceDir,
		CodeContext:     strings.TrimSpace(*codeContext),
		IsFalsePositive: *isFalsePositive,
	}
	runner, err := verify.NewRunner(brain, handler, streamer, opts)
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
	if result != nil {
		switch result.Status {
		case "bug_wrong":
			status = "bug_wrong"
		case "bug_confirmed":
			status = "bug_confirmed"
		case "cannot_disprove":
			status = "cannot_disprove"
		case "error":
			status = "error"
		}
		if streamer != nil && streamer.Enabled() {
			streamer.EmitThreadCompleted(status, result.Summary, map[string]any{
				"bug_description": result.BugDescription,
				"status":          result.Status,
				"summary":         result.Summary,
				"task1_result":    result.Task1Result,
				"task2_result":    result.Task2Result,
				"task3_result":    result.Task3Result,
			})
		}
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Fprintln(os.Stderr, string(out))
}
