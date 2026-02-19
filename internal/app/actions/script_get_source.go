package actions

import (
	"context"
	"strings"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type ScriptGetSourceAction struct {
	bridge BridgeInvoker
}

func NewScriptGetSourceAction(bridge BridgeInvoker) ScriptGetSourceAction {
	return ScriptGetSourceAction{bridge: bridge}
}

func (a ScriptGetSourceAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.script_get_source",
		Name:            "Roblox Script Get Source",
		Version:         "v1",
		Description:     "Reads Luau source for an existing script in the Roblox DataModel",
		SideEffectLevel: action.SideEffectRead,
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

func (a ScriptGetSourceAction) RequiresBridge() bool {
	return true
}

func (a ScriptGetSourceAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	scriptPath, _ := req.Input["scriptPath"].(string)
	scriptID, _ := req.Input["scriptId"].(string)
	if strings.TrimSpace(scriptPath) == "" && strings.TrimSpace(scriptID) == "" {
		return action.Result{}, fault.Validation("one of scriptPath or scriptId is required")
	}

	payload := map[string]any{}
	if strings.TrimSpace(scriptPath) != "" {
		payload["scriptPath"] = scriptPath
	}
	if strings.TrimSpace(scriptID) != "" {
		payload["scriptId"] = scriptID
	}

	resp, err := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpScriptGetSource, payload)
	if err != nil {
		return action.Result{}, roblox.ToFault(err, req.CorrelationID)
	}
	return action.Result{
		Output: resp.Payload,
	}, nil
}
