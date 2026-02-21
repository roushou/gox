package actions

import (
	"strings"

	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

func addSceneRoot(input map[string]any, payload map[string]any) (string, string, error) {
	rootPath, _ := input["rootPath"].(string)
	rootID, _ := input["rootId"].(string)
	rootPath = strings.TrimSpace(rootPath)
	rootID = strings.TrimSpace(rootID)
	if rootPath == "" && rootID == "" {
		return "", "", fault.Validation("one of rootPath or rootId is required")
	}
	if rootPath != "" {
		payload["rootPath"] = rootPath
	}
	if rootID != "" {
		payload["rootId"] = rootID
	}
	return rootPath, rootID, nil
}

func diffFromPayload(payload map[string]any) *action.Diff {
	if payload == nil {
		return nil
	}
	summary, _ := payload["diffSummary"].(string)
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	return &action.Diff{Summary: summary}
}
