package runlog

import (
	"errors"
	"testing"
)

func TestStoreErrorHelpers(t *testing.T) {
	cause := errors.New("database busy")
	err := NewStoreError(StoreErrorUnavailable, "runlog.upsert", "write failed", cause)

	if !IsStoreErrorCode(err, StoreErrorUnavailable) {
		t.Fatalf("expected unavailable store error, got %v", err)
	}
	out, ok := AsStoreError(err)
	if !ok {
		t.Fatalf("expected AsStoreError to match")
	}
	if out.Op != "runlog.upsert" {
		t.Fatalf("unexpected op %q", out.Op)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("expected wrapped cause")
	}
}
