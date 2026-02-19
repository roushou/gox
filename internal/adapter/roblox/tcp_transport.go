package roblox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

type TCPTransport struct {
	network     string
	address     string
	dialTimeout time.Duration
	authToken   string
	requireAuth bool
	clientName  string

	mu            sync.Mutex
	conn          net.Conn
	authenticated bool
	helloDone     bool
}

type TCPTransportOptions struct {
	AuthToken   string
	RequireAuth bool
	ClientName  string
}

func NewTCPTransport(network, address string, dialTimeout time.Duration, opts ...TCPTransportOptions) *TCPTransport {
	if network == "" {
		network = "tcp"
	}
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	options := TCPTransportOptions{}
	if len(opts) > 0 {
		options = opts[0]
	}
	if options.ClientName == "" {
		options.ClientName = "gox"
	}
	return &TCPTransport{
		network:     network,
		address:     address,
		dialTimeout: dialTimeout,
		authToken:   options.AuthToken,
		requireAuth: options.RequireAuth,
		clientName:  options.ClientName,
	}
}

func (t *TCPTransport) Call(ctx context.Context, req Request) (Response, error) {
	if err := req.Validate(); err != nil {
		return Response{}, ProtocolError{Message: err.Error()}
	}
	var lastErr error
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := t.callOnce(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if isHelloRequiredError(err) {
			t.mu.Lock()
			t.helloDone = false
			t.mu.Unlock()
			continue
		}
		if !isRetryableTransportError(err) {
			return Response{}, err
		}
	}
	return Response{}, lastErr
}

func (t *TCPTransport) Ping(ctx context.Context) error {
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

func (t *TCPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closeLocked()
}

func (t *TCPTransport) ensureConn(ctx context.Context) (net.Conn, error) {
	if t.conn != nil {
		return t.conn, nil
	}
	dialer := net.Dialer{Timeout: t.dialTimeout}
	conn, err := dialer.DialContext(ctx, t.network, t.address)
	if err != nil {
		return nil, fmt.Errorf("dial bridge transport: %w", err)
	}
	t.conn = conn
	t.authenticated = false
	t.helloDone = false
	return conn, nil
}

func (t *TCPTransport) callOnce(ctx context.Context, req Request) (Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	conn, err := t.ensureConn(ctx)
	if err != nil {
		return Response{}, err
	}
	if err := setDeadlineFromContext(conn, ctx); err != nil {
		_ = t.closeLocked()
		return Response{}, err
	}
	if req.Operation != OpAuth {
		if err := t.ensureAuthenticatedLocked(ctx, conn); err != nil {
			_ = t.closeLocked()
			return Response{}, err
		}
		if req.Operation != OpHello {
			if err := t.ensureHelloLocked(conn); err != nil {
				_ = t.closeLocked()
				return Response{}, err
			}
		}
	}

	return t.sendRequestLocked(conn, req)
}

func (t *TCPTransport) ensureAuthenticatedLocked(ctx context.Context, conn net.Conn) error {
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
	resp, err := t.sendRequestLocked(conn, authReq)
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

func (t *TCPTransport) sendRequestLocked(conn net.Conn, req Request) (Response, error) {
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = req.RequestID
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		_ = t.closeLocked()
		return Response{}, fmt.Errorf("write request: %w", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		_ = t.closeLocked()
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		_ = t.closeLocked()
		return Response{}, ProtocolError{Message: "failed to decode bridge response"}
	}
	if err := resp.Validate(); err != nil {
		return Response{}, ProtocolError{Message: err.Error()}
	}
	return resp, nil
}

func (t *TCPTransport) closeLocked() error {
	if t.conn == nil {
		return nil
	}
	err := t.conn.Close()
	t.conn = nil
	t.authenticated = false
	return err
}

func (t *TCPTransport) ensureHelloLocked(conn net.Conn) error {
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
	resp, err := t.sendRequestLocked(conn, helloReq)
	if err != nil {
		// Legacy bridges may not implement bridge.hello.
		var bridgeErr BridgeCallError
		if errors.As(err, &bridgeErr) {
			if bridgeErr.Code == "NOT_FOUND" || bridgeErr.Code == "UNIMPLEMENTED" {
				t.helloDone = true
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
				return nil
			}
		}
		return err
	}
	if payloadVersion, _ := resp.Payload["protocolVersion"].(string); strings.TrimSpace(payloadVersion) == "" {
		// Legacy bridge success payload without explicit version.
		t.helloDone = true
		return nil
	}
	t.helloDone = true
	return nil
}

func setDeadlineFromContext(conn net.Conn, ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		// Zero value clears deadline.
		return conn.SetDeadline(time.Time{})
	}
	if err := conn.SetDeadline(deadline); err != nil {
		if errors.Is(err, net.ErrClosed) {
			return err
		}
		return fmt.Errorf("set deadline: %w", err)
	}
	return nil
}

func isRetryableTransportError(err error) bool {
	var protocolErr ProtocolError
	if errors.As(err, &protocolErr) {
		return false
	}
	var bridgeErr BridgeCallError
	if errors.As(err, &bridgeErr) {
		if !bridgeErr.Retryable || bridgeErr.Code == "ACCESS_DENIED" {
			return false
		}
	}
	return true
}

func isHelloRequiredError(err error) bool {
	var bridgeErr BridgeCallError
	if !errors.As(err, &bridgeErr) {
		return false
	}
	if bridgeErr.Code != "ACCESS_DENIED" {
		return false
	}
	return strings.Contains(strings.ToLower(bridgeErr.Message), "bridge.hello required")
}
