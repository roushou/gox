package actions

import (
	"context"
	"strings"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type ScriptCreateAction struct {
	bridge BridgeInvoker
}

func NewScriptCreateAction(bridge BridgeInvoker) ScriptCreateAction {
	return ScriptCreateAction{bridge: bridge}
}

func (a ScriptCreateAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.script_create",
		Name:            "Roblox Script Create",
		Version:         "v1",
		Description:     "Creates a Luau script in the Roblox DataModel",
		SideEffectLevel: action.SideEffectWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"parentPath": map[string]any{"type": "string", "description": "Dot-separated path in the DataModel, e.g. 'ServerScriptService' or 'ServerStorage.Modules'"},
				"parentId":   map[string]any{"type": "string"},
				"name":       map[string]any{"type": "string"},
				"scriptType": map[string]any{
					"type": "string",
					"enum": []any{"Script", "LocalScript", "ModuleScript"},
				},
				"source": map[string]any{"type": "string"},
			},
			"required": []string{
				"name",
				"scriptType",
				"source",
			},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a ScriptCreateAction) RequiresBridge() bool {
	return true
}

func (a ScriptCreateAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	parentPath, _ := req.Input["parentPath"].(string)
	parentID, _ := req.Input["parentId"].(string)
	if strings.TrimSpace(parentPath) == "" && strings.TrimSpace(parentID) == "" {
		return action.Result{}, fault.Validation("one of parentPath or parentId is required")
	}
	name, ok := req.Input["name"].(string)
	if !ok || strings.TrimSpace(name) == "" {
		return action.Result{}, fault.Validation("name is required")
	}
	scriptType, ok := req.Input["scriptType"].(string)
	if !ok || strings.TrimSpace(scriptType) == "" {
		return action.Result{}, fault.Validation("scriptType is required")
	}
	source, ok := req.Input["source"].(string)
	if !ok {
		return action.Result{}, fault.Validation("source is required")
	}

	payload := map[string]any{
		"name":       name,
		"scriptType": scriptType,
		"source":     source,
		"dryRun":     req.DryRun,
	}
	if strings.TrimSpace(parentPath) != "" {
		payload["parentPath"] = parentPath
	}
	if strings.TrimSpace(parentID) != "" {
		payload["parentId"] = parentID
	}
	resp, err := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpScriptCreate, payload)
	if err != nil {
		return action.Result{}, roblox.ToFault(err, req.CorrelationID)
	}
	result := action.Result{Output: resp.Payload}
	if summary, ok := resp.Payload["diffSummary"].(string); ok && summary != "" {
		result.Diff = &action.Diff{Summary: summary}
	}
	return result, nil
}
