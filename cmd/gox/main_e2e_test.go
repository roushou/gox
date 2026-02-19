package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCommandStdioWorkflowTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.Command("go", "run", "./cmd/gox")
	cmd.Dir = repoRoot(t)
	cmd.Stderr = io.Discard
	cmd.Env = append(os.Environ(),
		"GOX_LOG_LEVEL=error",
		"GOX_BRIDGE_ENABLED=false",
	)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "gox-e2e-client",
		Version: "v0.1.0",
	}, nil)
	session, err := client.Connect(ctx, &sdkmcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to gox command: %v", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	assertHasTool(t, tools.Tools, "core.ping")
	assertHasTool(t, tools.Tools, "workflow.run")
	assertHasTool(t, tools.Tools, "workflow.status")
	assertHasTool(t, tools.Tools, "workflow.cancel")
	assertHasTool(t, tools.Tools, "workflow.list")
	assertHasTool(t, tools.Tools, "workflow.logs")

	runResult, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "workflow.run",
		Arguments: map[string]any{
			"definition": map[string]any{
				"id": "e2e.workflow",
				"steps": []any{
					map[string]any{
						"id":       "step-1",
						"actionId": "core.ping",
						"input":    map[string]any{},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("workflow.run: %v", err)
	}
	runState := requireStructuredMap(t, runResult)
	runID, _ := runState["runId"].(string)
	if runID == "" {
		t.Fatalf("workflow.run missing runId: %#v", runState)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		statusResult, statusErr := session.CallTool(ctx, &sdkmcp.CallToolParams{
			Name: "workflow.status",
			Arguments: map[string]any{
				"runId": runID,
			},
		})
		if statusErr != nil {
			t.Fatalf("workflow.status: %v", statusErr)
		}
		state := requireStructuredMap(t, statusResult)
		if state["status"] == "succeeded" {
			listResult, listErr := session.CallTool(ctx, &sdkmcp.CallToolParams{
				Name: "workflow.list",
				Arguments: map[string]any{
					"status": "succeeded",
					"limit":  5,
				},
			})
			if listErr != nil {
				t.Fatalf("workflow.list: %v", listErr)
			}
			listOut := requireStructuredMap(t, listResult)
			if _, ok := listOut["runs"].([]any); !ok {
				t.Fatalf("workflow.list missing runs: %#v", listOut)
			}

			logsResult, logsErr := session.CallTool(ctx, &sdkmcp.CallToolParams{
				Name: "workflow.logs",
				Arguments: map[string]any{
					"runId": runID,
					"limit": 5,
				},
			})
			if logsErr != nil {
				t.Fatalf("workflow.logs: %v", logsErr)
			}
			logsOut := requireStructuredMap(t, logsResult)
			logs, ok := logsOut["logs"].([]any)
			if !ok || len(logs) == 0 {
				t.Fatalf("workflow.logs missing entries: %#v", logsOut)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for workflow %q to succeed", runID)
}

func TestCommandPolicyPackFileEnforcement(t *testing.T) {
	testCases := []struct {
		name       string
		packPath   string
		wantDenied bool
		wantReason string
	}{
		{
			name:       "local-dev-allows-ping",
			packPath:   filepath.Join("policy", "packs", "local-dev.json"),
			wantDenied: false,
		},
		{
			name:       "quarantine-denies-ping",
			packPath:   filepath.Join("policy", "packs", "quarantine.json"),
			wantDenied: true,
			wantReason: "server is in quarantine mode",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			session, cleanup := connectCommandSession(t, ctx, []string{
				"GOX_POLICY_MODE=deny-by-default",
				"GOX_POLICY_RULES_PATH=" + filepath.Join(repoRoot(t), tc.packPath),
			})
			defer cleanup()

			result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
				Name:      "core.ping",
				Arguments: map[string]any{},
			})

			if tc.wantDenied {
				if err == nil {
					t.Fatalf("expected denied error, got result %#v", result)
				}
				var rpcErr *jsonrpc.Error
				if !errors.As(err, &rpcErr) {
					t.Fatalf("expected jsonrpc error, got %T: %v", err, err)
				}
				if rpcErr.Code != -32043 {
					t.Fatalf("unexpected error code %d", rpcErr.Code)
				}
				if tc.wantReason != "" && !strings.Contains(rpcErr.Message, tc.wantReason) {
					t.Fatalf("expected deny reason %q in %q", tc.wantReason, rpcErr.Message)
				}
				return
			}

			if err != nil {
				t.Fatalf("core.ping denied unexpectedly: %v", err)
			}
			structured := requireStructuredMap(t, result)
			runID, _ := structured["runId"].(string)
			if runID == "" {
				t.Fatalf("core.ping missing runId: %#v", structured)
			}
		})
	}
}

func connectCommandSession(t *testing.T, ctx context.Context, extraEnv []string) (*sdkmcp.ClientSession, func()) {
	t.Helper()

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/gox")
	cmd.Dir = repoRoot(t)
	cmd.Stderr = io.Discard
	cmd.Env = append(os.Environ(),
		"GOX_LOG_LEVEL=error",
		"GOX_BRIDGE_ENABLED=false",
	)
	cmd.Env = append(cmd.Env, extraEnv...)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "gox-e2e-client",
		Version: "v0.1.0",
	}, nil)
	session, err := client.Connect(ctx, &sdkmcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to gox command: %v", err)
	}
	return session, func() { _ = session.Close() }
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func assertHasTool(t *testing.T, tools []*sdkmcp.Tool, name string) {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return
		}
	}
	t.Fatalf("tool %q not found in tool list", name)
}

func requireStructuredMap(t *testing.T, result *sdkmcp.CallToolResult) map[string]any {
	t.Helper()
	if result == nil {
		t.Fatal("expected tool result")
	}
	typed, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("unexpected structured content type %T", result.StructuredContent)
	}
	return typed
}
