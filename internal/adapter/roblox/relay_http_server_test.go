package roblox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRelayHTTPServerSessionAuth(t *testing.T) {
	hub := NewRelayHub("token")
	server := NewRelayHTTPServer("127.0.0.1:0", hub, "token", nil)
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/bridge/session", strings.NewReader(`{"clientName":"plugin"}`))
	req.Header.Set("X-Gox-Auth", "wrong")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/bridge/session", strings.NewReader(`{"clientName":"plugin"}`))
	req.Header.Set("X-Gox-Auth", "token")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var payload map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["sessionId"] == "" {
		t.Fatalf("missing session id: %#v", payload)
	}
}

func TestRelayHTTPServerReadWrite(t *testing.T) {
	hub := NewRelayHub("token")
	sessionID, err := hub.OpenSession("token", "plugin")
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	server := NewRelayHTTPServer("127.0.0.1:0", hub, "token", nil)
	handler := server.Handler()

	go func() {
		_ = hub.SendToPlugin(context.Background(), "hello-plugin")
	}()

	req := httptest.NewRequest(http.MethodGet, "/v1/bridge/session/"+sessionID+"/read?timeout_ms=1000", nil)
	req.Header.Set("X-Gox-Auth", "token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var readPayload map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&readPayload); err != nil {
		t.Fatalf("decode read payload: %v", err)
	}
	if readPayload["line"] != "hello-plugin" {
		t.Fatalf("unexpected read line: %#v", readPayload)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/bridge/session/"+sessionID+"/write", strings.NewReader(`{"line":"hello-gox"}`))
	req.Header.Set("X-Gox-Auth", "token")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	line, err := hub.ReceiveFromPlugin(ctx)
	if err != nil {
		t.Fatalf("receive from plugin: %v", err)
	}
	if line != "hello-gox" {
		t.Fatalf("unexpected line %q", line)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/bridge/session/"+sessionID+"/close", nil)
	req.Header.Set("X-Gox-Auth", "token")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 close, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/bridge/session/"+sessionID+"/read?timeout_ms=10", nil)
	req.Header.Set("X-Gox-Auth", "token")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after close, got %d", rr.Code)
	}
}
