package roblox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	bridgeProtocolVersion = "1.1"
	relaySessionPollDelay = 100 * time.Millisecond
)

type RelayTransport struct {
	hub         *RelayHub
	requireAuth bool
	authToken   string
	clientName  string

	mu            sync.Mutex
	sessionID     string
	authenticated bool
	helloDone     bool
	capabilities  map[string]bool
}

func NewRelayTransport(hub *RelayHub, opts TCPTransportOptions) *RelayTransport {
	if opts.ClientName == "" {
		opts.ClientName = "gox"
	}
	return &RelayTransport{
		hub:         hub,
		requireAuth: opts.RequireAuth,
		authToken:   opts.AuthToken,
		clientName:  opts.ClientName,
	}
}

func (t *RelayTransport) Call(ctx context.Context, req Request) (Response, error) {
	if err := req.Validate(); err != nil {
		return Response{}, ProtocolError{Message: err.Error()}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.syncSessionStateLocked()

	if req.Operation != OpAuth {
		if err := t.ensureAuthenticatedLocked(ctx); err != nil {
			return Response{}, err
		}
		if req.Operation != OpHello {
			if err := t.ensureHelloLocked(ctx); err != nil {
				return Response{}, err
			}
		}
	}
	return t.sendRequestLocked(ctx, req)
}

func (t *RelayTransport) Ping(ctx context.Context) error {
	req := Request{
		RequestID:     "bridge-ping",
		CorrelationID: "bridge-ping",
		Operation:     OpPing,
		Timestamp:     time.Now().UTC(),
	}
	resp, err := t.Call(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success {
		if resp.Error != nil {
			return BridgeCallError{
				Code:          resp.Error.Code,
				Message:       resp.Error.Message,
				Retryable:     resp.Error.Retryable,
				CorrelationID: resp.CorrelationID,
				Details:       resp.Error.Details,
			}
		}
		return ProtocolError{Message: "ping returned unsuccessful response without error payload"}
	}
	return nil
}

func (t *RelayTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.resetSessionStateLocked("")
	return nil
}

func (t *RelayTransport) ensureAuthenticatedLocked(ctx context.Context) error {
	if !t.requireAuth {
		return nil
	}
	if t.authenticated {
		return nil
	}
	if t.authToken == "" {
		return ProtocolError{Message: "bridge auth is required but auth token is empty"}
	}
	authReq := Request{
		RequestID:     "bridge-auth",
		CorrelationID: "bridge-auth",
		Operation:     OpAuth,
		Payload: map[string]any{
			"token":      t.authToken,
			"clientName": t.clientName,
		},
		Timestamp: time.Now().UTC(),
	}
	resp, err := t.sendRequestLocked(ctx, authReq)
	if err != nil {
		return err
	}
	if !resp.Success {
		if resp.Error != nil {
			return BridgeCallError{
				Code:          resp.Error.Code,
				Message:       resp.Error.Message,
				Retryable:     resp.Error.Retryable,
				CorrelationID: resp.CorrelationID,
				Details:       resp.Error.Details,
			}
		}
		return ProtocolError{Message: "authentication failed without error payload"}
	}
	t.authenticated = true
	return nil
}

func (t *RelayTransport) ensureHelloLocked(ctx context.Context) error {
	if t.helloDone {
		return nil
	}
	helloReq := Request{
		RequestID:      "bridge-hello",
		CorrelationID:  "bridge-hello",
		Operation:      OpHello,
		IdempotencyKey: "bridge-hello",
		Payload: map[string]any{
			"protocolVersion": bridgeProtocolVersion,
			"clientName":      t.clientName,
		},
		Timestamp: time.Now().UTC(),
	}
	resp, err := t.sendRequestWithRetryLocked(ctx, helloReq)
	if err != nil {
		// Legacy bridge implementations may not support bridge.hello.
		var bridgeErr BridgeCallError
		if errors.As(err, &bridgeErr) {
			if bridgeErr.Code == "NOT_FOUND" || bridgeErr.Code == "UNIMPLEMENTED" {
				t.helloDone = true
				t.capabilities = map[string]bool{}
				return nil
			}
		}
		return err
	}
	if !resp.Success {
		err = bridgeCallErrorFromResponse(resp, "bridge hello failed")
		var bridgeErr BridgeCallError
		if errors.As(err, &bridgeErr) {
			if bridgeErr.Code == "NOT_FOUND" || bridgeErr.Code == "UNIMPLEMENTED" {
				t.helloDone = true
				t.capabilities = map[string]bool{}
				return nil
			}
		}
		return err
	}
	if payloadVersion, _ := resp.Payload["protocolVersion"].(string); strings.TrimSpace(payloadVersion) == "" {
		// Legacy bridges may return a generic success without handshake fields.
		t.helloDone = true
		t.capabilities = map[string]bool{}
		return nil
	}
	caps := map[string]bool{}
	if rawCaps, ok := resp.Payload["capabilities"].([]any); ok {
		for _, raw := range rawCaps {
			name, ok := raw.(string)
			if !ok {
				continue
			}
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			caps[name] = true
		}
	}
	t.capabilities = caps
	t.helloDone = true
	return nil
}

func (t *RelayTransport) sendRequestLocked(ctx context.Context, req Request) (Response, error) {
	return t.sendRequestWithRetryLocked(ctx, req)
}

func (t *RelayTransport) sendRequestWithRetryLocked(ctx context.Context, req Request) (Response, error) {
	if t.hub == nil {
		return Response{}, ErrRelayNoSession
	}
	if err := t.waitForActiveSessionLocked(ctx); err != nil {
		return Response{}, err
	}
	t.syncSessionStateLocked()
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = req.RequestID
	}
	data, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}
	const maxAttempts = 3
	backoff := 100 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := t.hub.SendToPlugin(ctx, string(data)); err != nil {
			if errors.Is(err, ErrRelayNoSession) || errors.Is(err, ErrRelayExpired) {
				t.resetSessionStateLocked("")
				if waitErr := t.waitForActiveSessionLocked(ctx); waitErr != nil {
					lastErr = waitErr
					break
				}
				t.syncSessionStateLocked()
			}
			lastErr = err
		} else {
			line, err := t.hub.ReceiveFromPlugin(ctx)
			if err == nil {
				var resp Response
				if err := json.Unmarshal([]byte(line), &resp); err != nil {
					return Response{}, ProtocolError{Message: "failed to decode bridge response"}
				}
				if err := resp.Validate(); err != nil {
					return Response{}, ProtocolError{Message: err.Error()}
				}
				return resp, nil
			}
			if errors.Is(err, ErrRelayNoSession) || errors.Is(err, ErrRelayExpired) {
				t.resetSessionStateLocked("")
				if waitErr := t.waitForActiveSessionLocked(ctx); waitErr != nil {
					lastErr = waitErr
					break
				}
				t.syncSessionStateLocked()
			}
			lastErr = err
		}
		if attempt >= maxAttempts || !isRelayTransient(lastErr) {
			break
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Response{}, ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
	}
	if lastErr == nil {
		lastErr = ErrRelayNoSession
	}
	return Response{}, lastErr
}

