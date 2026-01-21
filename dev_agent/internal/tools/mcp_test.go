package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMCPClientAddsMetaTagToToolCalls(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		invoke func(t *testing.T, c *MCPClient)
	}{
		{
			name: "CallTool",
			invoke: func(t *testing.T, c *MCPClient) {
				t.Helper()
				if _, err := c.CallTool("parallel_explore", map[string]any{"project_name": "proj"}); err != nil {
					t.Fatalf("CallTool failed: %v", err)
				}
			},
		},
		{
			name: "ParallelExplore",
			invoke: func(t *testing.T, c *MCPClient) {
				t.Helper()
				if _, err := c.ParallelExplore("proj", "parent", []string{"prompt"}, "agent", 2); err != nil {
					t.Fatalf("ParallelExplore failed: %v", err)
				}
			},
		},
		{
			name: "GetBranch",
			invoke: func(t *testing.T, c *MCPClient) {
				t.Helper()
				if _, err := c.GetBranch("branch-1"); err != nil {
					t.Fatalf("GetBranch failed: %v", err)
				}
			},
		},
		{
			name: "BranchReadFile",
			invoke: func(t *testing.T, c *MCPClient) {
				t.Helper()
				if _, err := c.BranchReadFile("branch-2", "file.txt"); err != nil {
					t.Fatalf("BranchReadFile failed: %v", err)
				}
			},
		},
		{
			name: "BranchOutput",
			invoke: func(t *testing.T, c *MCPClient) {
				t.Helper()
				if _, err := c.BranchOutput("branch-3", true); err != nil {
					t.Fatalf("BranchOutput failed: %v", err)
				}
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reqCh := make(chan map[string]any, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if err := r.Body.Close(); err != nil {
					t.Fatalf("close body: %v", err)
				}

				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("unmarshal body %q: %v", string(body), err)
				}
				reqCh <- payload

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte(`{"jsonrpc":"2.0","result":{"ok":true}}`)); err != nil {
					t.Fatalf("write response: %v", err)
				}
			}))
			defer srv.Close()

			client := NewMCPClient(srv.URL)
			client.client = srv.Client()
			client.timeout = time.Second

			tc.invoke(t, client)

			select {
			case payload := <-reqCh:
				meta, ok := payload["_meta"].(map[string]any)
				if !ok {
					t.Fatalf("payload missing _meta: %+v", payload)
				}
				v, ok := meta["ai.tidb.pantheon-ai/agent"].(string)
				if !ok || v != "dev_agent" {
					t.Fatalf("unexpected agent tag: %+v", meta)
				}

				params, ok := payload["params"].(map[string]any)
				if !ok {
					t.Fatalf("params not a map in payload: %+v", payload)
				}
				if len(params) == 0 {
					t.Fatalf("params unexpectedly empty")
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("no request captured for %s", tc.name)
			}
		})
	}
}
