package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	b "plan_agent/internal/brain"
	cfg "plan_agent/internal/config"
	"plan_agent/internal/logx"
	"plan_agent/internal/plan"
	"plan_agent/internal/streaming"
	t "plan_agent/internal/tools"
)

func main() {
	query := flag.String("query", "", "User query to plan for")
	parent := flag.String("parent-branch-id", "", "Optional parent branch id context")
	project := flag.String("project-name", "", "Override project name")
	workspaceDir := flag.String("workspace-dir", "", "Workspace directory for context files (e.g., review-map.md)")
	headless := flag.Bool("headless", false, "Headless mode (no interactive prompt)")
	streamJSON := flag.Bool("stream-json", false, "Emit workflow events as NDJSON (implies headless)")
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
	if *workspaceDir != "" {
		conf.WorkspaceDir = *workspaceDir
	}
	if conf.ProjectName == "" {
		fmt.Fprintln(os.Stderr, "Project name required via PROJECT_NAME or --project-name")
		os.Exit(1)
	}

	q := strings.TrimSpace(*query)
	if q == "" && !*headless {
		fmt.Printf("you> Enter query: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		q = strings.TrimSpace(line)
	}
	if q == "" {
		fmt.Fprintln(os.Stderr, "query is required")
		os.Exit(1)
	}

	brain := b.NewLLMBrain(conf.AzureAPIKey, conf.AzureEndpoint, conf.AzureDeployment, conf.AzureAPIVersion, 3)
	mcp := t.NewMCPClient(conf.MCPBaseURL)
	handler := t.NewToolHandler(mcp, conf.ProjectName, strings.TrimSpace(*parent), conf.WorkspaceDir, nil)

	var streamer *streaming.JSONStreamer
	if streamEnabled {
		streamer = streaming.NewJSONStreamer(true, os.Stdout)
		streamer.EmitThreadStarted(q, conf.ProjectName, strings.TrimSpace(*parent), *headless)
	}

	runner, err := plan.NewRunner(brain, handler, streamer, plan.Options{
		Query:          q,
		ProjectName:    conf.ProjectName,
		ParentBranchID: strings.TrimSpace(*parent),
		WorkspaceDir:   conf.WorkspaceDir,
	})
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

	if streamer != nil && streamer.Enabled() && result != nil {
		streamer.EmitThreadCompleted("completed", "Plan generated", map[string]any{
			"query":       result.Query,
			"project":     result.ProjectName,
			"plan_result": result.PlanResult,
		})
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Fprintln(os.Stderr, string(out))
}
