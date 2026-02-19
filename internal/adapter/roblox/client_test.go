package roblox

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

type fakeTransport struct {
	callFn  func(context.Context, Request) (Response, error)
	pingFn  func(context.Context) error
	closeFn func() error
}

func (f fakeTransport) Call(ctx context.Context, req Request) (Response, error) {
	if f.callFn != nil {
		return f.callFn(ctx, req)
	}
	return Response{}, nil
}

func (f fakeTransport) Ping(ctx context.Context) error {
	if f.pingFn != nil {
		return f.pingFn(ctx)
	}
	return nil
}

func (f fakeTransport) Close() error {
	if f.closeFn != nil {
		return f.closeFn()
	}
	return nil
}

func TestClientExecuteSuccess(t *testing.T) {
	client := NewClient(fakeTransport{
		callFn: func(_ context.Context, req Request) (Response, error) {
			return Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload:       map[string]any{"ok": true},
				Timestamp:     time.Now().UTC(),
			}, nil
		},
	}, slog.Default(), time.Second)

	resp, err := client.Execute(context.Background(), "corr-1", OpPing, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Success {
		t.Fatalf("unexpected response: %#v", resp)
	}
	health := client.Health()
	if !health.Connected {
		t.Fatalf("expected connected health: %#v", health)
	}
}

func TestClientExecuteBridgeError(t *testing.T) {
	client := NewClient(fakeTransport{
		callFn: func(_ context.Context, req Request) (Response, error) {
			return Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       false,
				Error: &BridgeError{
					Code:    "VALIDATION_ERROR",
					Message: "bad payload",
				},
				Timestamp: time.Now().UTC(),
			}, nil
		},
	}, slog.Default(), time.Second)

	_, err := client.Execute(context.Background(), "corr-1", OpScriptCreate, map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
	var bridgeErr BridgeCallError
	if !errors.As(err, &bridgeErr) {
		t.Fatalf("expected BridgeCallError, got %T", err)
	}
	if bridgeErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("unexpected bridge code %q", bridgeErr.Code)
	}
	health := client.Health()
	if health.ConsecutiveFailures != 1 {
		t.Fatalf("expected failure count 1, got %d", health.ConsecutiveFailures)
	}
}

func TestClientExecuteProtocolMismatch(t *testing.T) {
	client := NewClient(fakeTransport{
		callFn: func(_ context.Context, req Request) (Response, error) {
			return Response{
				RequestID:     "wrong-id",
				CorrelationID: req.CorrelationID,
				Success:       true,
				Timestamp:     time.Now().UTC(),
			}, nil
		},
	}, slog.Default(), time.Second)

	_, err := client.Execute(context.Background(), "corr-1", OpPing, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var protocolErr ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("expected ProtocolError, got %T", err)
	}
}

func TestClientPingHealth(t *testing.T) {
	shouldFail := true
	client := NewClient(fakeTransport{
		pingFn: func(_ context.Context) error {
			if shouldFail {
				return errors.New("temporary down")
			}
			return nil
		},
	}, slog.Default(), time.Second)

	if err := client.Ping(context.Background()); err == nil {
		t.Fatal("expected ping failure")
	}
	health := client.Health()
	if health.ConsecutiveFailures != 1 || health.Connected {
		t.Fatalf("unexpected health after failure: %#v", health)
	}

	shouldFail = false
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("expected ping success, got %v", err)
	}
	health = client.Health()
	if health.ConsecutiveFailures != 0 || !health.Connected {
		t.Fatalf("unexpected health after success: %#v", health)
	}
}
