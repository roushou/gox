package actions

import (
	"context"
	"strings"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type InstanceCreateAction struct {
	bridge BridgeInvoker
}

func NewInstanceCreateAction(bridge BridgeInvoker) InstanceCreateAction {
	return InstanceCreateAction{bridge: bridge}
}

func (a InstanceCreateAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.instance_create",
		Name:            "Roblox Instance Create",
		Version:         "v1",
		Description:     "Creates an Instance under a parent path in the Roblox DataModel",
		SideEffectLevel: action.SideEffectWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"parentPath": map[string]any{"type": "string", "description": "Dot-separated path in the DataModel, e.g. 'Workspace' or 'ServerStorage.Items'"},
				"parentId":   map[string]any{"type": "string"},
				"className":  map[string]any{"type": "string"},
				"name":       map[string]any{"type": "string"},
			},
			"required":             []string{"className", "name"},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a InstanceCreateAction) RequiresBridge() bool {
	return true
}

func (a InstanceCreateAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	parentPath, _ := req.Input["parentPath"].(string)
	parentID, _ := req.Input["parentId"].(string)
	if strings.TrimSpace(parentPath) == "" && strings.TrimSpace(parentID) == "" {
		return action.Result{}, fault.Validation("one of parentPath or parentId is required")
	}
	className, ok := req.Input["className"].(string)
	if !ok || strings.TrimSpace(className) == "" {
		return action.Result{}, fault.Validation("className is required")
	}
	name, ok := req.Input["name"].(string)
	if !ok || strings.TrimSpace(name) == "" {
		return action.Result{}, fault.Validation("name is required")
	}

	payload := map[string]any{
		"className": className,
		"name":      name,
		"dryRun":    req.DryRun,
	}
	if strings.TrimSpace(parentPath) != "" {
		payload["parentPath"] = parentPath
	}
	if strings.TrimSpace(parentID) != "" {
		payload["parentId"] = parentID
	}
	resp, err := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpInstanceCreate, payload)
	if err != nil {
		return action.Result{}, roblox.ToFault(err, req.CorrelationID)
	}
	result := action.Result{Output: resp.Payload}
	if summary, ok := resp.Payload["diffSummary"].(string); ok && summary != "" {
		result.Diff = &action.Diff{Summary: summary}
	}
	return result, nil
}
