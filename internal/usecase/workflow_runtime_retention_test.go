package usecase

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/runlog"
	"github.com/roushou/gox/internal/domain/workflow"
)

type retentionCatalog struct {
	handler action.Handler
}

func (c retentionCatalog) Get(id string) (action.Handler, bool) {
	if c.handler == nil {
		return nil, false
	}
	if c.handler.Spec().ID != id {
		return nil, false
	}
	return c.handler, true
}

type retentionHandler struct {
	spec action.Spec
}

func (h retentionHandler) Spec() action.Spec {
	return h.spec
}

func (h retentionHandler) Handle(_ context.Context, _ action.Request) (action.Result, error) {
	return action.Result{Output: map[string]any{"ok": true}}, nil
}

type retentionExecutor struct{}

func (retentionExecutor) Execute(_ context.Context, cmd ExecuteActionCommand) (ExecuteActionResult, error) {
	return ExecuteActionResult{
		RunID: "action-" + cmd.ActionID,
		Result: action.Result{
			Output: map[string]any{"ok": true},
		},
	}, nil
}

func TestWorkflowRuntimeRetentionMaxRuns(t *testing.T) {
	handler := retentionHandler{
		spec: action.Spec{
			ID:              "core.ping",
			Name:            "Ping",
			Version:         "v1",
			SideEffectLevel: action.SideEffectRead,
			InputSchema:     map[string]any{"type": "object"},
		},
	}
	runtime := NewWorkflowRuntime(
		slog.Default(),
		retentionCatalog{handler: handler},
		retentionExecutor{},
		runlog.NewInMemoryStore(),
		WorkflowRuntimeConfig{
			MaxConcurrentRuns: 4,
			RetentionMaxRuns:  2,
		},
	)
	engine := runtime.(*workflowRuntimeEngine)

	for i := 0; i < 3; i++ {
		snapshot, err := engine.Start(workflow.Definition{
			ID: "wf-retention",
			Steps: []workflow.Step{
				{ID: "step-1", ActionID: "core.ping", Input: map[string]any{}},
			},
		}, WorkflowRunOptions{})
		if err != nil {
			t.Fatalf("start workflow: %v", err)
		}
		waitWorkflowStatus(t, engine, snapshot.RunID, "succeeded")
	}

	engine.prune()

	engine.mu.RLock()
	defer engine.mu.RUnlock()
	if len(engine.runs) > 2 {
		t.Fatalf("expected <=2 retained runs, got %d", len(engine.runs))
	}
}

func TestWorkflowRuntimeRetentionTTL(t *testing.T) {
	handler := retentionHandler{
		spec: action.Spec{
			ID:              "core.ping",
			Name:            "Ping",
			Version:         "v1",
			SideEffectLevel: action.SideEffectRead,
			InputSchema:     map[string]any{"type": "object"},
		},
	}
	runtime := NewWorkflowRuntime(
		slog.Default(),
		retentionCatalog{handler: handler},
		retentionExecutor{},
		runlog.NewInMemoryStore(),
		WorkflowRuntimeConfig{
			MaxConcurrentRuns: 4,
			RetentionTTL:      50 * time.Millisecond,
		},
	)
	engine := runtime.(*workflowRuntimeEngine)

	snapshot, err := engine.Start(workflow.Definition{
		ID: "wf-ttl",
		Steps: []workflow.Step{
			{ID: "step-1", ActionID: "core.ping", Input: map[string]any{}},
		},
	}, WorkflowRunOptions{})
	if err != nil {
		t.Fatalf("start workflow: %v", err)
	}
	waitWorkflowStatus(t, engine, snapshot.RunID, "succeeded")

	time.Sleep(80 * time.Millisecond)
	engine.prune()

	engine.mu.RLock()
	defer engine.mu.RUnlock()
	if len(engine.runs) != 0 {
		t.Fatalf("expected runs map to be pruned, got %d", len(engine.runs))
	}
}

func waitWorkflowStatus(t *testing.T, runtime WorkflowRuntime, runID string, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := runtime.Status(runID)
		if err == nil && snapshot.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status %q", want)
}
