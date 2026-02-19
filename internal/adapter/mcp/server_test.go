package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/roushou/gox/internal/adapter/roblox"
	appactions "github.com/roushou/gox/internal/app/actions"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
	"github.com/roushou/gox/internal/domain/runlog"
	"github.com/roushou/gox/internal/usecase"
)

type fakeCatalog struct {
	specs    []action.Spec
	handlers map[string]action.Handler
}

func (c fakeCatalog) List() []action.Spec {
	return c.specs
}

func (c fakeCatalog) Get(id string) (action.Handler, bool) {
	if c.handlers == nil {
		return nil, false
	}
	handler, ok := c.handlers[id]
	return handler, ok
}

type fakeMetadataHandler struct {
	spec           action.Spec
	supportsDryRun bool
	requiresBridge bool
}

func (h fakeMetadataHandler) Spec() action.Spec {
	return h.spec
}

func (h fakeMetadataHandler) Handle(_ context.Context, _ action.Request) (action.Result, error) {
	return action.Result{}, nil
}

func (h fakeMetadataHandler) SupportsDryRun() bool {
	return h.supportsDryRun
}

func (h fakeMetadataHandler) RequiresBridge() bool {
	return h.requiresBridge
}

type fakeExecutor struct {
	result    usecase.ExecuteActionResult
	err       error
	executeFn func(context.Context, usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error)
}

type noopBridgeInvoker struct{}

func (noopBridgeInvoker) Execute(
	_ context.Context,
	_ string,
	_ roblox.Operation,
	_ map[string]any,
) (roblox.Response, error) {
	return roblox.Response{Success: true}, nil
}

func (e fakeExecutor) Execute(ctx context.Context, cmd usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error) {
	if e.executeFn != nil {
		return e.executeFn(ctx, cmd)
	}
	return e.result, e.err
}

func TestInitialize(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{}, fakeExecutor{})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	initResult := client.InitializeResult()
	if initResult == nil || initResult.ServerInfo == nil {
		t.Fatalf("missing initialize result: %#v", initResult)
	}
	if initResult.ServerInfo.Name != "gox-test" {
		t.Fatalf("unexpected server name: %#v", initResult.ServerInfo)
	}
}

func TestToolsList(t *testing.T) {
	spec := action.Spec{
		ID:              "core.ping",
		Name:            "Ping",
		Description:     "probe",
		Version:         "v1",
		SideEffectLevel: action.SideEffectRead,
		InputSchema: map[string]any{
			"type": "object",
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{spec},
		handlers: map[string]action.Handler{
			"core.ping": fakeMetadataHandler{
				spec:           spec,
				supportsDryRun: false,
				requiresBridge: true,
			},
		},
	}, fakeExecutor{})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	list, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(list.Tools) != 6 {
		t.Fatalf("unexpected tools: %#v", list.Tools)
	}
	tool, ok := toolByName(list.Tools, "core.ping")
	if !ok {
		t.Fatalf("core.ping tool not found: %#v", list.Tools)
	}
	if tool.Title != "Ping" {
		t.Fatalf("unexpected title: %#v", tool)
	}
	if !tool.Annotations.ReadOnlyHint {
		t.Fatalf("expected read-only tool annotation: %#v", tool.Annotations)
	}
	if got := tool.Meta["gox:version"]; got != "v1" {
		t.Fatalf("unexpected version metadata: %#v", tool.Meta)
	}
	if got := tool.Meta["gox:sideEffectLevel"]; got != "read" {
		t.Fatalf("unexpected side effect metadata: %#v", tool.Meta)
	}
	if got := tool.Meta["gox:supportsDryRun"]; got != false {
		t.Fatalf("unexpected supportsDryRun metadata: %#v", tool.Meta)
	}
	if got := tool.Meta["gox:requiresBridge"]; got != true {
		t.Fatalf("unexpected requiresBridge metadata: %#v", tool.Meta)
	}
	if _, ok := toolByName(list.Tools, "workflow.run"); !ok {
		t.Fatalf("workflow.run tool not found: %#v", list.Tools)
	}
	if _, ok := toolByName(list.Tools, "workflow.status"); !ok {
		t.Fatalf("workflow.status tool not found: %#v", list.Tools)
	}
	if _, ok := toolByName(list.Tools, "workflow.cancel"); !ok {
		t.Fatalf("workflow.cancel tool not found: %#v", list.Tools)
	}
	if _, ok := toolByName(list.Tools, "workflow.list"); !ok {
		t.Fatalf("workflow.list tool not found: %#v", list.Tools)
	}
	if _, ok := toolByName(list.Tools, "workflow.logs"); !ok {
		t.Fatalf("workflow.logs tool not found: %#v", list.Tools)
	}
}

