package usecase

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/roushou/gox/internal/domain/fault"
	"github.com/roushou/gox/internal/domain/runlog"
)

type errorAwareRunlogStore struct {
	listErr error
	getErr  error
}

func (s errorAwareRunlogStore) Upsert(_ context.Context, _ runlog.Record) error {
	return nil
}

func (s errorAwareRunlogStore) Get(ctx context.Context, runID string) (runlog.Record, bool) {
	record, ok, _ := s.GetWithError(ctx, runID)
	return record, ok
}

func (s errorAwareRunlogStore) List(ctx context.Context) []runlog.Record {
	records, _ := s.ListWithError(ctx)
	return records
}

func (s errorAwareRunlogStore) GetWithError(_ context.Context, _ string) (runlog.Record, bool, error) {
	if s.getErr != nil {
		return runlog.Record{}, false, s.getErr
	}
	return runlog.Record{}, false, nil
}

func (s errorAwareRunlogStore) ListWithError(_ context.Context) ([]runlog.Record, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return nil, nil
}

func (s errorAwareRunlogStore) DeleteOlderThan(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

func TestWorkflowRuntimeListMapsRunlogListError(t *testing.T) {
	errStore := errorAwareRunlogStore{
		listErr: runlog.NewStoreError(
			runlog.StoreErrorUnavailable,
			"runlog.list",
			"db unavailable",
			nil,
		),
	}
	runtime := NewWorkflowRuntime(
		slog.Default(),
		nil,
		nil,
		errStore,
		WorkflowRuntimeConfig{},
	)

	_, _, err := runtime.List(WorkflowListOptions{})
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
}

func TestWorkflowRuntimeStatusMapsRunlogGetError(t *testing.T) {
	errStore := errorAwareRunlogStore{
		getErr: runlog.NewStoreError(
			runlog.StoreErrorCorruptRow,
			"runlog.get",
			"bad timestamp",
			nil,
		),
	}
	runtime := NewWorkflowRuntime(
		slog.Default(),
		nil,
		nil,
		errStore,
		WorkflowRuntimeConfig{},
	)

	_, err := runtime.Status("wf-1")
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
	if got := f.Details["storage_code"]; got != runlog.StoreErrorCorruptRow {
		t.Fatalf("unexpected storage_code detail: %#v", f.Details)
	}
}
