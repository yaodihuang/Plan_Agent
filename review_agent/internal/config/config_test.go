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

func TestFromEnv_DefaultPollTimeoutIs2Hours(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MCP_POLL_TIMEOUT_SECONDS", "")

	conf, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}
	if conf.PollTimeout != 2*time.Hour {
		t.Fatalf("expected default PollTimeout 2h, got %s", conf.PollTimeout)
	}
}

func TestFromEnv_ClampsPollTimeoutBelow2Hours(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MCP_POLL_TIMEOUT_SECONDS", "600")

	conf, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}
	if conf.PollTimeout != 2*time.Hour {
		t.Fatalf("expected PollTimeout clamped to 2h, got %s", conf.PollTimeout)
	}
}

func TestFromEnv_RespectsLargerPollTimeout(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MCP_POLL_TIMEOUT_SECONDS", "10800")

	conf, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}
	if conf.PollTimeout != 3*time.Hour {
		t.Fatalf("expected PollTimeout 3h, got %s", conf.PollTimeout)
	}
}
