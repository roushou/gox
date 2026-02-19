package actions

import (
	"context"
	"strings"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type InstanceListChildrenAction struct {
	bridge BridgeInvoker
}

const (
	defaultInstanceListChildrenLimit = 100
	maxInstanceListChildrenLimit     = 500
)

func NewInstanceListChildrenAction(bridge BridgeInvoker) InstanceListChildrenAction {
	return InstanceListChildrenAction{bridge: bridge}
}

func (a InstanceListChildrenAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.instance_list_children",
		Name:            "Roblox Instance List Children",
		Version:         "v1",
		Description:     "Lists direct children for an Instance path in the Roblox DataModel",
		SideEffectLevel: action.SideEffectRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"parentPath": map[string]any{"type": "string", "description": "Dot-separated path in the DataModel, e.g. 'Workspace' or 'ReplicatedStorage'"},
				"parentId":   map[string]any{"type": "string"},
				"limit":      map[string]any{"type": "integer"},
				"offset":     map[string]any{"type": "integer"},
			},
			"required":             []string{},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a InstanceListChildrenAction) RequiresBridge() bool {
	return true
}

func (a InstanceListChildrenAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	parentPath, _ := req.Input["parentPath"].(string)
	parentID, _ := req.Input["parentId"].(string)
	if strings.TrimSpace(parentPath) == "" && strings.TrimSpace(parentID) == "" {
		return action.Result{}, fault.Validation("one of parentPath or parentId is required")
	}

	limit, hasLimit, err := parsePositiveInteger(req.Input, "limit")
	if err != nil {
		return action.Result{}, fault.Validation(err.Error())
	}
	if !hasLimit {
		limit = defaultInstanceListChildrenLimit
	}
	if limit > maxInstanceListChildrenLimit {
		limit = maxInstanceListChildrenLimit
	}
	offset, hasOffset, err := parseNonNegativeInteger(req.Input, "offset")
	if err != nil {
		return action.Result{}, fault.Validation(err.Error())
	}
	if !hasOffset {
		offset = 0
	}

	payload := map[string]any{
		"limit":  limit,
		"offset": offset,
	}
	if strings.TrimSpace(parentPath) != "" {
		payload["parentPath"] = parentPath
	}
	if strings.TrimSpace(parentID) != "" {
		payload["parentId"] = parentID
	}

	resp, err := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpInstanceListChildren, payload)
	if err != nil {
		return action.Result{}, roblox.ToFault(err, req.CorrelationID)
	}
	return action.Result{
		Output: resp.Payload,
	}, nil
}
