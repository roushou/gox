package actions

import (
	"context"
	"strings"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type InstanceDeleteAction struct {
	bridge BridgeInvoker
}

func NewInstanceDeleteAction(bridge BridgeInvoker) InstanceDeleteAction {
	return InstanceDeleteAction{bridge: bridge}
}

func (a InstanceDeleteAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.instance_delete",
		Name:            "Roblox Instance Delete",
		Version:         "v1",
		Description:     "Deletes an existing Instance from the Roblox DataModel",
		SideEffectLevel: action.SideEffectWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"instancePath": map[string]any{"type": "string", "description": "Dot-separated path to the instance, e.g. 'Workspace.OldPart'"},
				"instanceId":   map[string]any{"type": "string"},
			},
			"required":             []string{},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a InstanceDeleteAction) RequiresBridge() bool {
	return true
}

func (a InstanceDeleteAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	instancePath, _ := req.Input["instancePath"].(string)
	instanceID, _ := req.Input["instanceId"].(string)
	if strings.TrimSpace(instancePath) == "" && strings.TrimSpace(instanceID) == "" {
		return action.Result{}, fault.Validation("one of instancePath or instanceId is required")
	}

	payload := map[string]any{
		"dryRun": req.DryRun,
	}
	if strings.TrimSpace(instancePath) != "" {
		payload["instancePath"] = instancePath
	}
	if strings.TrimSpace(instanceID) != "" {
		payload["instanceId"] = instanceID
	}
	resp, err := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpInstanceDelete, payload)
	if err != nil {
		return action.Result{}, roblox.ToFault(err, req.CorrelationID)
	}
	result := action.Result{Output: resp.Payload}
	if summary, ok := resp.Payload["diffSummary"].(string); ok && summary != "" {
		result.Diff = &action.Diff{Summary: summary}
	}
	return result, nil
}
