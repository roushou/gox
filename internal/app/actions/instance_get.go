package actions

import (
	"context"
	"strings"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type InstanceGetAction struct {
	bridge BridgeInvoker
}

func NewInstanceGetAction(bridge BridgeInvoker) InstanceGetAction {
	return InstanceGetAction{bridge: bridge}
}

func (a InstanceGetAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.instance_get",
		Name:            "Roblox Instance Get",
		Version:         "v1",
		Description:     "Reads metadata for a single Instance in the Roblox DataModel",
		SideEffectLevel: action.SideEffectRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"instancePath": map[string]any{"type": "string", "description": "Dot-separated path to the instance, e.g. 'Workspace.SpawnPlatform'"},
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

func (a InstanceGetAction) RequiresBridge() bool {
	return true
}

func (a InstanceGetAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	instancePath, _ := req.Input["instancePath"].(string)
	instanceID, _ := req.Input["instanceId"].(string)
	if strings.TrimSpace(instancePath) == "" && strings.TrimSpace(instanceID) == "" {
		return action.Result{}, fault.Validation("one of instancePath or instanceId is required")
	}

	payload := map[string]any{}
	if strings.TrimSpace(instancePath) != "" {
		payload["instancePath"] = instancePath
	}
	if strings.TrimSpace(instanceID) != "" {
		payload["instanceId"] = instanceID
	}

	resp, err := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpInstanceGet, payload)
	if err != nil {
		return action.Result{}, roblox.ToFault(err, req.CorrelationID)
	}
	return action.Result{
		Output: resp.Payload,
	}, nil
}
