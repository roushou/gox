package actions

import (
	"context"
	"strings"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type ScriptDeleteAction struct {
	bridge BridgeInvoker
}

func NewScriptDeleteAction(bridge BridgeInvoker) ScriptDeleteAction {
	return ScriptDeleteAction{bridge: bridge}
}

func (a ScriptDeleteAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.script_delete",
		Name:            "Roblox Script Delete",
		Version:         "v1",
		Description:     "Deletes an existing Luau script in the Roblox DataModel",
		SideEffectLevel: action.SideEffectWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scriptPath": map[string]any{"type": "string", "description": "Dot-separated path to the script, e.g. 'ServerScriptService.PlayerManager'"},
				"scriptId":   map[string]any{"type": "string"},
			},
			"required":             []string{},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a ScriptDeleteAction) RequiresBridge() bool {
	return true
}

func (a ScriptDeleteAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	scriptPath, _ := req.Input["scriptPath"].(string)
	scriptID, _ := req.Input["scriptId"].(string)
	if strings.TrimSpace(scriptPath) == "" && strings.TrimSpace(scriptID) == "" {
		return action.Result{}, fault.Validation("one of scriptPath or scriptId is required")
	}

	payload := map[string]any{
		"dryRun": req.DryRun,
	}
	if strings.TrimSpace(scriptPath) != "" {
		payload["scriptPath"] = scriptPath
	}
	if strings.TrimSpace(scriptID) != "" {
		payload["scriptId"] = scriptID
	}
	resp, err := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpScriptDelete, payload)
	if err != nil {
		return action.Result{}, roblox.ToFault(err, req.CorrelationID)
	}
	result := action.Result{Output: resp.Payload}
	if summary, ok := resp.Payload["diffSummary"].(string); ok && summary != "" {
		result.Diff = &action.Diff{Summary: summary}
	}
	return result, nil
}
