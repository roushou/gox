package roblox

import (
	"testing"
	"time"
)

func TestRequestValidate(t *testing.T) {
	req := Request{
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		Operation:     OpPing,
		Timestamp:     time.Now().UTC(),
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("validate request: %v", err)
	}
}

func TestResponseValidateAgainstRequest(t *testing.T) {
	req := Request{
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		Operation:     OpPing,
		Timestamp:     time.Now().UTC(),
	}
	resp := Response{
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		Success:       true,
		Timestamp:     time.Now().UTC(),
	}
	if err := resp.ValidateAgainstRequest(req); err != nil {
		t.Fatalf("validate response: %v", err)
	}
}

func TestResponseValidateAgainstRequestMismatch(t *testing.T) {
	req := Request{
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		Operation:     OpPing,
		Timestamp:     time.Now().UTC(),
	}
	resp := Response{
		RequestID:     "req-2",
		CorrelationID: "corr-1",
		Success:       true,
		Timestamp:     time.Now().UTC(),
	}
	if err := resp.ValidateAgainstRequest(req); err == nil {
		t.Fatal("expected mismatch error")
	}
}
