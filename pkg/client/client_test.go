package client

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeSession struct {
	callToolFn      func(context.Context, *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error)
	listResourcesFn func(context.Context, *sdkmcp.ListResourcesParams) (*sdkmcp.ListResourcesResult, error)
	readResourceFn  func(context.Context, *sdkmcp.ReadResourceParams) (*sdkmcp.ReadResourceResult, error)

	callToolCalls      []*sdkmcp.CallToolParams
	listResourcesCalls []*sdkmcp.ListResourcesParams
}

func (f *fakeSession) CallTool(ctx context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
	f.callToolCalls = append(f.callToolCalls, cloneCallToolParams(params))
	if f.callToolFn == nil {
		return nil, errors.New("unexpected call")
	}
	return f.callToolFn(ctx, params)
}

func (f *fakeSession) ListResources(ctx context.Context, params *sdkmcp.ListResourcesParams) (*sdkmcp.ListResourcesResult, error) {
	f.listResourcesCalls = append(f.listResourcesCalls, &sdkmcp.ListResourcesParams{Cursor: params.Cursor})
	if f.listResourcesFn == nil {
		return nil, errors.New("unexpected resources/list")
	}
	return f.listResourcesFn(ctx, params)
}

func (f *fakeSession) ReadResource(ctx context.Context, params *sdkmcp.ReadResourceParams) (*sdkmcp.ReadResourceResult, error) {
	if f.readResourceFn == nil {
		return nil, errors.New("unexpected resources/read")
	}
	return f.readResourceFn(ctx, params)
}

func TestCallActionParsesStructuredOutput(t *testing.T) {
	session := &fakeSession{
		callToolFn: func(_ context.Context, _ *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{
				StructuredContent: map[string]any{
					"runId":  "run-1",
					"output": map[string]any{"ok": true},
					"diff":   map[string]any{"summary": "noop"},
				},
			}, nil
		},
	}
	client := newWithSession(session)

	out, err := client.CallAction(context.Background(), "core.ping", map[string]any{"x": "y"}, CallOptions{
		Actor:         "dev",
		CorrelationID: "corr-1",
		DryRun:        true,
	})
	if err != nil {
		t.Fatalf("call action: %v", err)
	}
	if out.RunID != "run-1" {
		t.Fatalf("unexpected run id: %#v", out)
	}
	if ok, _ := out.Output["ok"].(bool); !ok {
		t.Fatalf("unexpected output: %#v", out.Output)
	}
	if len(session.callToolCalls) != 1 {
		t.Fatalf("unexpected call count: %d", len(session.callToolCalls))
	}
	call := session.callToolCalls[0]
	if call.Name != "core.ping" {
		t.Fatalf("unexpected tool name: %q", call.Name)
	}
	if call.Meta["actor"] != "dev" || call.Meta["correlationId"] != "corr-1" || call.Meta["dryRun"] != true {
		t.Fatalf("unexpected metadata: %#v", call.Meta)
	}
}