func TestToolsCallSuccess(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.ping",
				Name:            "Ping",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
	}, fakeExecutor{
		result: usecase.ExecuteActionResult{
			RunID: "run-1",
			Result: action.Result{
				Output: map[string]any{"ok": true},
			},
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "core.ping", map[string]any{}, "", false)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("expected result")
	}
	structured, ok := resp.Result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("unexpected structured content type %T", resp.Result.StructuredContent)
	}
	if structured["runId"] != "run-1" {
		t.Fatalf("unexpected run id: %#v", structured)
	}
}

func TestToolsCallPassesApprovedMetadata(t *testing.T) {
	var gotApproved bool
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.approved",
				Name:            "Approved",
				Version:         "v1",
				SideEffectLevel: action.SideEffectWrite,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
	}, fakeExecutor{
		executeFn: func(_ context.Context, cmd usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error) {
			gotApproved = cmd.Approved
			return usecase.ExecuteActionResult{
				RunID:  "run-1",
				Result: action.Result{Output: map[string]any{"ok": true}},
			}, nil
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	_, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "core.approved",
		Arguments: map[string]any{},
		Meta: sdkmcp.Meta{
			"approved": true,
		},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if !gotApproved {
		t.Fatal("expected approved metadata to propagate to execute command")
	}
}

func TestToolsCallFaultMapping(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "missing.action",
				Name:            "Missing",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
	}, fakeExecutor{
		err: fault.NotFound("missing action"),
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "missing.action", map[string]any{}, "", false)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != errorCodeNotFound {
		t.Fatalf("unexpected error code %d", resp.Error.Code)
	}
}

func TestResourcesListAndRead(t *testing.T) {
	store := runlog.NewInMemoryStore()
	_ = store.Upsert(context.Background(), runlog.Record{
		RunID:         "run-1",
		CorrelationID: "corr-1",
		Type:          runlog.RunTypeAction,
		Name:          "core.ping",
		Status:        runlog.StatusSucceeded,
		StartedAt:     time.Now().UTC(),
	})
	server := NewServerWithRunlog("gox-test", nil, nil, slog.Default(), fakeCatalog{}, fakeExecutor{}, store)
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	list, err := client.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(list.Resources) < 2 {
		t.Fatalf("expected status/runs resources, got %#v", list.Resources)
	}

	status, err := client.ReadResource(context.Background(), &sdkmcp.ReadResourceParams{URI: "gox://status"})
	if err != nil {
		t.Fatalf("read status resource: %v", err)
	}
	if len(status.Contents) != 1 || status.Contents[0].Text == "" {
		t.Fatalf("unexpected status resource payload: %#v", status)
	}

	runs, err := client.ReadResource(context.Background(), &sdkmcp.ReadResourceParams{URI: "gox://runs"})
	if err != nil {
		t.Fatalf("read runs resource: %v", err)
	}
	if len(runs.Contents) != 1 {
		t.Fatalf("unexpected runs resource payload: %#v", runs)
	}
	var runsOut map[string]any
	if err := json.Unmarshal([]byte(runs.Contents[0].Text), &runsOut); err != nil {
		t.Fatalf("unmarshal runs payload: %v", err)
	}
	if _, ok := runsOut["runs"].([]any); !ok {
		t.Fatalf("unexpected runs payload: %#v", runsOut)
	}
}

