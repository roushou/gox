package actions

import (
	"context"
	"os"
	"path/filepath"

	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
)

type WorkspaceStatsAction struct {
	root string
}

func NewWorkspaceStatsAction(root string) WorkspaceStatsAction {
	return WorkspaceStatsAction{root: root}
}

func (a WorkspaceStatsAction) Spec() action.Spec {
	return action.Spec{
		ID:              "project.workspace_stats",
		Name:            "Workspace Stats",
		Version:         "v1",
		Description:     "Counts files and directories under the workspace root",
		SideEffectLevel: action.SideEffectRead,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"files":       map[string]any{"type": "integer"},
				"directories": map[string]any{"type": "integer"},
				"root":        map[string]any{"type": "string"},
			},
		},
	}
}

func (a WorkspaceStatsAction) Handle(_ context.Context, _ action.Request) (action.Result, error) {
	var files int64
	var dirs int64

	err := filepath.WalkDir(a.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == a.root {
			return nil
		}
		if d.IsDir() {
			dirs++
			return nil
		}
		files++
		return nil
	})
	if err != nil {
		return action.Result{}, fault.Internal("failed to walk workspace")
	}

	return action.Result{
		Output: map[string]any{
			"root":        a.root,
			"files":       files,
			"directories": dirs,
		},
	}, nil
}
