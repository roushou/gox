package roblox

import (
	"errors"
	"testing"

	"github.com/roushou/gox/internal/domain/fault"
)

func TestToFaultFromBridgeCode(t *testing.T) {
	err := ToFault(BridgeCallError{
		Code:      "ACCESS_DENIED",
		Message:   "blocked",
		Retryable: false,
	}, "corr-1")
	f, ok := fault.As(err)
	if !ok {
		t.Fatalf("expected fault, got %T", err)
	}
	if f.Code != fault.CodeDenied {
		t.Fatalf("unexpected fault code %q", f.Code)
	}
	if f.CorrelationID != "corr-1" {
		t.Fatalf("unexpected correlation id %q", f.CorrelationID)
	}
}

func TestToFaultNotConnected(t *testing.T) {
	err := ToFault(ErrNotConnected, "corr-2")
	f, ok := fault.As(err)
	if !ok {
		t.Fatalf("expected fault, got %T", err)
	}
	if f.Code != fault.CodeInternal {
		t.Fatalf("unexpected fault code %q", f.Code)
	}
}

func TestToFaultFallback(t *testing.T) {
	err := ToFault(errors.New("boom"), "corr-3")
	f, ok := fault.As(err)
	if !ok {
		t.Fatalf("expected fault, got %T", err)
	}
	if f.Code != fault.CodeInternal {
		t.Fatalf("unexpected code %q", f.Code)
	}
}
