package actions

import (
	"context"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type SceneCaptureAction struct {
	bridge BridgeInvoker
}

func NewSceneCaptureAction(bridge BridgeInvoker) SceneCaptureAction {
	return SceneCaptureAction{bridge: bridge}
}

func (a SceneCaptureAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.scene_capture",
		Name:            "Roblox Scene Capture",
		Version:         "v1",
		Description:     "Captures a visual representation of a scene subtree when supported by Studio",
		SideEffectLevel: action.SideEffectRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"rootPath": map[string]any{"type": "string", "description": "Dot-separated path for capture focus root"},
				"rootId":   map[string]any{"type": "string"},
				"width":    map[string]any{"type": "integer", "minimum": 64},
				"height":   map[string]any{"type": "integer", "minimum": 64},
				"format":   map[string]any{"type": "string"},
			},
			"required":             []string{},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a SceneCaptureAction) RequiresBridge() bool {
	return true
}

func (a SceneCaptureAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	payload := map[string]any{}
	if _, _, err := addSceneRoot(req.Input, payload); err != nil {
		return action.Result{}, err
	}
	if width, ok, err := parsePositiveInteger(req.Input, "width"); err != nil {
		return action.Result{}, fault.Validation(err.Error())
	} else if ok {
		payload["width"] = width
	}
	if height, ok, err := parsePositiveInteger(req.Input, "height"); err != nil {
		return action.Result{}, fault.Validation(err.Error())
	} else if ok {
		payload["height"] = height
	}
	if format, ok := req.Input["format"].(string); ok && format != "" {
		payload["format"] = format
	}

	resp, callErr := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpSceneCapture, payload)
	if callErr != nil {
		return action.Result{}, roblox.ToFault(callErr, req.CorrelationID)
	}
	return action.Result{
		Output: resp.Payload,
	}, nil
}
