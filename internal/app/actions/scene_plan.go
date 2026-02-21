package actions

import (
	"context"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type ScenePlanAction struct {
	bridge BridgeInvoker
}

func NewScenePlanAction(bridge BridgeInvoker) ScenePlanAction {
	return ScenePlanAction{bridge: bridge}
}

func (a ScenePlanAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.scene_plan",
		Name:            "Roblox Scene Plan",
		Version:         "v1",
		Description:     "Computes a deterministic scene mutation plan from desired node definitions",
		SideEffectLevel: action.SideEffectRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"rootPath": map[string]any{"type": "string", "description": "Dot-separated path for the managed scene root"},
				"rootId":   map[string]any{"type": "string"},
				"definition": map[string]any{
					"type":                 "object",
					"description":          "Desired scene definition. Expected shape: {\"nodes\": [{\"id\": \"spawn\", \"className\": \"Part\", \"name\": \"Spawn\", \"parentId\": \"root\", \"properties\": {...}}]}",
					"additionalProperties": true,
				},
				"allowDelete": map[string]any{"type": "boolean"},
				"seed": map[string]any{
					"description": "Determinism seed for plan ordering and id generation",
				},
				"maxOps": map[string]any{"type": "integer", "minimum": 1},
			},
			"required":             []string{"definition"},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a ScenePlanAction) RequiresBridge() bool {
	return true
}

func (a ScenePlanAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	payload := map[string]any{}
	if _, _, err := addSceneRoot(req.Input, payload); err != nil {
		return action.Result{}, err
	}

	definition, ok := req.Input["definition"].(map[string]any)
	if !ok {
		return action.Result{}, fault.Validation("definition is required")
	}
	payload["definition"] = definition

	if allowDelete, ok := req.Input["allowDelete"].(bool); ok {
		payload["allowDelete"] = allowDelete
	}
	if seed, ok := req.Input["seed"]; ok {
		payload["seed"] = seed
	}
	if maxOps, ok, err := parsePositiveInteger(req.Input, "maxOps"); err != nil {
		return action.Result{}, fault.Validation(err.Error())
	} else if ok {
		payload["maxOps"] = maxOps
	}

	resp, callErr := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpScenePlan, payload)
	if callErr != nil {
		return action.Result{}, roblox.ToFault(callErr, req.CorrelationID)
	}
	return action.Result{
		Output: resp.Payload,
	}, nil
}
