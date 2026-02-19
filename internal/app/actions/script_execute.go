package actions

import (
	"context"
	"strings"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type ScriptExecuteAction struct {
	bridge BridgeInvoker
}

func NewScriptExecuteAction(bridge BridgeInvoker) ScriptExecuteAction {
	return ScriptExecuteAction{bridge: bridge}
}

func (a ScriptExecuteAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.script_execute",
		Name:            "Roblox Script Execute",
		Version:         "v1",
		Description:     "Executes a script/module through the Roblox bridge (best-effort in Studio plugin context)",
		SideEffectLevel: action.SideEffectWrite,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scriptPath":   map[string]any{"type": "string", "description": "Dot-separated path to a LuaSourceContainer"},
				"scriptId":     map[string]any{"type": "string"},
				"functionName": map[string]any{"type": "string", "description": "Optional exported function name to invoke"},
				"args":         map[string]any{"type": "array", "description": "Optional positional args for function invocation"},
				"forceRequire": map[string]any{"type": "boolean", "description": "If true, force ModuleScript-style require execution"},
				"expectReturn": map[string]any{"type": "boolean", "description": "If true, treat missing return as validation error"},
			},
			"required":             []string{},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a ScriptExecuteAction) RequiresBridge() bool {
	return true
}

func (a ScriptExecuteAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
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
	if functionName, ok := req.Input["functionName"].(string); ok && strings.TrimSpace(functionName) != "" {
		payload["functionName"] = functionName
	}
	if args, ok := req.Input["args"].([]any); ok {
		payload["args"] = args
	}
	if forceRequire, ok := req.Input["forceRequire"].(bool); ok {
		payload["forceRequire"] = forceRequire
	}
	if expectReturn, ok := req.Input["expectReturn"].(bool); ok {
		payload["expectReturn"] = expectReturn
	}

	resp, err := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpScriptExecute, payload)
	if err != nil {
		return action.Result{}, roblox.ToFault(err, req.CorrelationID)
	}
	result := action.Result{Output: resp.Payload}
	if summary, ok := resp.Payload["diffSummary"].(string); ok && summary != "" {
		result.Diff = &action.Diff{Summary: summary}
	}
	return result, nil
}
