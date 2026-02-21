package actions

import (
	"context"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type SceneSnapshotAction struct {
	bridge BridgeInvoker
}

func NewSceneSnapshotAction(bridge BridgeInvoker) SceneSnapshotAction {
	return SceneSnapshotAction{bridge: bridge}
}

func (a SceneSnapshotAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.scene_snapshot",
		Name:            "Roblox Scene Snapshot",
		Version:         "v1",
		Description:     "Builds a recursive snapshot of a scene subtree with geometry and style metadata",
		SideEffectLevel: action.SideEffectRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"rootPath":           map[string]any{"type": "string", "description": "Dot-separated path for snapshot root, e.g. 'Workspace.MapRoot'"},
				"rootId":             map[string]any{"type": "string"},
				"maxDepth":           map[string]any{"type": "integer", "minimum": 0},
				"maxChildrenPerNode": map[string]any{"type": "integer", "minimum": 1},
				"includeProperties":  map[string]any{"type": "boolean"},
				"includeAttributes":  map[string]any{"type": "boolean"},
				"includeTags":        map[string]any{"type": "boolean"},
			},
			"required":             []string{},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a SceneSnapshotAction) RequiresBridge() bool {
	return true
}

func (a SceneSnapshotAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	payload := map[string]any{}
	if _, _, err := addSceneRoot(req.Input, payload); err != nil {
		return action.Result{}, err
	}

	if maxDepth, ok, err := parseNonNegativeInteger(req.Input, "maxDepth"); err != nil {
		return action.Result{}, fault.Validation(err.Error())
	} else if ok {
		payload["maxDepth"] = maxDepth
	}
	if maxChildren, ok, err := parsePositiveInteger(req.Input, "maxChildrenPerNode"); err != nil {
		return action.Result{}, fault.Validation(err.Error())
	} else if ok {
		payload["maxChildrenPerNode"] = maxChildren
	}
	if includeProperties, ok := req.Input["includeProperties"].(bool); ok {
		payload["includeProperties"] = includeProperties
	}
	if includeAttributes, ok := req.Input["includeAttributes"].(bool); ok {
		payload["includeAttributes"] = includeAttributes
	}
	if includeTags, ok := req.Input["includeTags"].(bool); ok {
		payload["includeTags"] = includeTags
	}

	resp, callErr := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpSceneSnapshot, payload)
	if callErr != nil {
		return action.Result{}, roblox.ToFault(callErr, req.CorrelationID)
	}
	return action.Result{
		Output: resp.Payload,
	}, nil
}