func TestServerRuntimeGuards(t *testing.T) {
	server := NewServerWithRuntimeConfig(
		"gox-test",
		nil,
		nil,
		slog.Default(),
		fakeCatalog{
			specs: []action.Spec{
				{
					ID:              "core.ping",
					Name:            "Ping",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
		fakeExecutor{
			result: usecase.ExecuteActionResult{
				RunID:  "run-1",
				Result: action.Result{Output: map[string]any{"ok": true}},
			},
		},
		runlog.NewInMemoryStore(),
		defaultWorkflowRuntimeConfig(),
		ServerRuntimeConfig{
			MaxPayloadBytes: 16,
		},
	)
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	tooBig := callTool(t, client, "core.ping", map[string]any{"a": "abcdefghijklmnopqrstuvwxyz"}, "", false)
	if tooBig.Error == nil {
		t.Fatal("expected payload size error")
	}
	if tooBig.Error.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("unexpected payload error code: %d", tooBig.Error.Code)
	}

	first := callTool(t, client, "core.ping", map[string]any{}, "", false)
	if first.Error != nil {
		t.Fatalf("unexpected first call error: %#v", first.Error)
	}
	second := callTool(t, client, "core.ping", map[string]any{}, "", false)
	if second.Error != nil {
		t.Fatalf("unexpected second call error: %#v", second.Error)
	}
}

func TestToolsListReadQueryPaginationSchema(t *testing.T) {
	registry := action.NewInMemoryRegistry()
	bridge := noopBridgeInvoker{}
	if err := registry.Register(appactions.NewInstanceListChildrenAction(bridge)); err != nil {
		t.Fatalf("register instance_list_children: %v", err)
	}
	if err := registry.Register(appactions.NewInstanceFindAction(bridge)); err != nil {
		t.Fatalf("register instance_find: %v", err)
	}

	server := NewServer("gox-test", nil, nil, slog.Default(), registry, fakeExecutor{})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	list, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	childrenTool, ok := toolByName(list.Tools, "roblox.instance_list_children")
	if !ok {
		t.Fatalf("roblox.instance_list_children tool not found: %#v", list.Tools)
	}
	childrenProps := schemaProperties(t, childrenTool.InputSchema)
	assertSchemaType(t, childrenProps, "parentPath", "string")
	assertSchemaType(t, childrenProps, "limit", "integer")
	assertSchemaType(t, childrenProps, "offset", "integer")

	findTool, ok := toolByName(list.Tools, "roblox.instance_find")
	if !ok {
		t.Fatalf("roblox.instance_find tool not found: %#v", list.Tools)
	}
	findProps := schemaProperties(t, findTool.InputSchema)
	assertSchemaType(t, findProps, "rootPath", "string")
	assertSchemaType(t, findProps, "nameContains", "string")
	assertSchemaType(t, findProps, "className", "string")
	assertSchemaType(t, findProps, "limit", "integer")
	assertSchemaType(t, findProps, "offset", "integer")
	assertSchemaType(t, findProps, "maxResults", "integer")
}

func TestToolsCallValidationPathMapping(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.any",
				Name:            "Any",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
	}, fakeExecutor{
		err: fault.Validation("input.name: must be a string").WithDetails(map[string]any{
			"validation_path": "input.name",
			"validation_rule": "type",
		}),
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "core.any", map[string]any{}, "", false)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	data := decodeErrorData(t, resp.Error)
	if data["validation_path"] != "input.name" {
		t.Fatalf("unexpected validation_path: %#v", data)
	}
	if data["validation_rule"] != "type" {
		t.Fatalf("unexpected validation_rule: %#v", data)
	}
}

func TestToolsCallInternalMapping(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.any",
				Name:            "Any",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
	}, fakeExecutor{
		err: errors.New("boom"),
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "core.any", map[string]any{}, "", false)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	var wireErr *jsonrpc.Error
	if !errors.As(resp.Error, &wireErr) {
		t.Fatalf("expected jsonrpc.Error, got %T", resp.Error)
	}
	if wireErr.Code != jsonrpc.CodeInternalError {
		t.Fatalf("unexpected code: %d", wireErr.Code)
	}
}

func TestWorkflowRunAndStatusSuccess(t *testing.T) {
	var calls int32
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.ping",
				Name:            "Ping",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.ping": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.ping",
					Name:            "Ping",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		executeFn: func(_ context.Context, cmd usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error) {
			atomic.AddInt32(&calls, 1)
			return usecase.ExecuteActionResult{
				RunID: "action-run-1",
				Result: action.Result{
					Output: map[string]any{"actionId": cmd.ActionID},
				},
			}, nil
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	runResp := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.success",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.ping",
					"input":    map[string]any{},
				},
			},
		},
	}, "", false)
	if runResp.Error != nil {
		t.Fatalf("workflow.run error: %#v", runResp.Error)
	}
	runState := structuredMap(t, runResp.Result)
	runID, _ := runState["runId"].(string)
	if runID == "" {
		t.Fatalf("workflow.run missing runId: %#v", runState)
	}

	status := waitForWorkflowStatus(t, client, runID, "succeeded")
	if status["completedSteps"] != float64(1) {
		t.Fatalf("unexpected completed steps: %#v", status)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("unexpected action call count: %d", calls)
	}
}

