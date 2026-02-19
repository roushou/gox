package actions

import (
	"context"
	"strings"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type ScriptUpdateAction struct {
	bridge BridgeInvoker
}

func NewScriptUpdateAction(bridge BridgeInvoker) ScriptUpdateAction {
	return ScriptUpdateAction{bridge: bridge}
}

func (a ScriptUpdateAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.script_update",
		Name:            "Roblox Script Update",
		Version:         "v1",
		Description:     "Updates source of an existing Luau script in the Roblox DataModel",
		SideEffectLevel: action.SideEffectWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scriptPath": map[string]any{"type": "string", "description": "Dot-separated path to the script, e.g. 'ServerScriptService.PlayerManager'"},
				"scriptId":   map[string]any{"type": "string"},
				"source":     map[string]any{"type": "string"},
			},
			"required":             []string{"source"},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a ScriptUpdateAction) RequiresBridge() bool {
	return true
}

func (a ScriptUpdateAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	scriptPath, _ := req.Input["scriptPath"].(string)
	scriptID, _ := req.Input["scriptId"].(string)
	if strings.TrimSpace(scriptPath) == "" && strings.TrimSpace(scriptID) == "" {
		return action.Result{}, fault.Validation("one of scriptPath or scriptId is required")
	}
	source, ok := req.Input["source"].(string)
	if !ok {
		return action.Result{}, fault.Validation("source is required")
	}

	payload := map[string]any{
		"source": source,
		"dryRun": req.DryRun,
	}
	if strings.TrimSpace(scriptPath) != "" {
		payload["scriptPath"] = scriptPath
	}
	if strings.TrimSpace(scriptID) != "" {
		payload["scriptId"] = scriptID
	}
	resp, err := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpScriptUpdate, payload)
	if err != nil {
		return action.Result{}, roblox.ToFault(err, req.CorrelationID)
	}
	result := action.Result{Output: resp.Payload}
	if summary, ok := resp.Payload["diffSummary"].(string); ok && summary != "" {
		result.Diff = &action.Diff{Summary: summary}
	}
	return result, nil
}