func TestCallActionWithRetryRetriesRetryableErrors(t *testing.T) {
	attempt := 0
	session := &fakeSession{
		callToolFn: func(_ context.Context, _ *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
			attempt++
			if attempt == 1 {
				return nil, wireJSONRPCError(jsonrpc.CodeInternalError, "internal", map[string]any{"retryable": true})
			}
			return &sdkmcp.CallToolResult{
				StructuredContent: map[string]any{
					"runId":  "run-2",
					"output": map[string]any{"ok": true},
				},
			}, nil
		},
	}
	client := newWithSession(session)

	out, err := client.CallActionWithRetry(context.Background(), "core.ping", map[string]any{}, CallOptions{}, RetryOptions{
		MaxAttempts:    2,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("call action with retry: %v", err)
	}
	if attempt != 2 {
		t.Fatalf("expected retry attempt, got %d", attempt)
	}
	if out.RunID != "run-2" {
		t.Fatalf("unexpected run id: %#v", out)
	}
}

func TestCallActionWithRetryStopsOnNonRetryableError(t *testing.T) {
	attempt := 0
	session := &fakeSession{
		callToolFn: func(_ context.Context, _ *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
			attempt++
			return nil, wireJSONRPCError(-32043, "denied", map[string]any{"retryable": false})
		},
	}
	client := newWithSession(session)

	_, err := client.CallActionWithRetry(context.Background(), "core.ping", map[string]any{}, CallOptions{}, RetryOptions{
		MaxAttempts:    3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempt != 1 {
		t.Fatalf("expected no retry for non-retryable error, got %d attempts", attempt)
	}
}

func TestWorkflowListAllPaginates(t *testing.T) {
	session := &fakeSession{
		callToolFn: func(_ context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
			cursor := ""
			if args, ok := params.Arguments.(map[string]any); ok {
				if raw, ok := args["cursor"].(string); ok {
					cursor = raw
				}
			}
			switch cursor {
			case "":
				return &sdkmcp.CallToolResult{
					StructuredContent: map[string]any{
						"runs": []any{
							map[string]any{"runId": "run-1", "workflowId": "wf", "correlationId": "c1", "status": "succeeded", "startedAt": "2026-01-01T00:00:00Z", "totalSteps": 1},
						},
						"nextCursor": "1",
					},
				}, nil
			case "1":
				return &sdkmcp.CallToolResult{
					StructuredContent: map[string]any{
						"runs": []any{
							map[string]any{"runId": "run-2", "workflowId": "wf", "correlationId": "c2", "status": "succeeded", "startedAt": "2026-01-01T00:00:01Z", "totalSteps": 1},
						},
					},
				}, nil
			default:
				t.Fatalf("unexpected cursor %q", cursor)
				return nil, nil
			}
		},
	}
	client := newWithSession(session)

	runs, err := client.WorkflowListAll(context.Background(), WorkflowListRequest{Limit: 1})
	if err != nil {
		t.Fatalf("workflow list all: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].RunID != "run-1" || runs[1].RunID != "run-2" {
		t.Fatalf("unexpected runs: %#v", runs)
	}
}

func TestListResourcesAllPaginates(t *testing.T) {
	session := &fakeSession{
		listResourcesFn: func(_ context.Context, params *sdkmcp.ListResourcesParams) (*sdkmcp.ListResourcesResult, error) {
			switch params.Cursor {
			case "":
				return &sdkmcp.ListResourcesResult{
					Resources: []*sdkmcp.Resource{
						{Name: "status", URI: "gox://status"},
					},
					NextCursor: "1",
				}, nil
			case "1":
				return &sdkmcp.ListResourcesResult{
					Resources: []*sdkmcp.Resource{
						{Name: "runs", URI: "gox://runs"},
					},
				}, nil
			default:
				t.Fatalf("unexpected cursor %q", params.Cursor)
				return nil, nil
			}
		},
	}
	client := newWithSession(session)

	resources, err := client.ListResourcesAll(context.Background())
	if err != nil {
		t.Fatalf("list resources all: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}
	if resources[0].URI != "gox://status" || resources[1].URI != "gox://runs" {
		t.Fatalf("unexpected resources: %#v", resources)
	}
}

func TestCallErrorMapping(t *testing.T) {
	session := &fakeSession{
		callToolFn: func(_ context.Context, _ *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
			return nil, wireJSONRPCError(-32043, "blocked by policy", map[string]any{
				"retryable": false,
				"code":      "DENIED",
			})
		},
	}
	client := newWithSession(session)

	_, err := client.WorkflowStatus(context.Background(), "run-1")
	if err == nil {
		t.Fatal("expected error")
	}
	var callErr *CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("expected CallError, got %T", err)
	}
	if callErr.Code != -32043 {
		t.Fatalf("unexpected code: %d", callErr.Code)
	}
	if callErr.Data["code"] != "DENIED" {
		t.Fatalf("unexpected data: %#v", callErr.Data)
	}
}

func wireJSONRPCError(code int64, message string, data map[string]any) error {
	raw, _ := json.Marshal(data)
	return &jsonrpc.Error{
		Code:    code,
		Message: message,
		Data:    raw,
	}
}

func cloneCallToolParams(params *sdkmcp.CallToolParams) *sdkmcp.CallToolParams {
	if params == nil {
		return nil
	}
	cloned := &sdkmcp.CallToolParams{
		Name: params.Name,
	}
	if params.Arguments != nil {
		if args, ok := params.Arguments.(map[string]any); ok {
			clonedArgs := make(map[string]any, len(args))
			for k, v := range args {
				clonedArgs[k] = v
			}
			cloned.Arguments = clonedArgs
		} else {
			cloned.Arguments = params.Arguments
		}
	}
	if params.Meta != nil {
		cloned.Meta = sdkmcp.Meta{}
		for k, v := range params.Meta {
			cloned.Meta[k] = v
		}
	}
	return cloned
}