func TestWorkflowStepConditionsRunAndSkip(t *testing.T) {
	var condActionCalls int32
	var runActionCalls int32
	var skippedActionCalls int32

	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.cond_source",
				Name:            "Condition Source",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
			{
				ID:              "core.cond_run",
				Name:            "Condition Run",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
			{
				ID:              "core.cond_skip",
				Name:            "Condition Skip",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.cond_source": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.cond_source",
					Name:            "Condition Source",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
			"core.cond_run": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.cond_run",
					Name:            "Condition Run",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
			"core.cond_skip": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.cond_skip",
					Name:            "Condition Skip",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		executeFn: func(_ context.Context, cmd usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error) {
			switch cmd.ActionID {
			case "core.cond_source":
				atomic.AddInt32(&condActionCalls, 1)
				return usecase.ExecuteActionResult{
					RunID: "action-source",
					Result: action.Result{
						Output: map[string]any{
							"mode": "green",
							"nested": map[string]any{
								"enabled": true,
							},
						},
					},
				}, nil
			case "core.cond_run":
				atomic.AddInt32(&runActionCalls, 1)
				return usecase.ExecuteActionResult{
					RunID: "action-run",
					Result: action.Result{
						Output: map[string]any{"ok": true},
					},
				}, nil
			case "core.cond_skip":
				atomic.AddInt32(&skippedActionCalls, 1)
				return usecase.ExecuteActionResult{
					RunID: "action-skip",
					Result: action.Result{
						Output: map[string]any{"ok": true},
					},
				}, nil
			default:
				return usecase.ExecuteActionResult{}, fault.NotFound("unexpected action")
			}
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	runResp := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.conditions",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.cond_source",
					"input":    map[string]any{},
				},
				map[string]any{
					"id":       "step-2",
					"actionId": "core.cond_run",
					"when": map[string]any{
						"stepId": "step-1",
						"path":   "nested.enabled",
						"equals": true,
					},
					"input": map[string]any{},
				},
				map[string]any{
					"id":       "step-3",
					"actionId": "core.cond_skip",
					"when": map[string]any{
						"stepId": "step-1",
						"path":   "mode",
						"equals": "red",
					},
					"input": map[string]any{},
				},
			},
		},
	}, "", false)
	if runResp.Error != nil {
		t.Fatalf("workflow.run error: %#v", runResp.Error)
	}
	runID, _ := structuredMap(t, runResp.Result)["runId"].(string)
	state := waitForWorkflowStatus(t, client, runID, "succeeded")
	if state["completedSteps"] != float64(3) {
		t.Fatalf("expected all steps to complete/skip, got %#v", state)
	}

	if atomic.LoadInt32(&condActionCalls) != 1 {
		t.Fatalf("unexpected condition source calls: %d", condActionCalls)
	}
	if atomic.LoadInt32(&runActionCalls) != 1 {
		t.Fatalf("unexpected conditional run calls: %d", runActionCalls)
	}
	if atomic.LoadInt32(&skippedActionCalls) != 0 {
		t.Fatalf("expected skipped action not to run, got %d", skippedActionCalls)
	}

	logsResp := callTool(t, client, "workflow.logs", map[string]any{
		"runId":  runID,
		"stepId": "step-3",
	}, "", false)
	if logsResp.Error != nil {
		t.Fatalf("workflow.logs error: %#v", logsResp.Error)
	}
	logsAny, ok := structuredMap(t, logsResp.Result)["logs"].([]any)
	if !ok {
		t.Fatalf("workflow.logs missing logs array: %#v", logsResp.Result.StructuredContent)
	}
	foundSkipLog := false
	for _, raw := range logsAny {
		entry, _ := raw.(map[string]any)
		message, _ := entry["message"].(string)
		if strings.Contains(message, "step skipped") {
			foundSkipLog = true
			break
		}
	}
	if !foundSkipLog {
		t.Fatalf("expected skip log entry for step-3, got %#v", logsAny)
	}
}

func TestWorkflowRunPassesApprovedFlagToActions(t *testing.T) {
	var sawApproved bool
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.approved_step",
				Name:            "Approved Step",
				Version:         "v1",
				SideEffectLevel: action.SideEffectWrite,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.approved_step": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.approved_step",
					Name:            "Approved Step",
					Version:         "v1",
					SideEffectLevel: action.SideEffectWrite,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		executeFn: func(_ context.Context, cmd usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error) {
			sawApproved = cmd.Approved
			return usecase.ExecuteActionResult{
				RunID: "action-run",
				Result: action.Result{
					Output: map[string]any{"ok": true},
				},
			}, nil
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.approved",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.approved_step",
					"input":    map[string]any{},
				},
			},
		},
		"approved": true,
	}, "", false)
	if resp.Error != nil {
		t.Fatalf("workflow.run error: %#v", resp.Error)
	}
	runID, _ := structuredMap(t, resp.Result)["runId"].(string)
	_ = waitForWorkflowStatus(t, client, runID, "succeeded")
	if !sawApproved {
		t.Fatal("expected approved flag to propagate to workflow action execution")
	}
}

func TestWorkflowStepConditionRejectsForwardReference(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.ping",
				Name:            "Ping",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.ping": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.ping",
					Name:            "Ping",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		result: usecase.ExecuteActionResult{
			RunID: "action-run",
			Result: action.Result{
				Output: map[string]any{"ok": true},
			},
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.invalid-condition",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.ping",
					"when": map[string]any{
						"stepId": "step-2",
						"equals": true,
					},
					"input": map[string]any{},
				},
				map[string]any{
					"id":       "step-2",
					"actionId": "core.ping",
					"input":    map[string]any{},
				},
			},
		},
	}, "", false)
	if resp.Error == nil {
		t.Fatal("expected validation error")
	}
	if resp.Error.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("unexpected error code: %d", resp.Error.Code)
	}
}

