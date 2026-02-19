package actions

import (
	"context"
	"time"

	"github.com/roushou/gox/internal/domain/action"
)

type PingAction struct{}

func NewPingAction() PingAction {
	return PingAction{}
}

func (PingAction) Spec() action.Spec {
	return action.Spec{
		ID:              "core.ping",
		Name:            "Core Ping",
		Version:         "v1",
		Description:     "Connectivity and readiness probe",
		SideEffectLevel: action.SideEffectNone,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
		},
	}
}

func (PingAction) Handle(_ context.Context, _ action.Request) (action.Result, error) {
	return action.Result{
		Output: map[string]any{
			"ok":         true,
			"serverTime": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}, nil
}
