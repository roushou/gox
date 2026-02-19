package actions

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

const (
	defaultInstanceFindMaxResults = 50
	maxInstanceFindMaxResults     = 500
)

type InstanceFindAction struct {
	bridge BridgeInvoker
}

func NewInstanceFindAction(bridge BridgeInvoker) InstanceFindAction {
	return InstanceFindAction{bridge: bridge}
}

func (a InstanceFindAction) Spec() action.Spec {
	return action.Spec{
		ID:              "roblox.instance_find",
		Name:            "Roblox Instance Find",
		Version:         "v1",
		Description:     "Finds descendant Instances under a root path using name/class filters",
		SideEffectLevel: action.SideEffectRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"rootPath": map[string]any{"type": "string", "description": "Dot-separated path to search from, e.g. 'Workspace' or 'ServerScriptService'"},
				"rootId":   map[string]any{"type": "string"},
				"nameContains": map[string]any{
					"type": "string",
				},
				"className":  map[string]any{"type": "string"},
				"limit":      map[string]any{"type": "integer"},
				"offset":     map[string]any{"type": "integer"},
				"maxResults": map[string]any{"type": "integer"},
			},
			"required":             []string{},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}
}

func (a InstanceFindAction) RequiresBridge() bool {
	return true
}

func (a InstanceFindAction) Handle(ctx context.Context, req action.Request) (action.Result, error) {
	rootPath, _ := req.Input["rootPath"].(string)
	rootID, _ := req.Input["rootId"].(string)
	if strings.TrimSpace(rootPath) == "" && strings.TrimSpace(rootID) == "" {
		return action.Result{}, fault.Validation("one of rootPath or rootId is required")
	}

	payload := map[string]any{}
	if strings.TrimSpace(rootPath) != "" {
		payload["rootPath"] = rootPath
	}
	if strings.TrimSpace(rootID) != "" {
		payload["rootId"] = rootID
	}

	nameContains, hasNameContains, err := optionalFilterString(req.Input, "nameContains")
	if err != nil {
		return action.Result{}, fault.Validation("nameContains must be a non-empty string when provided")
	}
	if hasNameContains {
		payload["nameContains"] = nameContains
	}

	className, hasClassName, err := optionalFilterString(req.Input, "className")
	if err != nil {
		return action.Result{}, fault.Validation("className must be a non-empty string when provided")
	}
	if hasClassName {
		payload["className"] = className
	}

	if len(payload) == 1 {
		return action.Result{}, fault.Validation("at least one filter is required: nameContains or className")
	}

	limit, hasLimit, err := parsePositiveInteger(req.Input, "limit")
	if err != nil {
		return action.Result{}, fault.Validation(err.Error())
	}
	legacyLimit, hasLegacyLimit, err := parsePositiveInteger(req.Input, "maxResults")
	if err != nil {
		return action.Result{}, fault.Validation(err.Error())
	}
	if hasLimit && hasLegacyLimit {
		return action.Result{}, fault.Validation("only one of limit or maxResults may be provided")
	}
	if !hasLimit && hasLegacyLimit {
		limit = legacyLimit
		hasLimit = true
	}
	if !hasLimit {
		limit = defaultInstanceFindMaxResults
	}
	if limit > maxInstanceFindMaxResults {
		limit = maxInstanceFindMaxResults
	}

	offset, hasOffset, err := parseNonNegativeInteger(req.Input, "offset")
	if err != nil {
		return action.Result{}, fault.Validation(err.Error())
	}
	if !hasOffset {
		offset = 0
	}

	payload["limit"] = limit
	payload["offset"] = offset

	resp, callErr := a.bridge.Execute(ctx, req.CorrelationID, roblox.OpInstanceFind, payload)
	if callErr != nil {
		return action.Result{}, roblox.ToFault(callErr, req.CorrelationID)
	}
	return action.Result{
		Output: resp.Payload,
	}, nil
}

func optionalFilterString(input map[string]any, key string) (value string, ok bool, err error) {
	raw, ok := input[key]
	if !ok {
		return "", false, nil
	}
	value, ok = raw.(string)
	if !ok {
		return "", false, fmt.Errorf("%s must be a string", key)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false, fmt.Errorf("%s must be non-empty", key)
	}
	return value, true, nil
}

func parseInteger(input map[string]any, key string) (int, bool, error) {
	raw, ok := input[key]
	if !ok {
		return 0, false, nil
	}
	switch v := raw.(type) {
	case int:
		return v, true, nil
	case int32:
		return int(v), true, nil
	case int64:
		return int(v), true, nil
	case float64:
		if math.Trunc(v) != v {
			return 0, false, fmt.Errorf("%s must be an integer", key)
		}
		return int(v), true, nil
	default:
		return 0, false, fmt.Errorf("%s must be an integer", key)
	}
}

func parsePositiveInteger(input map[string]any, key string) (int, bool, error) {
	value, ok, err := parseInteger(input, key)
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, nil
	}
	if value < 1 {
		return 0, false, fmt.Errorf("%s must be >= 1", key)
	}
	return value, true, nil
}

func parseNonNegativeInteger(input map[string]any, key string) (int, bool, error) {
	value, ok, err := parseInteger(input, key)
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, nil
	}
	if value < 0 {
		return 0, false, fmt.Errorf("%s must be >= 0", key)
	}
	return value, true, nil
}
