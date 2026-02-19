package roblox

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestRelayTransportCallWithAuth(t *testing.T) {
	hub := NewRelayHub("token")
	_, err := hub.OpenSession("token", "plugin")
	if err != nil {
		t.Fatalf("open session: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3; i++ {
			line, err := hub.ReadForPlugin(context.Background(), hub.ActiveSessionID())
			if err != nil {
				return
			}
			var req Request
			_ = json.Unmarshal([]byte(line), &req)
			payload := map[string]any{"ok": true, "op": string(req.Operation)}
			if req.Operation == OpHello {
				payload["protocolVersion"] = bridgeProtocolVersion
				payload["capabilities"] = []any{"typed-values.v2"}
			}
			resp := Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload:       payload,
				Timestamp:     time.Now().UTC(),
			}
			b, _ := json.Marshal(resp)
			_ = hub.WriteFromPlugin(context.Background(), hub.ActiveSessionID(), string(b))
		}
	}()

	transport := NewRelayTransport(hub, TCPTransportOptions{
		AuthToken:   "token",
		RequireAuth: true,
		ClientName:  "gox-test",
	})
	resp, err := transport.Call(context.Background(), Request{
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		Operation:     OpPing,
		Timestamp:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !resp.Success {
		t.Fatalf("unexpected response: %#v", resp)
	}
	<-done
}

func TestRelayTransportAuthFailure(t *testing.T) {
	hub := NewRelayHub("token")
	_, err := hub.OpenSession("token", "plugin")
	if err != nil {
		t.Fatalf("open session: %v", err)
	}

	go func() {
		line, err := hub.ReadForPlugin(context.Background(), hub.ActiveSessionID())
		if err != nil {
			return
		}
		var req Request
		_ = json.Unmarshal([]byte(line), &req)
		resp := Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       false,
			Error: &BridgeError{
				Code:    "ACCESS_DENIED",
				Message: "invalid auth token",
			},
			Timestamp: time.Now().UTC(),
		}
		b, _ := json.Marshal(resp)
		_ = hub.WriteFromPlugin(context.Background(), hub.ActiveSessionID(), string(b))
	}()

	transport := NewRelayTransport(hub, TCPTransportOptions{
		AuthToken:   "wrong",
		RequireAuth: true,
		ClientName:  "gox-test",
	})
	_, err = transport.Call(context.Background(), Request{
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		Operation:     OpPing,
		Timestamp:     time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRelayTransportWaitsForSessionAvailability(t *testing.T) {
	hub := NewRelayHub("token")
	transport := NewRelayTransport(hub, TCPTransportOptions{
		AuthToken:   "token",
		RequireAuth: true,
		ClientName:  "gox-test",
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(1200 * time.Millisecond)

		sessionID, err := hub.OpenSession("token", "plugin")
		if err != nil {
			return
		}
		for i := 0; i < 3; i++ {
			line, err := hub.ReadForPlugin(context.Background(), sessionID)
			if err != nil {
				return
			}
			var req Request
			_ = json.Unmarshal([]byte(line), &req)
			payload := map[string]any{"ok": true, "op": string(req.Operation)}
			if req.Operation == OpHello {
				payload["protocolVersion"] = bridgeProtocolVersion
				payload["capabilities"] = []any{"typed-values.v2"}
			}
			resp := Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload:       payload,
				Timestamp:     time.Now().UTC(),
			}
			b, _ := json.Marshal(resp)
			_ = hub.WriteFromPlugin(context.Background(), sessionID, string(b))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	resp, err := transport.Call(ctx, Request{
		RequestID:     "req-delayed-session",
		CorrelationID: "corr-delayed-session",
		Operation:     OpPing,
		Timestamp:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("call should succeed after session appears: %v", err)
	}
	if !resp.Success {
		t.Fatalf("unexpected response: %#v", resp)
	}
	<-done
}

func TestRelayTransportReauthOnSessionSwitch(t *testing.T) {
	hub := NewRelayHub("token")
	transport := NewRelayTransport(hub, TCPTransportOptions{
		AuthToken:   "token",
		RequireAuth: true,
		ClientName:  "gox-test",
	})

	serveSession := func(sessionID string, requests int) <-chan struct{} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < requests; i++ {
				line, err := hub.ReadForPlugin(context.Background(), sessionID)
				if err != nil {
					return
				}
				var req Request
				_ = json.Unmarshal([]byte(line), &req)
				payload := map[string]any{"ok": true, "op": string(req.Operation)}
				if req.Operation == OpHello {
					payload["protocolVersion"] = bridgeProtocolVersion
					payload["capabilities"] = []any{"typed-values.v2"}
				}
				resp := Response{
					RequestID:     req.RequestID,
					CorrelationID: req.CorrelationID,
					Success:       true,
					Payload:       payload,
					Timestamp:     time.Now().UTC(),
				}
				b, _ := json.Marshal(resp)
				_ = hub.WriteFromPlugin(context.Background(), sessionID, string(b))
			}
		}()
		return done
	}

	session1, err := hub.OpenSession("token", "plugin")
	if err != nil {
		t.Fatalf("open first session: %v", err)
	}
	done1 := serveSession(session1, 3) // auth + hello + ping
	if err := transport.Ping(context.Background()); err != nil {
		t.Fatalf("first ping: %v", err)
	}
	<-done1

	if ok := hub.CloseSession(session1); !ok {
		t.Fatalf("close first session")
	}
	session2, err := hub.OpenSession("token", "plugin")
	if err != nil {
		t.Fatalf("open second session: %v", err)
	}
	done2 := serveSession(session2, 3) // auth + hello + ping
	if err := transport.Ping(context.Background()); err != nil {
		t.Fatalf("second ping after session switch: %v", err)
	}
	<-done2
}
