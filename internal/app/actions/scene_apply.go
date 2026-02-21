package actions

import (
	"context"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type SceneApplyAction struct {
	bridge BridgeInvoker
}

func NewSceneApplyAction(bridge BridgeInvoker) SceneApplyAction {
	return SceneApplyAction{bridge: bridge}
}

func (a SceneApplyAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.scene_apply",
		Name:            "Roblox Scene Apply",
		Version:         "v1",
		Description:     "Applies a scene plan transactionally with rollback support",
		SideEffectLevel: action.SideEffectWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"rootPath": map[string]any{"type": "string", "description": "Dot-separated path for the managed scene root"},
				"rootId":   map[string]any{"type": "string"},
				"plan": map[string]any{
					"type":                 "object",
					"description":          "Plan payload from roblox.scene_plan output",
					"additionalProperties": true,
				},
				"definition": map[string]any{
					"type":                 "object",
					"description":          "Desired scene definition; used when plan is omitted",
					"additionalProperties": true,
				},
				"allowDelete":         map[string]any{"type": "boolean"},
				"rollbackOnError":     map[string]any{"type": "boolean"},
				"validateBeforeApply": map[string]any{"type": "boolean"},
				"seed":                map[string]any{},
				"maxOps":              map[string]any{"type": "integer", "minimum": 1},
			},
			"required":             []string{},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a SceneApplyAction) RequiresBridge() bool {
	return true
}

func (a SceneApplyAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	payload := map[string]any{}
	if _, _, err := addSceneRoot(req.Input, payload); err != nil {
		return action.Result{}, err
	}

	_, hasPlan := req.Input["plan"].(map[string]any)
	_, hasDefinition := req.Input["definition"].(map[string]any)
	if !hasPlan && !hasDefinition {
		return action.Result{}, fault.Validation("one of plan or definition is required")
	}

	if plan, ok := req.Input["plan"].(map[string]any); ok {
		payload["plan"] = plan
	}
	if definition, ok := req.Input["definition"].(map[string]any); ok {
		payload["definition"] = definition
	}
	if allowDelete, ok := req.Input["allowDelete"].(bool); ok {
		payload["allowDelete"] = allowDelete
	}
	if rollbackOnError, ok := req.Input["rollbackOnError"].(bool); ok {
		payload["rollbackOnError"] = rollbackOnError
	}
	if validateBeforeApply, ok := req.Input["validateBeforeApply"].(bool); ok {
		payload["validateBeforeApply"] = validateBeforeApply
	}
	if seed, ok := req.Input["seed"]; ok {
		payload["seed"] = seed
	}
	if maxOps, ok, err := parsePositiveInteger(req.Input, "maxOps"); err != nil {
		return action.Result{}, fault.Validation(err.Error())
	} else if ok {
		payload["maxOps"] = maxOps
	}
	payload["dryRun"] = req.DryRun

	resp, callErr := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpSceneApply, payload)
	if callErr != nil {
		return action.Result{}, roblox.ToFault(callErr, req.CorrelationID)
	}
	return action.Result{
		Output: resp.Payload,
		Diff:   diffFromPayload(resp.Payload),
	}, nil
}
