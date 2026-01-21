package config

import (
	"testing"
	"time"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AZURE_OPENAI_API_KEY", "test-key")
	t.Setenv("AZURE_OPENAI_BASE_URL", "https://example.openai.azure.com")
	t.Setenv("AZURE_OPENAI_DEPLOYMENT", "test-deployment")
	t.Setenv("AZURE_OPENAI_API_VERSION", "")
	t.Setenv("MCP_BASE_URL", "http://localhost:8000/mcp/sse")
	t.Setenv("MCP_POLL_INITIAL_SECONDS", "2")
	t.Setenv("MCP_POLL_MAX_SECONDS", "30")
	t.Setenv("MCP_POLL_BACKOFF_FACTOR", "")
	t.Setenv("PROJECT_NAME", "test-project")
	t.Setenv("WORKSPACE_DIR", "/tmp/workspace")
	t.Setenv("GITHUB_TOKEN", "ghp_test")
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
}

func TestFromEnv_DefaultPollTimeoutIs30Minutes(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MCP_POLL_TIMEOUT_SECONDS", "")

	conf, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}
	if conf.PollTimeout != 30*time.Minute {
		t.Fatalf("expected default PollTimeout 30m, got %s", conf.PollTimeout)
	}
}

func TestFromEnv_ClampsPollTimeoutBelow30Minutes(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MCP_POLL_TIMEOUT_SECONDS", "600")

	conf, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}
	if conf.PollTimeout != 30*time.Minute {
		t.Fatalf("expected PollTimeout clamped to 30m, got %s", conf.PollTimeout)
	}
}

func TestFromEnv_RespectsLargerPollTimeout(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MCP_POLL_TIMEOUT_SECONDS", "3600")

	conf, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}
	if conf.PollTimeout != time.Hour {
		t.Fatalf("expected PollTimeout 1h, got %s", conf.PollTimeout)
	}
}
