package usecase

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
	"github.com/roushou/gox/internal/domain/policy"
	"github.com/roushou/gox/internal/domain/runlog"
)

type fakePolicy struct {
	decision policy.Decision
}

func (f fakePolicy) Evaluate(_ context.Context, _ policy.DecisionInput) policy.Decision {
	return f.decision
}

type fakeHandler struct {
	spec   action.Spec
	result action.Result
	err    error
}

func (h fakeHandler) Spec() action.Spec {
	return h.spec
}

func (h fakeHandler) Handle(_ context.Context, _ action.Request) (action.Result, error) {
	return h.result, h.err
}

type fakeValidatedHandler struct {
	fakeHandler
	validateErr error
}

func (h fakeValidatedHandler) Validate(_ context.Context, _ action.Request) error {
	return h.validateErr
}

type fakeNoDryRunHandler struct {
	fakeHandler
}

func (h fakeNoDryRunHandler) SupportsDryRun() bool {
	return false
}

type countingHandler struct {
	spec   action.Spec
	called int
}

type failingRunlogStore struct {
	upsertErr error
}

func (s failingRunlogStore) Upsert(_ context.Context, _ runlog.Record) error {
	return s.upsertErr
}

func (s failingRunlogStore) Get(_ context.Context, _ string) (runlog.Record, bool) {
	return runlog.Record{}, false
}

func (s failingRunlogStore) List(_ context.Context) []runlog.Record {
	return nil
}