func TestWorkflowCancel(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.long_running",
				Name:            "Long Running",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.long_running": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.long_running",
					Name:            "Long Running",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		executeFn: func(ctx context.Context, _ usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error) {
			<-ctx.Done()
			return usecase.ExecuteActionResult{}, ctx.Err()
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	runResp := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.cancel",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.long_running",
					"input":    map[string]any{},
				},
			},
		},
	}, "", false)
	if runResp.Error != nil {
		t.Fatalf("workflow.run error: %#v", runResp.Error)
	}
	runState := structuredMap(t, runResp.Result)
	runID, _ := runState["runId"].(string)
	if runID == "" {
		t.Fatalf("workflow.run missing runId: %#v", runState)
	}

	cancelResp := callTool(t, client, "workflow.cancel", map[string]any{
		"runId": runID,
	}, "", false)
	if cancelResp.Error != nil {
		t.Fatalf("workflow.cancel error: %#v", cancelResp.Error)
	}
	cancelState := structuredMap(t, cancelResp.Result)
	if cancelState["acknowledged"] != true {
		t.Fatalf("expected acknowledged=true: %#v", cancelState)
	}

	status := waitForWorkflowStatus(t, client, runID, "canceled")
	if status["cancelRequested"] != true {
		t.Fatalf("expected cancelRequested=true: %#v", status)
	}
}

func TestWorkflowListAndLogs(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.ping",
				Name:            "Ping",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.ping": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.ping",
					Name:            "Ping",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		result: usecase.ExecuteActionResult{
			RunID: "action-run-1",
			Result: action.Result{
				Output: map[string]any{"ok": true},
			},
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	runResp := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.listlogs",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.ping",
					"input":    map[string]any{},
				},
			},
		},
	}, "", false)
	if runResp.Error != nil {
		t.Fatalf("workflow.run error: %#v", runResp.Error)
	}
	runState := structuredMap(t, runResp.Result)
	runID, _ := runState["runId"].(string)
	if runID == "" {
		t.Fatalf("workflow.run missing runId: %#v", runState)
	}
	_ = waitForWorkflowStatus(t, client, runID, "succeeded")

	listResp := callTool(t, client, "workflow.list", map[string]any{
		"status": "succeeded",
		"limit":  5,
	}, "", false)
	if listResp.Error != nil {
		t.Fatalf("workflow.list error: %#v", listResp.Error)
	}
	listOut := structuredMap(t, listResp.Result)
	runs, ok := listOut["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatalf("expected non-empty runs: %#v", listOut)
	}

	logsResp := callTool(t, client, "workflow.logs", map[string]any{
		"runId": runID,
		"limit": 10,
	}, "", false)
	if logsResp.Error != nil {
		t.Fatalf("workflow.logs error: %#v", logsResp.Error)
	}
	logsOut := structuredMap(t, logsResp.Result)
	logEntries, ok := logsOut["logs"].([]any)
	if !ok || len(logEntries) == 0 {
		t.Fatalf("expected non-empty logs: %#v", logsOut)
	}
}

func TestWorkflowStatusFromPersistedRunlog(t *testing.T) {
	store := runlog.NewInMemoryStore()
	serverA := NewServerWithRunlog("gox-a", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.ping",
				Name:            "Ping",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.ping": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.ping",
					Name:            "Ping",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		result: usecase.ExecuteActionResult{
			RunID: "action-run-1",
			Result: action.Result{
				Output: map[string]any{"ok": true},
			},
		},
	}, store)
	clientA, cleanupA := connectMCPClient(t, serverA)

	runResp := callTool(t, clientA, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.persisted",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.ping",
					"input":    map[string]any{},
				},
			},
		},
	}, "", false)
	if runResp.Error != nil {
		t.Fatalf("workflow.run error: %#v", runResp.Error)
	}
	runID, _ := structuredMap(t, runResp.Result)["runId"].(string)
	if runID == "" {
		t.Fatalf("workflow.run missing runId")
	}
	_ = waitForWorkflowStatus(t, clientA, runID, "succeeded")
	cleanupA()

	serverB := NewServerWithRunlog("gox-b", nil, nil, slog.Default(), fakeCatalog{}, fakeExecutor{}, store)
	clientB, cleanupB := connectMCPClient(t, serverB)
	defer cleanupB()

	statusResp := callTool(t, clientB, "workflow.status", map[string]any{
		"runId": runID,
	}, "", false)
	if statusResp.Error != nil {
		t.Fatalf("workflow.status error: %#v", statusResp.Error)
	}
	status := structuredMap(t, statusResp.Result)
	if status["status"] != "succeeded" {
		t.Fatalf("unexpected persisted status: %#v", status)
	}
	if status["workflowId"] != "wf.persisted" {
		t.Fatalf("unexpected persisted workflow id: %#v", status)
	}
}

