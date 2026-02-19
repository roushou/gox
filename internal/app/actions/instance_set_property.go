package actions

import (
	"context"
	"strings"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type InstanceSetPropertyAction struct {
	bridge BridgeInvoker
}

func NewInstanceSetPropertyAction(bridge BridgeInvoker) InstanceSetPropertyAction {
	return InstanceSetPropertyAction{bridge: bridge}
}

func (a InstanceSetPropertyAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.instance_set_property",
		Name:            "Roblox Instance Set Property",
		Version:         "v1",
		Description:     "Sets a Roblox Instance property through the bridge",
		SideEffectLevel: action.SideEffectWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"instancePath": map[string]any{"type": "string", "description": "Dot-separated path to the instance, e.g. 'Workspace.SpawnPlatform'"},
				"instanceId":   map[string]any{"type": "string"},
				"propertyName": map[string]any{"type": "string"},
				"value": map[string]any{
					"description": "Property value. Primitives (string, number, boolean) are passed directly. For Roblox types, use tagged objects: {\"_type\": \"Vector3\", \"x\": 1, \"y\": 2, \"z\": 3}, {\"_type\": \"Color3\", \"r\": 255, \"g\": 0, \"b\": 0}, {\"_type\": \"CFrame\", \"x\": 0, \"y\": 5, \"z\": 0}, {\"_type\": \"UDim2\", \"sx\": 1, \"ox\": 0, \"sy\": 1, \"oy\": 0}, {\"_type\": \"BrickColor\", \"name\": \"Bright red\"}, {\"_type\": \"Enum\", \"enum\": \"Material\", \"value\": \"Neon\"}, {\"_type\": \"Ref\", \"path\": \"Workspace.Model.Part\"} for Instance references (e.g. PrimaryPart)",
				},
			},
			"required": []string{
				"propertyName",
				"value",
			},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a InstanceSetPropertyAction) RequiresBridge() bool {
	return true
}

func (a InstanceSetPropertyAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	instancePath, _ := req.Input["instancePath"].(string)
	instanceID, _ := req.Input["instanceId"].(string)
	if strings.TrimSpace(instancePath) == "" && strings.TrimSpace(instanceID) == "" {
		return action.Result{}, fault.Validation("one of instancePath or instanceId is required")
	}
	propertyName, ok := req.Input["propertyName"].(string)
	if !ok || strings.TrimSpace(propertyName) == "" {
		return action.Result{}, fault.Validation("propertyName is required")
	}
	value, ok := req.Input["value"]
	if !ok {
		return action.Result{}, fault.Validation("value is required")
	}

	payload := map[string]any{
		"propertyName": propertyName,
		"value":        value,
		"dryRun":       req.DryRun,
	}
	if strings.TrimSpace(instancePath) != "" {
		payload["instancePath"] = instancePath
	}
	if strings.TrimSpace(instanceID) != "" {
		payload["instanceId"] = instanceID
	}
	resp, err := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpInstanceSetProperty, payload)
	if err != nil {
		return action.Result{}, roblox.ToFault(err, req.CorrelationID)
	}
	result := action.Result{Output: resp.Payload}
	if summary, ok := resp.Payload["diffSummary"].(string); ok && summary != "" {
		result.Diff = &action.Diff{Summary: summary}
	}
	return result, nil
}
