package actions

import (
	"context"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
)

type BridgeInvoker interface {
	Execute(ctx context.Context, correlationID string, operation roblox.Operation, payload map[string]any) (roblox.Response, error)
}

type BridgePingAction struct {
	bridge BridgeInvoker
}

func NewBridgePingAction(bridge BridgeInvoker) BridgePingAction {
	return BridgePingAction{bridge: bridge}
}

func (a BridgePingAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.bridge_ping",
		Name:            "Roblox Bridge Ping",
		Version:         "v1",
		Description:     "Checks health of Roblox Studio bridge connection",
		SideEffectLevel: action.SideEffectRead,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a BridgePingAction) RequiresBridge() bool {
	return true
}

func (a BridgePingAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	resp, err := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpPing, nil)
	if err != nil {
		return action.Result{}, roblox.ToFault(err, req.CorrelationID)
	}
	return action.Result{
		Output: resp.Payload,
	}, nil
}