func (s failingRunlogStore) DeleteOlderThan(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

func (h *countingHandler) Spec() action.Spec {
	return h.spec
}

func (h *countingHandler) Handle(_ context.Context, _ action.Request) (action.Result, error) {
	h.called++
	return action.Result{Output: map[string]any{"ok": true}}, nil
}

func TestExecuteActionSuccess(t *testing.T) {
	registry := action.NewInMemoryRegistry()
	err := registry.Register(fakeHandler{
		spec: action.Spec{
			ID:              "core.test",
			Name:            "Test",
			Version:         "v1",
			SideEffectLevel: action.SideEffectNone,
		},
		result: action.Result{
			Output: map[string]any{"ok": true},
			Diff:   &action.Diff{Summary: "no changes"},
		},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	runlogs := runlog.NewInMemoryStore()
	service := NewExecuteActionService(
		registry,
		fakePolicy{decision: policy.Decision{Allowed: true}},
		runlogs,
		slog.Default(),
		time.Second,
		testIDGen("corr-1", "run-1"),
	)

	out, execErr := service.Execute(context.Background(), ExecuteActionCommand{
		ActionID: "core.test",
		Actor:    "tester",
	})
	if execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
	if out.RunID != "run-1" {
		t.Fatalf("unexpected run id %q", out.RunID)
	}
	if ok, _ := out.Result.Output["ok"].(bool); !ok {
		t.Fatalf("unexpected output: %#v", out.Result.Output)
	}

	record, found := runlogs.Get(context.Background(), "run-1")
	if !found {
		t.Fatal("expected run log record")
	}
	if record.Status != runlog.StatusSucceeded {
		t.Fatalf("expected succeeded status, got %q", record.Status)
	}
}

func TestExecuteActionNotFound(t *testing.T) {
	service := NewExecuteActionService(
		action.NewInMemoryRegistry(),
		fakePolicy{decision: policy.Decision{Allowed: true}},
		runlog.NewInMemoryStore(),
		slog.Default(),
		time.Second,
		testIDGen("corr-2"),
	)

	_, err := service.Execute(context.Background(), ExecuteActionCommand{
		ActionID: "missing.action",
		Actor:    "tester",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	f, ok := fault.As(err)
	if !ok {
		t.Fatalf("expected fault, got %T", err)
	}
	if f.Code != fault.CodeNotFound {
		t.Fatalf("expected not found, got %q", f.Code)
	}
	if f.CorrelationID != "corr-2" {
		t.Fatalf("expected correlation id corr-2, got %q", f.CorrelationID)
	}
}

func TestExecuteActionDenied(t *testing.T) {
	registry := action.NewInMemoryRegistry()
	_ = registry.Register(fakeHandler{
		spec: action.Spec{
			ID:              "core.blocked",
			Name:            "Blocked",
			Version:         "v1",
			SideEffectLevel: action.SideEffectWrite,
		},
	})
	service := NewExecuteActionService(
		registry,
		fakePolicy{decision: policy.Decision{Allowed: false, Reason: "blocked by policy"}},
		runlog.NewInMemoryStore(),
		slog.Default(),
		time.Second,
		testIDGen("corr-3"),
	)

	_, err := service.Execute(context.Background(), ExecuteActionCommand{
		ActionID: "core.blocked",
		Actor:    "tester",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	f, _ := fault.As(err)
	if f.Code != fault.CodeDenied {
		t.Fatalf("expected denied, got %q", f.Code)
	}
}

func TestExecuteActionRequiresApprovalDeniedWithoutApproval(t *testing.T) {
	registry := action.NewInMemoryRegistry()
	_ = registry.Register(fakeHandler{
		spec: action.Spec{
			ID:              "core.approval_required",
			Name:            "Approval Required",
			Version:         "v1",
			SideEffectLevel: action.SideEffectWrite,
		},
	})
	service := NewExecuteActionService(
		registry,
		fakePolicy{decision: policy.Decision{
			Allowed:          true,
			RequiresApproval: true,
			Reason:           "manual approval required",
		}},
		runlog.NewInMemoryStore(),
		slog.Default(),
		time.Second,
		testIDGen("corr-approval-denied"),
	)

	_, err := service.Execute(context.Background(), ExecuteActionCommand{
		ActionID: "core.approval_required",
		Actor:    "tester",
		Approved: false,
	})
	if err == nil {
		t.Fatal("expected approval denied error")
	}
	f, ok := fault.As(err)
	if !ok {
		t.Fatalf("expected fault, got %T", err)
	}
	if f.Code != fault.CodeDenied {
		t.Fatalf("expected denied, got %q", f.Code)
	}
	if f.Message != "manual approval required" {
		t.Fatalf("unexpected deny message: %#v", f)
	}
}

func TestExecuteActionRequiresApprovalPassesWhenApproved(t *testing.T) {
	registry := action.NewInMemoryRegistry()
	_ = registry.Register(fakeHandler{
		spec: action.Spec{
			ID:              "core.approval_required",
			Name:            "Approval Required",
			Version:         "v1",
			SideEffectLevel: action.SideEffectWrite,
		},
		result: action.Result{
			Output: map[string]any{"ok": true},
		},
	})
	service := NewExecuteActionService(
		registry,
		fakePolicy{decision: policy.Decision{
			Allowed:          true,
			RequiresApproval: true,
			Reason:           "manual approval required",
		}},
		runlog.NewInMemoryStore(),
		slog.Default(),
		time.Second,
		testIDGen("corr-approval-allowed", "run-approval-allowed"),
	)

	out, err := service.Execute(context.Background(), ExecuteActionCommand{
		ActionID: "core.approval_required",
		Actor:    "tester",
		Approved: true,
	})
	if err != nil {
		t.Fatalf("expected success with approval, got %v", err)
	}
	if out.RunID != "run-approval-allowed" {
		t.Fatalf("unexpected run id: %#v", out)
	}
}

func TestExecuteActionWrapsUnknownHandlerError(t *testing.T) {
	registry := action.NewInMemoryRegistry()
	_ = registry.Register(fakeHandler{
		spec: action.Spec{
			ID:              "core.fail",
			Name:            "Fail",
			Version:         "v1",
			SideEffectLevel: action.SideEffectRead,
		},
		err: errors.New("boom"),
	})
	service := NewExecuteActionService(
		registry,
		fakePolicy{decision: policy.Decision{Allowed: true}},
		runlog.NewInMemoryStore(),
		slog.Default(),
		time.Second,
		testIDGen("corr-4", "run-4"),
	)

	_, err := service.Execute(context.Background(), ExecuteActionCommand{
		ActionID: "core.fail",
		Actor:    "tester",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	f, _ := fault.As(err)
	if f.Code != fault.CodeInternal {
		t.Fatalf("expected internal code, got %q", f.Code)
	}

	record, ok := service.runlogs.Get(context.Background(), "run-4")
	if !ok {
		t.Fatal("expected runlog")
	}
	if record.Status != runlog.StatusFailed {
		t.Fatalf("expected failed status, got %q", record.Status)
	}
}

func TestExecuteActionRunsPreconditionValidation(t *testing.T) {
	registry := action.NewInMemoryRegistry()
	_ = registry.Register(fakeValidatedHandler{
		fakeHandler: fakeHandler{
			spec: action.Spec{
				ID:              "core.validate",
				Name:            "Validate",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
			},
		},
		validateErr: fault.Validation("invalid input"),
	})
	service := NewExecuteActionService(
		registry,
		fakePolicy{decision: policy.Decision{Allowed: true}},
		runlog.NewInMemoryStore(),
		slog.Default(),
		time.Second,
		testIDGen("corr-5"),
	)

	_, err := service.Execute(context.Background(), ExecuteActionCommand{
		ActionID: "core.validate",
		Actor:    "tester",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	f, ok := fault.As(err)
	if !ok {
		t.Fatalf("expected fault, got %T", err)
	}
	if f.Code != fault.CodeValidation {
		t.Fatalf("expected validation code, got %q", f.Code)
	}
}

func TestExecuteActionPreconditionValidationErrorIncludesPathRule(t *testing.T) {
	registry := action.NewInMemoryRegistry()
	_ = registry.Register(fakeValidatedHandler{
		fakeHandler: fakeHandler{
			spec: action.Spec{
				ID:              "core.validate_structured",
				Name:            "Validate Structured",
				Version:         "v1",
				SideEffectLevel: action.SideEffectRead,
			},
		},
		validateErr: action.ValidationError{
			Path:    "input.scriptType",
			Rule:    "enum",
			Message: "must be one of the allowed values",
		},
	})
	service := NewExecuteActionService(
		registry,
		fakePolicy{decision: policy.Decision{Allowed: true}},
		runlog.NewInMemoryStore(),
		slog.Default(),
		time.Second,
		testIDGen("corr-structured"),
	)

	_, err := service.Execute(context.Background(), ExecuteActionCommand{
		ActionID: "core.validate_structured",
		Actor:    "tester",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	f, ok := fault.As(err)
	if !ok {
		t.Fatalf("expected fault, got %T", err)
	}
	if f.Code != fault.CodeValidation {
		t.Fatalf("expected validation code, got %q", f.Code)
	}
	if got := f.Details["validation_path"]; got != "input.scriptType" {
		t.Fatalf("unexpected validation_path detail: %#v", f.Details)
	}
	if got := f.Details["validation_rule"]; got != "enum" {
		t.Fatalf("unexpected validation_rule detail: %#v", f.Details)
	}
}

func TestExecuteActionRejectsUnsupportedDryRun(t *testing.T) {
	registry := action.NewInMemoryRegistry()
	_ = registry.Register(fakeNoDryRunHandler{
		fakeHandler: fakeHandler{
			spec: action.Spec{
				ID:              "core.no_dry_run",
				Name:            "No Dry Run",
				Version:         "v1",
				SideEffectLevel: action.SideEffectWrite,
			},
			result: action.Result{Output: map[string]any{"ok": true}},
		},
	})
	service := NewExecuteActionService(
		registry,
		fakePolicy{decision: policy.Decision{Allowed: true}},
		runlog.NewInMemoryStore(),
		slog.Default(),
		time.Second,
		testIDGen("corr-6"),
	)

	_, err := service.Execute(context.Background(), ExecuteActionCommand{
		ActionID: "core.no_dry_run",
		Actor:    "tester",
		DryRun:   true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	f, ok := fault.As(err)
	if !ok {
		t.Fatalf("expected fault, got %T", err)
	}
	if f.Code != fault.CodeValidation {
		t.Fatalf("expected validation code, got %q", f.Code)
	}
}

func TestExecuteActionRejectsInvalidSchemaInputBeforeHandle(t *testing.T) {
	registry := action.NewInMemoryRegistry()
	handler := &countingHandler{
		spec: action.Spec{
			ID:              "core.schema_guard",
			Name:            "Schema Guard",
			Version:         "v1",
			SideEffectLevel: action.SideEffectRead,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required":             []string{"name"},
				"additionalProperties": false,
			},
		},
	}
	_ = registry.Register(handler)

	service := NewExecuteActionService(
		registry,
		fakePolicy{decision: policy.Decision{Allowed: true}},
		runlog.NewInMemoryStore(),
		slog.Default(),
		time.Second,
		testIDGen("corr-7"),
	)

	_, err := service.Execute(context.Background(), ExecuteActionCommand{
		ActionID: "core.schema_guard",
		Actor:    "tester",
		Input: map[string]any{
			"name": 123,
		},
	})
	if err == nil {
		t.Fatal("expected schema validation error")
	}
	f, ok := fault.As(err)
	if !ok {
		t.Fatalf("expected fault, got %T", err)
	}
	if f.Code != fault.CodeValidation {
		t.Fatalf("expected validation code, got %q", f.Code)
	}
	if got := f.Details["validation_path"]; got != "input.name" {
		t.Fatalf("unexpected validation_path detail: %#v", f.Details)
	}
	if got := f.Details["validation_rule"]; got != "type" {
		t.Fatalf("unexpected validation_rule detail: %#v", f.Details)
	}
	if handler.called != 0 {
		t.Fatalf("expected handler not called, called=%d", handler.called)
	}
}

func TestExecuteActionMapsUnavailableRunlogStoreError(t *testing.T) {
	registry := action.NewInMemoryRegistry()
	_ = registry.Register(fakeHandler{
		spec: action.Spec{
			ID:              "core.unavailable_runlog",
			Name:            "Unavailable Runlog",
			Version:         "v1",
			SideEffectLevel: action.SideEffectRead,
		},
		result: action.Result{Output: map[string]any{"ok": true}},
	})
	storeErr := runlog.NewStoreError(
		runlog.StoreErrorUnavailable,
		"runlog.upsert",
		"database locked",
		errors.New("database is busy"),
	)
	service := NewExecuteActionService(
		registry,
		fakePolicy{decision: policy.Decision{Allowed: true}},
		failingRunlogStore{upsertErr: storeErr},
		slog.Default(),
		time.Second,
		testIDGen("corr-8", "run-8"),
	)

	_, err := service.Execute(context.Background(), ExecuteActionCommand{
		ActionID: "core.unavailable_runlog",
		Actor:    "tester",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	f, ok := fault.As(err)
	if !ok {
		t.Fatalf("expected fault, got %T", err)
	}
	if f.Code != fault.CodeInternal {
		t.Fatalf("expected internal code, got %q", f.Code)
	}
	if f.Category != fault.CategoryPlatform {
		t.Fatalf("expected platform category, got %q", f.Category)
	}
	if !f.Retryable {
		t.Fatalf("expected retryable fault, got %#v", f)
	}
	if got := f.Details["storage_code"]; got != runlog.StoreErrorUnavailable {
		t.Fatalf("unexpected storage_code detail: %#v", f.Details)
	}
}

func TestActionObservabilityFieldsScriptExecute(t *testing.T) {
	fields := actionObservabilityFields("roblox.script_execute", map[string]any{
		"scriptPath":   "ServerScriptService.BuildOlympus",
		"scriptId":     "inst-123",
		"functionName": "run",
	})
	want := []any{
		"script_path", "ServerScriptService.BuildOlympus",
		"script_id", "inst-123",
		"function_name", "run",
	}
	if len(fields) != len(want) {
		t.Fatalf("unexpected observability fields: %#v", fields)
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Fatalf("unexpected observability field at %d: got %#v want %#v", i, fields[i], want[i])
		}
	}
}

func testIDGen(ids ...string) IDGenerator {
	i := 0
	return func() string {
		if i >= len(ids) {
			return "id-overflow"
		}
		v := ids[i]
		i++
		return v
	}
}