func TestWorkflowRunRejectsTooManySteps(t *testing.T) {
	steps := make([]any, 0, defaultWorkflowMaxSteps+1)
	for i := 0; i < defaultWorkflowMaxSteps+1; i++ {
		steps = append(steps, map[string]any{
			"id":       "step",
			"actionId": "core.ping",
			"input":    map[string]any{},
		})
	}

	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.ping",
				Name:            "Ping",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.ping": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.ping",
					Name:            "Ping",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		result: usecase.ExecuteActionResult{
			RunID: "action-run-1",
			Result: action.Result{
				Output: map[string]any{"ok": true},
			},
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id":    "wf.too_many_steps",
			"steps": steps,
		},
	}, "", false)
	if resp.Error == nil {
		t.Fatal("expected validation error")
	}
	if resp.Error.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("unexpected error code: %d", resp.Error.Code)
	}
}

func TestWorkflowConcurrencyLimit(t *testing.T) {
	block := make(chan struct{})
	server := NewServerWithRunlogAndWorkflowConfig("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.block",
				Name:            "Block",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.block": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.block",
					Name:            "Block",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		executeFn: func(ctx context.Context, _ usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error) {
			select {
			case <-ctx.Done():
				return usecase.ExecuteActionResult{}, ctx.Err()
			case <-block:
				return usecase.ExecuteActionResult{
					RunID: "action-run",
					Result: action.Result{
						Output: map[string]any{"ok": true},
					},
				}, nil
			}
		},
	}, runlog.NewInMemoryStore(), WorkflowRuntimeConfig{
		MaxConcurrentRuns: 1,
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	first := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.first",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.block",
					"input":    map[string]any{},
				},
			},
		},
	}, "", false)
	if first.Error != nil {
		t.Fatalf("first workflow.run failed: %#v", first.Error)
	}

	second := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.second",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.block",
					"input":    map[string]any{},
				},
			},
		},
	}, "", false)
	if second.Error == nil {
		t.Fatal("expected concurrency limit error")
	}
	if second.Error.Code != errorCodeDenied {
		t.Fatalf("unexpected error code: %d", second.Error.Code)
	}

	firstRunID, _ := structuredMap(t, first.Result)["runId"].(string)
	_ = callTool(t, client, "workflow.cancel", map[string]any{"runId": firstRunID}, "", false)
	close(block)
}

func TestWorkflowStepRetryPolicy(t *testing.T) {
	var attempts int32
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.retry",
				Name:            "Retry",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.retry": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.retry",
					Name:            "Retry",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		executeFn: func(_ context.Context, _ usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n < 3 {
				return usecase.ExecuteActionResult{}, fault.Internal("temporary failure")
			}
			return usecase.ExecuteActionResult{
				RunID: "action-run",
				Result: action.Result{
					Output: map[string]any{"ok": true},
				},
			}, nil
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.retry",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.retry",
					"input":    map[string]any{},
					"retry": map[string]any{
						"maxAttempts": 3,
						"backoffMs":   0,
					},
				},
			},
		},
	}, "", false)
	if resp.Error != nil {
		t.Fatalf("workflow.run error: %#v", resp.Error)
	}
	runID, _ := structuredMap(t, resp.Result)["runId"].(string)
	_ = waitForWorkflowStatus(t, client, runID, "succeeded")
	if atomic.LoadInt32(&attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestWorkflowStepTimeoutAndRetry(t *testing.T) {
	var attempts int32
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.timeout",
				Name:            "Timeout",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.timeout": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.timeout",
					Name:            "Timeout",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		executeFn: func(ctx context.Context, _ usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				<-ctx.Done()
				return usecase.ExecuteActionResult{}, ctx.Err()
			}
			return usecase.ExecuteActionResult{
				RunID: "action-run",
				Result: action.Result{
					Output: map[string]any{"ok": true},
				},
			}, nil
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.timeout-retry",
			"steps": []any{
				map[string]any{
					"id":        "step-1",
					"actionId":  "core.timeout",
					"input":     map[string]any{},
					"timeoutMs": 30,
					"retry": map[string]any{
						"maxAttempts": 2,
						"backoffMs":   0,
					},
				},
			},
		},
	}, "", false)
	if resp.Error != nil {
		t.Fatalf("workflow.run error: %#v", resp.Error)
	}
	runID, _ := structuredMap(t, resp.Result)["runId"].(string)
	_ = waitForWorkflowStatus(t, client, runID, "succeeded")
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestWorkflowLogsRingBuffer(t *testing.T) {
	server := NewServerWithRunlogAndWorkflowConfig("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.fail",
				Name:            "Fail",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.fail": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.fail",
					Name:            "Fail",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		executeFn: func(_ context.Context, _ usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error) {
			return usecase.ExecuteActionResult{}, fault.Internal("always fail")
		},
	}, runlog.NewInMemoryStore(), WorkflowRuntimeConfig{
		MaxLogsPerRun: 3,
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.logs-ring",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.fail",
					"input":    map[string]any{},
					"retry": map[string]any{
						"maxAttempts": 4,
						"backoffMs":   0,
					},
				},
			},
		},
	}, "", false)
	if resp.Error != nil {
		t.Fatalf("workflow.run error: %#v", resp.Error)
	}
	runID, _ := structuredMap(t, resp.Result)["runId"].(string)
	_ = waitForWorkflowStatus(t, client, runID, "failed")

	logsResp := callTool(t, client, "workflow.logs", map[string]any{
		"runId": runID,
		"limit": 10,
	}, "", false)
	if logsResp.Error != nil {
		t.Fatalf("workflow.logs error: %#v", logsResp.Error)
	}
	logsOut := structuredMap(t, logsResp.Result)
	logEntries, ok := logsOut["logs"].([]any)
	if !ok {
		t.Fatalf("unexpected logs payload: %#v", logsOut)
	}
	if len(logEntries) > 3 {
		t.Fatalf("expected ring buffer size <=3, got %d", len(logEntries))
	}
}

