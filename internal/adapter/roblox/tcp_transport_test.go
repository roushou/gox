package roblox

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestTCPTransportCallSuccess(t *testing.T) {
	addr, stop := startBridgeServer(t, func(req Request) Response {
		return Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       true,
			Payload:       map[string]any{"echo": string(req.Operation)},
			Timestamp:     time.Now().UTC(),
		}
	})
	defer stop()

	transport := NewTCPTransport("tcp", addr, 2*time.Second)
	defer func() { _ = transport.Close() }()

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
		t.Fatalf("expected success response: %#v", resp)
	}
}

func TestTCPTransportPingSuccess(t *testing.T) {
	addr, stop := startBridgeServer(t, func(req Request) Response {
		return Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       true,
			Timestamp:     time.Now().UTC(),
		}
	})
	defer stop()

	transport := NewTCPTransport("tcp", addr, 2*time.Second)
	defer func() { _ = transport.Close() }()

	if err := transport.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestTCPTransportReconnectsAfterServerRestart(t *testing.T) {
	var hits int32
	addr, stop := startBridgeServer(t, func(req Request) Response {
		atomic.AddInt32(&hits, 1)
		return Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       true,
			Timestamp:     time.Now().UTC(),
		}
	})

	transport := NewTCPTransport("tcp", addr, 2*time.Second)
	defer func() { _ = transport.Close() }()

	_, err := transport.Call(context.Background(), Request{
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		Operation:     OpPing,
		Timestamp:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Simulate server restart on the same address.
	stop()
	_, stop = startBridgeServerOnAddr(t, addr, func(req Request) Response {
		atomic.AddInt32(&hits, 1)
		return Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       true,
			Timestamp:     time.Now().UTC(),
		}
	})
	defer stop()

	_, err = transport.Call(context.Background(), Request{
		RequestID:     "req-2",
		CorrelationID: "corr-2",
		Operation:     OpPing,
		Timestamp:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got < 2 {
		t.Fatalf("expected at least 2 hits, got %d", got)
	}
}

func TestTCPTransportAuthHandshakeSuccess(t *testing.T) {
	var authHits int32
	var opHits int32
	addr, stop := startAuthenticatedBridgeServer(t, "token-123", func(req Request) Response {
		atomic.AddInt32(&opHits, 1)
		return Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       true,
			Timestamp:     time.Now().UTC(),
		}
	}, &authHits)
	defer stop()

	transport := NewTCPTransport("tcp", addr, 2*time.Second, TCPTransportOptions{
		AuthToken:   "token-123",
		RequireAuth: true,
		ClientName:  "gox-test",
	})
	defer func() { _ = transport.Close() }()

	_, err := transport.Call(context.Background(), Request{
		RequestID:     "req-auth-1",
		CorrelationID: "corr-auth-1",
		Operation:     OpPing,
		Timestamp:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if got := atomic.LoadInt32(&authHits); got != 1 {
		t.Fatalf("expected one auth hit, got %d", got)
	}
	if got := atomic.LoadInt32(&opHits); got != 1 {
		t.Fatalf("expected one op hit, got %d", got)
	}
}

func TestTCPTransportAuthHandshakeFailure(t *testing.T) {
	addr, stop := startAuthenticatedBridgeServer(t, "good-token", func(req Request) Response {
		return Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       true,
			Timestamp:     time.Now().UTC(),
		}
	}, nil)
	defer stop()

	transport := NewTCPTransport("tcp", addr, 2*time.Second, TCPTransportOptions{
		AuthToken:   "bad-token",
		RequireAuth: true,
		ClientName:  "gox-test",
	})
	defer func() { _ = transport.Close() }()

	_, err := transport.Call(context.Background(), Request{
		RequestID:     "req-auth-2",
		CorrelationID: "corr-auth-2",
		Operation:     OpPing,
		Timestamp:     time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected auth failure")
	}
	bridgeErr, ok := err.(BridgeCallError)
	if !ok {
		t.Fatalf("expected BridgeCallError, got %T", err)
	}
	if bridgeErr.Code != "ACCESS_DENIED" {
		t.Fatalf("unexpected code: %#v", bridgeErr)
	}
}

func startBridgeServer(t *testing.T, handler func(Request) Response) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go serveListener(ln, done, handler)
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func startBridgeServerOnAddr(t *testing.T, addr string, handler func(Request) Response) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen on addr %q: %v", addr, err)
	}
	done := make(chan struct{})
	go serveListener(ln, done, handler)
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func serveListener(ln net.Listener, done chan<- struct{}, handler func(Request) Response) {
	defer close(done)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleConn(conn, handler)
	}
}

func handleConn(conn net.Conn, handler func(Request) Response) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			return
		}
		var resp Response
		if req.Operation == OpHello {
			resp = helloResponseForTest(req)
		} else {
			resp = handler(req)
		}
		b, err := json.Marshal(resp)
		if err != nil {
			return
		}
		if _, err := conn.Write(append(b, '\n')); err != nil {
			return
		}
	}
}

func startAuthenticatedBridgeServer(
	t *testing.T,
	expectedToken string,
	handler func(Request) Response,
	authHits *int32,
) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				reader := bufio.NewReader(conn)
				authenticated := false
				for {
					line, err := reader.ReadBytes('\n')
					if err != nil {
						return
					}
					var req Request
					if err := json.Unmarshal(line, &req); err != nil {
						return
					}
					var resp Response
					if !authenticated {
						if req.Operation != OpAuth {
							resp = Response{
								RequestID:     req.RequestID,
								CorrelationID: req.CorrelationID,
								Success:       false,
								Error: &BridgeError{
									Code:    "ACCESS_DENIED",
									Message: "authentication required",
								},
								Timestamp: time.Now().UTC(),
							}
						} else {
							token := fmt.Sprint(req.Payload["token"])
							if token != expectedToken {
								resp = Response{
									RequestID:     req.RequestID,
									CorrelationID: req.CorrelationID,
									Success:       false,
									Error: &BridgeError{
										Code:    "ACCESS_DENIED",
										Message: "invalid auth token",
									},
									Timestamp: time.Now().UTC(),
								}
							} else {
								authenticated = true
								if authHits != nil {
									atomic.AddInt32(authHits, 1)
								}
								resp = Response{
									RequestID:     req.RequestID,
									CorrelationID: req.CorrelationID,
									Success:       true,
									Payload: map[string]any{
										"authenticated": true,
									},
									Timestamp: time.Now().UTC(),
								}
							}
						}
					} else {
						if req.Operation == OpHello {
							resp = helloResponseForTest(req)
						} else {
							resp = handler(req)
						}
					}
					data, err := json.Marshal(resp)
					if err != nil {
						return
					}
					if _, err := conn.Write(append(data, '\n')); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func helloResponseForTest(req Request) Response {
	return Response{
		RequestID:     req.RequestID,
		CorrelationID: req.CorrelationID,
		Success:       true,
		Payload: map[string]any{
			"protocolVersion": bridgeProtocolVersion,
			"capabilities":    []any{"typed-values.v2"},
		},
		Timestamp: time.Now().UTC(),
	}
}