func (t *RelayTransport) syncSessionStateLocked() {
	if t.hub == nil {
		t.resetSessionStateLocked("")
		return
	}
	activeSessionID := t.hub.ActiveSessionID()
	if activeSessionID == t.sessionID {
		return
	}
	t.resetSessionStateLocked(activeSessionID)
}

func (t *RelayTransport) resetSessionStateLocked(sessionID string) {
	t.sessionID = sessionID
	t.authenticated = false
	t.helloDone = false
	t.capabilities = nil
}

func (t *RelayTransport) waitForActiveSessionLocked(ctx context.Context) error {
	if t.hub == nil {
		return ErrRelayNoSession
	}
	if t.hub.ActiveSessionID() != "" {
		return nil
	}
	timer := time.NewTimer(relaySessionPollDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if t.hub.ActiveSessionID() != "" {
				return nil
			}
			timer.Reset(relaySessionPollDelay)
		}
	}
}

func isRelayTransient(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrRelayNoSession) ||
		errors.Is(err, ErrRelayExpired) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled)
}

func bridgeCallErrorFromResponse(resp Response, fallback string) error {
	if resp.Error != nil {
		return BridgeCallError{
			Code:          resp.Error.Code,
			Message:       resp.Error.Message,
			Retryable:     resp.Error.Retryable,
			CorrelationID: resp.CorrelationID,
			Details:       resp.Error.Details,
		}
	}
	return ProtocolError{Message: fallback}
}
