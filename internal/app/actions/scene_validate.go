package actions

import (
	"context"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type SceneValidateAction struct {
	bridge BridgeInvoker
}

func NewSceneValidateAction(bridge BridgeInvoker) SceneValidateAction {
	return SceneValidateAction{bridge: bridge}
}

func (a SceneValidateAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.scene_validate",
		Name:            "Roblox Scene Validate",
		Version:         "v1",
		Description:     "Runs geometry and style quality gates for a scene subtree",
		SideEffectLevel: action.SideEffectRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"rootPath": map[string]any{"type": "string", "description": "Dot-separated path for validation root"},
				"rootId":   map[string]any{"type": "string"},
				"rules": map[string]any{
					"type":                 "object",
					"description":          "Validation rule overrides",
					"additionalProperties": true,
				},
				"maxPartChecks": map[string]any{"type": "integer", "minimum": 1},
			},
			"required":             []string{},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a SceneValidateAction) RequiresBridge() bool {
	return true
}

func (a SceneValidateAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	payload := map[string]any{}
	if _, _, err := addSceneRoot(req.Input, payload); err != nil {
		return action.Result{}, err
	}
	if rules, ok := req.Input["rules"].(map[string]any); ok {
		payload["rules"] = rules
	}
	if maxPartChecks, ok, err := parsePositiveInteger(req.Input, "maxPartChecks"); err != nil {
		return action.Result{}, fault.Validation(err.Error())
	} else if ok {
		payload["maxPartChecks"] = maxPartChecks
	}

	resp, callErr := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpSceneValidate, payload)
	if callErr != nil {
		return action.Result{}, roblox.ToFault(callErr, req.CorrelationID)
	}
	return action.Result{
		Output: resp.Payload,
	}, nil
}
