package config

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

type AgentConfig struct {
	AzureAPIKey     string
	AzureEndpoint   string
	AzureDeployment string
	AzureAPIVersion string
	MCPBaseURL      string
	ProjectName     string
	WorkspaceDir    string
}

func FromEnv() (AgentConfig, error) {
	_ = loadDotenv(".env")

	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	if apiKey == "" {
		return AgentConfig{}, errors.New("AZURE_OPENAI_API_KEY must be set")
	}

	endpoint := os.Getenv("AZURE_OPENAI_BASE_URL")
	if endpoint == "" {
		return AgentConfig{}, errors.New("AZURE_OPENAI_BASE_URL must be set")
	}
	if !strings.HasPrefix(endpoint, "https://") {
		return AgentConfig{}, errors.New("AZURE_OPENAI_BASE_URL must start with 'https://'")
	}
	endpoint = strings.TrimRight(endpoint, "/")

	deployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
	if deployment == "" {
		return AgentConfig{}, errors.New("AZURE_OPENAI_DEPLOYMENT must be set")
	}

	apiVersion := os.Getenv("AZURE_OPENAI_API_VERSION")
	if apiVersion == "" {
		apiVersion = "2024-12-01-preview"
	}

	baseURL := os.Getenv("MCP_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8000/mcp/sse"
	}
	if !(strings.HasPrefix(baseURL, "http://") || strings.HasPrefix(baseURL, "https://")) {
		return AgentConfig{}, errors.New("MCP_BASE_URL must be a valid HTTP/HTTPS URL")
	}

	project := strings.TrimSpace(os.Getenv("PROJECT_NAME"))
	workspace := os.Getenv("WORKSPACE_DIR")
	if workspace == "" {
		workspace = "/home/pan/workspace"
	}

	return AgentConfig{
		AzureAPIKey:     apiKey,
		AzureEndpoint:   endpoint,
		AzureDeployment: deployment,
		AzureAPIVersion: apiVersion,
		MCPBaseURL:      baseURL,
		ProjectName:     project,
		WorkspaceDir:    workspace,
	}, nil
}

func loadDotenv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "="); i >= 0 {
			key := strings.TrimSpace(line[:i])
			val := strings.TrimSpace(line[i+1:])
			val = trimQuotes(val)
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
	}
	return scanner.Err()
}

func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
