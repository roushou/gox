package actions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type fakeBridgeInvoker struct {
	resp      roblox.Response
	err       error
	operation roblox.Operation
	payload   map[string]any
}

func (f *fakeBridgeInvoker) Execute(_ context.Context, _ string, op roblox.Operation, payload map[string]any) (roblox.Response, error) {
	f.operation = op
	f.payload = payload
	if f.err != nil {
		return roblox.Response{}, f.err
	}
	return f.resp, nil
}

func TestScriptCreateActionValidation(t *testing.T) {
	bridge := &fakeBridgeInvoker{}
	a := NewScriptCreateAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input:         map[string]any{},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	f, ok := fault.As(err)
	if !ok || f.Code != fault.CodeValidation {
		t.Fatalf("expected validation fault, got %v", err)
	}
}

func TestScriptCreateActionBridgeFailure(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		err: roblox.BridgeCallError{
			Code:    "ACCESS_DENIED",
			Message: "forbidden",
		},
	}
	a := NewScriptCreateAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"parentPath": "ServerScriptService",
			"name":       "Main",
			"scriptType": "Script",
			"source":     "print('x')",
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	f, ok := fault.As(err)
	if !ok || f.Code != fault.CodeDenied {
		t.Fatalf("expected denied fault, got %v", err)
	}
}

func TestInstanceSetPropertyActionSuccess(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload: map[string]any{
				"updated":     true,
				"diffSummary": "set BrickColor",
			},
			Timestamp: time.Now().UTC(),
		},
	}
	a := NewInstanceSetPropertyAction(bridge)
	out, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"instancePath": "Workspace.Part",
			"propertyName": "BrickColor",
			"value":        "Bright red",
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Diff == nil || out.Diff.Summary == "" {
		t.Fatalf("expected diff summary, got %#v", out.Diff)
	}
	if bridge.operation != roblox.OpInstanceSetProperty {
		t.Fatalf("unexpected operation %q", bridge.operation)
	}
}

func TestBridgePingActionFaultWrap(t *testing.T) {
	a := NewBridgePingAction(&fakeBridgeInvoker{err: errors.New("offline")})
	_, err := a.Handle(context.Background(), action.Request{CorrelationID: "corr-1"})
	if err == nil {
		t.Fatal("expected error")
	}
	f, ok := fault.As(err)
	if !ok || f.Code != fault.CodeInternal {
		t.Fatalf("expected internal fault, got %v", err)
	}
}

func TestScriptUpdateActionUsesBridgeOperation(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload: map[string]any{
				"updated":     true,
				"diffSummary": "updated script source",
			},
			Timestamp: time.Now().UTC(),
		},
	}
	a := NewScriptUpdateAction(bridge)
	out, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"scriptPath": "ServerScriptService.Main",
			"source":     "print('ok')",
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Diff == nil {
		t.Fatalf("expected diff, got %#v", out)
	}
	if bridge.operation != roblox.OpScriptUpdate {
		t.Fatalf("unexpected operation %q", bridge.operation)
	}
}

func TestScriptDeleteActionUsesBridgeOperation(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload: map[string]any{
				"deleted":     true,
				"diffSummary": "deleted script",
			},
			Timestamp: time.Now().UTC(),
		},
	}
	a := NewScriptDeleteAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"scriptPath": "ServerScriptService.Main",
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if bridge.operation != roblox.OpScriptDelete {
		t.Fatalf("unexpected operation %q", bridge.operation)
	}
}

func TestScriptExecuteActionUsesBridgeOperation(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload: map[string]any{
				"executed":    true,
				"diffSummary": "executed script",
			},
			Timestamp: time.Now().UTC(),
		},
	}
	a := NewScriptExecuteAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"scriptPath": "ServerScriptService.BuildOlympus",
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if bridge.operation != roblox.OpScriptExecute {
		t.Fatalf("unexpected operation %q", bridge.operation)
	}
}

func TestInstanceCreateActionUsesBridgeOperation(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload: map[string]any{
				"created":     true,
				"diffSummary": "created Part",
			},
			Timestamp: time.Now().UTC(),
		},
	}
	a := NewInstanceCreateAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"parentPath": "Workspace",
			"className":  "Part",
			"name":       "MyPart",
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if bridge.operation != roblox.OpInstanceCreate {
		t.Fatalf("unexpected operation %q", bridge.operation)
	}
}

func TestInstanceDeleteActionUsesBridgeOperation(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload: map[string]any{
				"deleted":     true,
				"diffSummary": "deleted instance",
			},
			Timestamp: time.Now().UTC(),
		},
	}
	a := NewInstanceDeleteAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"instancePath": "Workspace.MyPart",
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if bridge.operation != roblox.OpInstanceDelete {
		t.Fatalf("unexpected operation %q", bridge.operation)
	}
}