func TestWorkflowListCursorPagination(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.ping",
				Name:            "Ping",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.ping": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.ping",
					Name:            "Ping",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		result: usecase.ExecuteActionResult{
			RunID: "action-run-1",
			Result: action.Result{
				Output: map[string]any{"ok": true},
			},
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	for i := 0; i < 3; i++ {
		resp := callTool(t, client, "workflow.run", map[string]any{
			"definition": map[string]any{
				"id": "wf.page",
				"steps": []any{
					map[string]any{
						"id":       "step-1",
						"actionId": "core.ping",
						"input":    map[string]any{},
					},
				},
			},
		}, "", false)
		if resp.Error != nil {
			t.Fatalf("workflow.run error: %#v", resp.Error)
		}
		runID, _ := structuredMap(t, resp.Result)["runId"].(string)
		_ = waitForWorkflowStatus(t, client, runID, "succeeded")
	}

	page1 := callTool(t, client, "workflow.list", map[string]any{
		"limit": 1,
	}, "", false)
	if page1.Error != nil {
		t.Fatalf("workflow.list page1 error: %#v", page1.Error)
	}
	page1Out := structuredMap(t, page1.Result)
	page1Runs, ok := page1Out["runs"].([]any)
	if !ok || len(page1Runs) != 1 {
		t.Fatalf("unexpected page1 runs: %#v", page1Out)
	}
	cursor, _ := page1Out["nextCursor"].(string)
	if cursor == "" {
		t.Fatalf("expected nextCursor in page1: %#v", page1Out)
	}

	page2 := callTool(t, client, "workflow.list", map[string]any{
		"limit":  1,
		"cursor": cursor,
	}, "", false)
	if page2.Error != nil {
		t.Fatalf("workflow.list page2 error: %#v", page2.Error)
	}
	page2Out := structuredMap(t, page2.Result)
	page2Runs, ok := page2Out["runs"].([]any)
	if !ok || len(page2Runs) != 1 {
		t.Fatalf("unexpected page2 runs: %#v", page2Out)
	}
}

func TestWorkflowSoakRuns(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.ping",
				Name:            "Ping",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.ping": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.ping",
					Name:            "Ping",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		result: usecase.ExecuteActionResult{
			RunID: "action-run",
			Result: action.Result{
				Output: map[string]any{"ok": true},
			},
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	for i := 0; i < 25; i++ {
		resp := callTool(t, client, "workflow.run", map[string]any{
			"definition": map[string]any{
				"id": "wf.soak",
				"steps": []any{
					map[string]any{
						"id":       "step-1",
						"actionId": "core.ping",
						"input":    map[string]any{},
					},
				},
			},
		}, "", false)
		if resp.Error != nil {
			t.Fatalf("workflow.run error at iteration %d: %#v", i, resp.Error)
		}
		runID, _ := structuredMap(t, resp.Result)["runId"].(string)
		if runID == "" {
			t.Fatalf("missing run id at iteration %d", i)
		}
		_ = waitForWorkflowStatus(t, client, runID, "succeeded")
	}

	listResp := callTool(t, client, "workflow.list", map[string]any{"limit": 50}, "", false)
	if listResp.Error != nil {
		t.Fatalf("workflow.list error: %#v", listResp.Error)
	}
	runs, _ := structuredMap(t, listResp.Result)["runs"].([]any)
	if len(runs) < 25 {
		t.Fatalf("expected at least 25 workflow runs, got %d", len(runs))
	}
}

func TestWorkflowLogsFilters(t *testing.T) {
	var attempts int32
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{
		specs: []action.Spec{
			{
				ID:              "core.retry",
				Name:            "Retry",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
				InputSchema:     map[string]any{"type": "object"},
			},
		},
		handlers: map[string]action.Handler{
			"core.retry": fakeMetadataHandler{
				spec: action.Spec{
					ID:              "core.retry",
					Name:            "Retry",
					Version:         "v1",
					SideEffectLevel: action.SideEffectRead,
					InputSchema:     map[string]any{"type": "object"},
				},
			},
		},
	}, fakeExecutor{
		executeFn: func(_ context.Context, _ usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				return usecase.ExecuteActionResult{}, fault.Internal("temporary")
			}
			return usecase.ExecuteActionResult{
				RunID: "action-run",
				Result: action.Result{
					Output: map[string]any{"ok": true},
				},
			}, nil
		},
	})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	runResp := callTool(t, client, "workflow.run", map[string]any{
		"definition": map[string]any{
			"id": "wf.logs-filter",
			"steps": []any{
				map[string]any{
					"id":       "step-1",
					"actionId": "core.retry",
					"retry": map[string]any{
						"maxAttempts": 2,
						"backoffMs":   0,
					},
					"input": map[string]any{},
				},
			},
		},
	}, "", false)
	if runResp.Error != nil {
		t.Fatalf("workflow.run error: %#v", runResp.Error)
	}
	runID, _ := structuredMap(t, runResp.Result)["runId"].(string)
	_ = waitForWorkflowStatus(t, client, runID, "succeeded")

	warnResp := callTool(t, client, "workflow.logs", map[string]any{
		"runId": runID,
		"level": "warn",
	}, "", false)
	if warnResp.Error != nil {
		t.Fatalf("workflow.logs warn filter error: %#v", warnResp.Error)
	}
	warnOut := structuredMap(t, warnResp.Result)
	warnLogs, ok := warnOut["logs"].([]any)
	if !ok || len(warnLogs) == 0 {
		t.Fatalf("expected warn logs: %#v", warnOut)
	}
	for _, raw := range warnLogs {
		entry, _ := raw.(map[string]any)
		if entry["level"] != "warn" {
			t.Fatalf("unexpected non-warn entry: %#v", entry)
		}
	}

	since := time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
	sinceResp := callTool(t, client, "workflow.logs", map[string]any{
		"runId": runID,
		"since": since,
	}, "", false)
	if sinceResp.Error != nil {
		t.Fatalf("workflow.logs since filter error: %#v", sinceResp.Error)
	}
	sinceOut := structuredMap(t, sinceResp.Result)
	sinceLogs, _ := sinceOut["logs"].([]any)
	if len(sinceLogs) != 0 {
		t.Fatalf("expected empty logs for future since filter: %#v", sinceOut)
	}
}