func TestInstanceGetActionValidation(t *testing.T) {
	a := NewInstanceGetAction(&fakeBridgeInvoker{})
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input:         map[string]any{},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	f, ok := fault.As(err)
	if !ok || f.Code != fault.CodeValidation {
		t.Fatalf("expected validation fault, got %v", err)
	}
}

func TestInstanceGetActionUsesBridgeOperation(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload: map[string]any{
				"path":      "Workspace.Part",
				"name":      "Part",
				"className": "Part",
			},
			Timestamp: time.Now().UTC(),
		},
	}
	a := NewInstanceGetAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"instancePath": "Workspace.Part",
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if bridge.operation != roblox.OpInstanceGet {
		t.Fatalf("unexpected operation %q", bridge.operation)
	}
}

func TestInstanceListChildrenActionUsesBridgeOperation(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload: map[string]any{
				"count": 2,
			},
			Timestamp: time.Now().UTC(),
		},
	}
	a := NewInstanceListChildrenAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"parentPath": "Workspace",
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if bridge.operation != roblox.OpInstanceListChildren {
		t.Fatalf("unexpected operation %q", bridge.operation)
	}
	if bridge.payload["limit"] != defaultInstanceListChildrenLimit {
		t.Fatalf("unexpected default limit payload: %#v", bridge.payload)
	}
	if bridge.payload["offset"] != 0 {
		t.Fatalf("unexpected default offset payload: %#v", bridge.payload)
	}
}

func TestInstanceListChildrenActionPaginationPayload(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload: map[string]any{
				"count": 1,
			},
			Timestamp: time.Now().UTC(),
		},
	}
	a := NewInstanceListChildrenAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"parentPath": "Workspace",
			"limit":      2,
			"offset":     3,
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if bridge.payload["limit"] != 2 {
		t.Fatalf("unexpected limit payload: %#v", bridge.payload)
	}
	if bridge.payload["offset"] != 3 {
		t.Fatalf("unexpected offset payload: %#v", bridge.payload)
	}
}

func TestScriptGetSourceActionUsesBridgeOperation(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload: map[string]any{
				"path":   "ServerScriptService.Main",
				"source": "print('ok')",
			},
			Timestamp: time.Now().UTC(),
		},
	}
	a := NewScriptGetSourceAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"scriptPath": "ServerScriptService.Main",
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if bridge.operation != roblox.OpScriptGetSource {
		t.Fatalf("unexpected operation %q", bridge.operation)
	}
}

func TestInstanceFindActionValidationRequiresFilter(t *testing.T) {
	a := NewInstanceFindAction(&fakeBridgeInvoker{})
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"rootPath": "Workspace",
		},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	f, ok := fault.As(err)
	if !ok || f.Code != fault.CodeValidation {
		t.Fatalf("expected validation fault, got %v", err)
	}
}

func TestInstanceFindActionUsesBridgeOperation(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload: map[string]any{
				"count": 1,
			},
			Timestamp: time.Now().UTC(),
		},
	}
	a := NewInstanceFindAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"rootPath":     "Workspace",
			"nameContains": "spawn",
			"limit":        10,
			"offset":       4,
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if bridge.operation != roblox.OpInstanceFind {
		t.Fatalf("unexpected operation %q", bridge.operation)
	}
	if bridge.payload["limit"] != 10 {
		t.Fatalf("unexpected limit payload: %#v", bridge.payload)
	}
	if bridge.payload["offset"] != 4 {
		t.Fatalf("unexpected offset payload: %#v", bridge.payload)
	}
}

func TestInstanceFindActionAcceptsLegacyMaxResults(t *testing.T) {
	bridge := &fakeBridgeInvoker{
		resp: roblox.Response{
			RequestID:     "req-1",
			CorrelationID: "corr-1",
			Success:       true,
			Payload:       map[string]any{"count": 0},
			Timestamp:     time.Now().UTC(),
		},
	}
	a := NewInstanceFindAction(bridge)
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"rootPath":     "Workspace",
			"nameContains": "spawn",
			"maxResults":   7,
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if bridge.payload["limit"] != 7 {
		t.Fatalf("unexpected limit payload: %#v", bridge.payload)
	}
	if _, exists := bridge.payload["maxResults"]; exists {
		t.Fatalf("did not expect maxResults payload key: %#v", bridge.payload)
	}
}

func TestInstanceFindActionRejectsLimitAndMaxResultsTogether(t *testing.T) {
	a := NewInstanceFindAction(&fakeBridgeInvoker{})
	_, err := a.Handle(context.Background(), action.Request{
		CorrelationID: "corr-1",
		Input: map[string]any{
			"rootPath":     "Workspace",
			"nameContains": "spawn",
			"limit":        5,
			"maxResults":   5,
		},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	f, ok := fault.As(err)
	if !ok || f.Code != fault.CodeValidation {
		t.Fatalf("expected validation fault, got %v", err)
	}
}