func TestWorkflowListInvalidCursor(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{}, fakeExecutor{})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "workflow.list", map[string]any{
		"cursor": "bad-cursor",
	}, "", false)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("unexpected error code: %d", resp.Error.Code)
	}
}

func TestWorkflowLogsInvalidSince(t *testing.T) {
	server := NewServer("gox-test", nil, nil, slog.Default(), fakeCatalog{}, fakeExecutor{})
	client, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, client, "workflow.logs", map[string]any{
		"runId": "any",
		"since": "not-a-time",
	}, "", false)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("unexpected error code: %d", resp.Error.Code)
	}
}

func toolByName(tools []*sdkmcp.Tool, name string) (*sdkmcp.Tool, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return nil, false
}

func structuredMap(t *testing.T, result *sdkmcp.CallToolResult) map[string]any {
	t.Helper()
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	m, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("unexpected structured content type %T", result.StructuredContent)
	}
	return m
}

func schemaProperties(t *testing.T, schema any) map[string]any {
	t.Helper()
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		t.Fatalf("unexpected schema type %T", schema)
	}
	properties, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected schema.properties type %T", schemaMap["properties"])
	}
	return properties
}

func assertSchemaType(t *testing.T, properties map[string]any, key string, wantType string) {
	t.Helper()
	propertySchema, ok := properties[key].(map[string]any)
	if !ok {
		t.Fatalf("missing or invalid %q schema: %#v", key, properties[key])
	}
	if got := propertySchema["type"]; got != wantType {
		t.Fatalf("unexpected %q schema type: got=%v want=%q", key, got, wantType)
	}
}

func waitForWorkflowStatus(t *testing.T, client *sdkmcp.ClientSession, runID string, wantStatus string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp := callTool(t, client, "workflow.status", map[string]any{
			"runId": runID,
		}, "", false)
		if resp.Error != nil {
			t.Fatalf("workflow.status error: %#v", resp.Error)
		}
		state := structuredMap(t, resp.Result)
		if state["status"] == wantStatus {
			return state
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for workflow status %q", wantStatus)
	return nil
}
