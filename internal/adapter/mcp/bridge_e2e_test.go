package mcp

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/app/actions"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/policy"
	"github.com/roushou/gox/internal/domain/runlog"
	"github.com/roushou/gox/internal/usecase"
)

func TestBridgeE2EScriptCreateCorrelationAndSuccess(t *testing.T) {
	requests := make(chan roblox.Request, 1)
	addr, stop := startBridgeTestServer(t, false, "e2e-token", func(req roblox.Request) roblox.Response {
		requests <- req
		return roblox.Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       true,
			Payload: map[string]any{
				"created":     true,
				"path":        "ServerScriptService.Main",
				"diffSummary": "created Script Main",
			},
			Timestamp: time.Now().UTC(),
		}
	})
	defer stop()

	client, cleanup := newBridgeE2EServer(t, addr, "e2e-token")
	defer cleanup()

	resp := callTool(t, client, "roblox.script_create", map[string]any{
		"parentPath": "ServerScriptService",
		"name":       "Main",
		"scriptType": "Script",
		"source":     "print('hello')",
	}, "corr-e2e-1", false)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}

	select {
	case req := <-requests:
		if req.CorrelationID != "corr-e2e-1" {
			t.Fatalf("unexpected correlation id: %#v", req)
		}
		if req.Operation != roblox.OpScriptCreate {
			t.Fatalf("unexpected operation: %#v", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive bridge request")
	}
}

func TestBridgeE2EErrorMapping(t *testing.T) {
	addr, stop := startBridgeTestServer(t, false, "e2e-token", func(req roblox.Request) roblox.Response {
		return roblox.Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       false,
			Error: &roblox.BridgeError{
				Code:    "ACCESS_DENIED",
				Message: "mutation blocked by policy",
			},
			Timestamp: time.Now().UTC(),
		}
	})
	defer stop()

	client, cleanup := newBridgeE2EServer(t, addr, "e2e-token")
	defer cleanup()

	resp := callTool(t, client, "roblox.instance_set_property", map[string]any{
		"instancePath": "Workspace.Part",
		"propertyName": "BrickColor",
		"value":        "Bright red",
	}, "corr-e2e-2", false)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != errorCodeDenied {
		t.Fatalf("unexpected error code: %d", resp.Error.Code)
	}

	data := decodeErrorData(t, resp.Error)
	if data["code"] != "ACCESS_DENIED" {
		t.Fatalf("unexpected mapped code: %#v", data)
	}
	if data["correlation_id"] != "corr-e2e-2" {
		t.Fatalf("unexpected correlation id in response: %#v", data)
	}
}

func TestBridgeE2EReconnectAcrossCalls(t *testing.T) {
	var hits int32
	addr, stop := startBridgeTestServer(t, true, "e2e-token", func(req roblox.Request) roblox.Response {
		atomic.AddInt32(&hits, 1)
		return roblox.Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       true,
			Payload: map[string]any{
				"ok": true,
			},
			Timestamp: time.Now().UTC(),
		}
	})
	defer stop()

	client, cleanup := newBridgeE2EServer(t, addr, "e2e-token")
	defer cleanup()

	resp1 := callTool(t, client, "roblox.bridge_ping", map[string]any{}, "corr-e2e-3a", false)
	if resp1.Error != nil {
		t.Fatalf("first call failed: %#v", resp1.Error)
	}
	resp2 := callTool(t, client, "roblox.bridge_ping", map[string]any{}, "corr-e2e-3b", false)
	if resp2.Error != nil {
		t.Fatalf("second call failed: %#v", resp2.Error)
	}
	if atomic.LoadInt32(&hits) < 2 {
		t.Fatalf("expected at least 2 bridge hits, got %d", hits)
	}
}

func TestBridgeE2ENewMutationOperations(t *testing.T) {
	requests := make(chan roblox.Request, 4)
	addr, stop := startBridgeTestServer(t, false, "e2e-token", func(req roblox.Request) roblox.Response {
		requests <- req
		return roblox.Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       true,
			Payload: map[string]any{
				"ok":          true,
				"diffSummary": "mutated",
			},
			Timestamp: time.Now().UTC(),
		}
	})
	defer stop()

	client, cleanup := newBridgeE2EServer(t, addr, "e2e-token")
	defer cleanup()

	resp1 := callTool(t, client, "roblox.script_update", map[string]any{
		"scriptPath": "ServerScriptService.Main",
		"source":     "print('x')",
	}, "corr-e2e-op-1", false)
	if resp1.Error != nil {
		t.Fatalf("script_update failed: %#v", resp1.Error)
	}
	resp2 := callTool(t, client, "roblox.instance_delete", map[string]any{
		"instancePath": "Workspace.MyPart",
	}, "corr-e2e-op-2", true)
	if resp2.Error != nil {
		t.Fatalf("instance_delete failed: %#v", resp2.Error)
	}

	var op1, op2 roblox.Operation
	select {
	case req := <-requests:
		op1 = req.Operation
	case <-time.After(2 * time.Second):
		t.Fatal("missing first request")
	}
	select {
	case req := <-requests:
		op2 = req.Operation
	case <-time.After(2 * time.Second):
		t.Fatal("missing second request")
	}

	if op1 != roblox.OpScriptUpdate && op2 != roblox.OpScriptUpdate {
		t.Fatalf("script.update operation not observed, got %q and %q", op1, op2)
	}
	if op1 != roblox.OpInstanceDelete && op2 != roblox.OpInstanceDelete {
		t.Fatalf("instance.delete operation not observed, got %q and %q", op1, op2)
	}
}

func TestBridgeE2EScriptExecuteVariants(t *testing.T) {
	requests := make(chan roblox.Request, 4)
	addr, stop := startBridgeTestServer(t, false, "e2e-token", func(req roblox.Request) roblox.Response {
		requests <- req
		switch req.Operation {
		case roblox.OpScriptExecute:
			functionName, _ := req.Payload["functionName"].(string)
			if functionName != "" {
				return roblox.Response{
					RequestID:     req.RequestID,
					CorrelationID: req.CorrelationID,
					Success:       true,
					Payload: map[string]any{
						"executed":    true,
						"called":      true,
						"path":        req.Payload["scriptPath"],
						"returnValue": map[string]any{"built": true, "zone": "Olympus"},
						"diffSummary": "executed function",
					},
					Timestamp: time.Now().UTC(),
				}
			}
			return roblox.Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload: map[string]any{
					"executed":    true,
					"called":      false,
					"path":        req.Payload["scriptPath"],
					"returnValue": "module-export",
					"diffSummary": "executed module",
				},
				Timestamp: time.Now().UTC(),
			}
		default:
			return roblox.Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload:       map[string]any{"ok": true},
				Timestamp:     time.Now().UTC(),
			}
		}
	})
	defer stop()

	client, cleanup := newBridgeE2EServer(t, addr, "e2e-token")
	defer cleanup()

	moduleResp := callTool(t, client, "roblox.script_execute", map[string]any{
		"scriptPath": "ServerScriptService.BuildOlympus",
	}, "corr-e2e-exec-1", false)
	if moduleResp.Error != nil {
		t.Fatalf("script_execute module variant failed: %#v", moduleResp.Error)
	}
	moduleOut, _ := structuredMap(t, moduleResp.Result)["output"].(map[string]any)
	if moduleOut["called"] != false {
		t.Fatalf("expected called=false for module variant: %#v", moduleOut)
	}
	if moduleOut["returnValue"] != "module-export" {
		t.Fatalf("unexpected module returnValue: %#v", moduleOut)
	}

	functionResp := callTool(t, client, "roblox.script_execute", map[string]any{
		"scriptPath":   "ServerScriptService.BuildOlympus",
		"functionName": "run",
		"args":         []any{"Olympus"},
		"expectReturn": true,
	}, "corr-e2e-exec-2", false)
	if functionResp.Error != nil {
		t.Fatalf("script_execute function variant failed: %#v", functionResp.Error)
	}
	functionOut, _ := structuredMap(t, functionResp.Result)["output"].(map[string]any)
	if functionOut["called"] != true {
		t.Fatalf("expected called=true for function variant: %#v", functionOut)
	}
	returnValue, _ := functionOut["returnValue"].(map[string]any)
	if returnValue["built"] != true {
		t.Fatalf("unexpected function returnValue: %#v", functionOut)
	}

	var seenModule, seenFunction bool
	for i := 0; i < 2; i++ {
		select {
		case req := <-requests:
			if req.Operation != roblox.OpScriptExecute {
				continue
			}
			if _, ok := req.Payload["functionName"]; ok {
				seenFunction = true
				if req.Payload["expectReturn"] != true {
					t.Fatalf("expected expectReturn=true payload, got %#v", req.Payload)
				}
			} else {
				seenModule = true
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for script.execute requests")
		}
	}
	if !seenModule || !seenFunction {
		t.Fatalf("expected module+function execute requests, seenModule=%v seenFunction=%v", seenModule, seenFunction)
	}
}

func TestBridgeE2EReadQueryOperations(t *testing.T) {
	requests := make(chan roblox.Request, 8)
	addr, stop := startBridgeTestServer(t, false, "e2e-token", func(req roblox.Request) roblox.Response {
		requests <- req
		switch req.Operation {
		case roblox.OpScriptGetSource:
			return roblox.Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload: map[string]any{
					"path":      "ServerScriptService.Main",
					"className": "Script",
					"source":    "print('hello')",
				},
				Timestamp: time.Now().UTC(),
			}
		case roblox.OpInstanceGet:
			return roblox.Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload: map[string]any{
					"instance": map[string]any{
						"path":      "Workspace.Baseplate",
						"name":      "Baseplate",
						"className": "Part",
					},
				},
				Timestamp: time.Now().UTC(),
			}
		case roblox.OpInstanceListChildren:
			limit, _ := req.Payload["limit"].(float64)
			offset, _ := req.Payload["offset"].(float64)
			return roblox.Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload: map[string]any{
					"parentPath": req.Payload["parentPath"],
					"limit":      limit,
					"offset":     offset,
					"count":      1,
					"hasMore":    true,
					"nextOffset": offset + limit,
					"children": []any{
						map[string]any{
							"path":      "Workspace.Baseplate",
							"name":      "Baseplate",
							"className": "Part",
						},
					},
				},
				Timestamp: time.Now().UTC(),
			}
		case roblox.OpInstanceFind:
			limit, _ := req.Payload["limit"].(float64)
			offset, _ := req.Payload["offset"].(float64)
			return roblox.Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload: map[string]any{
					"rootPath":   req.Payload["rootPath"],
					"limit":      limit,
					"offset":     offset,
					"count":      1,
					"hasMore":    true,
					"nextOffset": offset + 1,
					"matches": []any{
						map[string]any{
							"path":      "Workspace.SpawnLocation",
							"name":      "SpawnLocation",
							"className": "SpawnLocation",
						},
					},
				},
				Timestamp: time.Now().UTC(),
			}
		default:
			return roblox.Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload:       map[string]any{"ok": true},
				Timestamp:     time.Now().UTC(),
			}
		}
	})
	defer stop()

	client, cleanup := newBridgeE2EServer(t, addr, "e2e-token")
	defer cleanup()

	scriptResp := callTool(t, client, "roblox.script_get_source", map[string]any{
		"scriptPath": "ServerScriptService.Main",
	}, "corr-e2e-read-1", false)
	if scriptResp.Error != nil {
		t.Fatalf("script_get_source failed: %#v", scriptResp.Error)
	}
	scriptOut, _ := structuredMap(t, scriptResp.Result)["output"].(map[string]any)
	if scriptOut["path"] != "ServerScriptService.Main" {
		t.Fatalf("unexpected script output: %#v", scriptOut)
	}

	instanceResp := callTool(t, client, "roblox.instance_get", map[string]any{
		"instancePath": "Workspace.Baseplate",
	}, "corr-e2e-read-2", false)
	if instanceResp.Error != nil {
		t.Fatalf("instance_get failed: %#v", instanceResp.Error)
	}
	instanceOut, _ := structuredMap(t, instanceResp.Result)["output"].(map[string]any)
	if _, ok := instanceOut["instance"].(map[string]any); !ok {
		t.Fatalf("unexpected instance output: %#v", instanceOut)
	}

	listResp := callTool(t, client, "roblox.instance_list_children", map[string]any{
		"parentPath": "Workspace",
		"limit":      2,
		"offset":     1,
	}, "corr-e2e-read-3", false)
	if listResp.Error != nil {
		t.Fatalf("instance_list_children failed: %#v", listResp.Error)
	}
	listOut, _ := structuredMap(t, listResp.Result)["output"].(map[string]any)
	if listOut["hasMore"] != true {
		t.Fatalf("expected hasMore=true: %#v", listOut)
	}
	if listOut["nextOffset"] != float64(3) {
		t.Fatalf("unexpected nextOffset: %#v", listOut)
	}

	findResp := callTool(t, client, "roblox.instance_find", map[string]any{
		"rootPath":     "Workspace",
		"nameContains": "spawn",
		"limit":        1,
		"offset":       3,
	}, "corr-e2e-read-4", false)
	if findResp.Error != nil {
		t.Fatalf("instance_find failed: %#v", findResp.Error)
	}
	findOut, _ := structuredMap(t, findResp.Result)["output"].(map[string]any)
	if findOut["hasMore"] != true {
		t.Fatalf("expected hasMore=true: %#v", findOut)
	}
	if findOut["nextOffset"] != float64(4) {
		t.Fatalf("unexpected nextOffset: %#v", findOut)
	}

	observed := map[roblox.Operation]roblox.Request{}
	for i := 0; i < 4; i++ {
		select {
		case req := <-requests:
			observed[req.Operation] = req
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for read/query request %d", i+1)
		}
	}
	if _, ok := observed[roblox.OpScriptGetSource]; !ok {
		t.Fatalf("did not observe script.get_source request: %#v", observed)
	}
	if _, ok := observed[roblox.OpInstanceGet]; !ok {
		t.Fatalf("did not observe instance.get request: %#v", observed)
	}
	listReq, ok := observed[roblox.OpInstanceListChildren]
	if !ok {
		t.Fatalf("did not observe instance.list_children request: %#v", observed)
	}
	if listReq.Payload["limit"] != float64(2) || listReq.Payload["offset"] != float64(1) {
		t.Fatalf("unexpected instance.list_children payload: %#v", listReq.Payload)
	}
	findReq, ok := observed[roblox.OpInstanceFind]
	if !ok {
		t.Fatalf("did not observe instance.find request: %#v", observed)
	}
	if findReq.Payload["limit"] != float64(1) || findReq.Payload["offset"] != float64(3) {
		t.Fatalf("unexpected instance.find payload: %#v", findReq.Payload)
	}
}

func TestBridgeE2EAuthFailure(t *testing.T) {
	addr, stop := startBridgeTestServer(t, false, "expected-token", func(req roblox.Request) roblox.Response {
		return roblox.Response{
			RequestID:     req.RequestID,
			CorrelationID: req.CorrelationID,
			Success:       true,
			Timestamp:     time.Now().UTC(),
		}
	})
	defer stop()

	client, cleanup := newBridgeE2EServer(t, addr, "wrong-token")
	defer cleanup()

	resp := callTool(t, client, "roblox.bridge_ping", map[string]any{}, "corr-e2e-auth-fail", false)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != errorCodeDenied {
		t.Fatalf("unexpected error code: %d", resp.Error.Code)
	}
}

func newBridgeE2EServer(t *testing.T, bridgeAddr string, token string) (*sdkmcp.ClientSession, func()) {
	t.Helper()

	registry := action.NewInMemoryRegistry()
	transport := roblox.NewTCPTransport("tcp", bridgeAddr, 2*time.Second, roblox.TCPTransportOptions{
		AuthToken:   token,
		RequireAuth: true,
		ClientName:  "gox-e2e",
	})
	bridgeClient := roblox.NewClient(transport, slog.Default(), 2*time.Second)

	if err := registry.Register(actions.NewBridgePingAction(bridgeClient)); err != nil {
		t.Fatalf("register bridge ping action: %v", err)
	}
	if err := registry.Register(actions.NewScriptCreateAction(bridgeClient)); err != nil {
		t.Fatalf("register script create action: %v", err)
	}
	if err := registry.Register(actions.NewScriptUpdateAction(bridgeClient)); err != nil {
		t.Fatalf("register script update action: %v", err)
	}
	if err := registry.Register(actions.NewScriptDeleteAction(bridgeClient)); err != nil {
		t.Fatalf("register script delete action: %v", err)
	}
	if err := registry.Register(actions.NewScriptGetSourceAction(bridgeClient)); err != nil {
		t.Fatalf("register script get source action: %v", err)
	}
	if err := registry.Register(actions.NewScriptExecuteAction(bridgeClient)); err != nil {
		t.Fatalf("register script execute action: %v", err)
	}
	if err := registry.Register(actions.NewInstanceCreateAction(bridgeClient)); err != nil {
		t.Fatalf("register instance create action: %v", err)
	}
	if err := registry.Register(actions.NewInstanceSetPropertyAction(bridgeClient)); err != nil {
		t.Fatalf("register instance set property action: %v", err)
	}
	if err := registry.Register(actions.NewInstanceDeleteAction(bridgeClient)); err != nil {
		t.Fatalf("register instance delete action: %v", err)
	}
	if err := registry.Register(actions.NewInstanceGetAction(bridgeClient)); err != nil {
		t.Fatalf("register instance get action: %v", err)
	}
	if err := registry.Register(actions.NewInstanceListChildrenAction(bridgeClient)); err != nil {
		t.Fatalf("register instance list children action: %v", err)
	}
	if err := registry.Register(actions.NewInstanceFindAction(bridgeClient)); err != nil {
		t.Fatalf("register instance find action: %v", err)
	}

	executor := usecase.NewExecuteActionService(
		registry,
		policy.AllowAllEvaluator{},
		runlog.NewInMemoryStore(),
		slog.Default(),
		3*time.Second,
		idSequence("corr-seq-1", "run-seq-1", "corr-seq-2", "run-seq-2"),
	)
	server := NewServer("gox-e2e", nil, nil, slog.Default(), registry, executor)
	clientSession, closeMCP := connectMCPClient(t, server)
	return clientSession, func() {
		closeMCP()
		_ = bridgeClient.Close()
	}
}

func idSequence(ids ...string) usecase.IDGenerator {
	i := 0
	return func() string {
		if i >= len(ids) {
			return "seq-overflow"
		}
		out := ids[i]
		i++
		return out
	}
}

func startBridgeTestServer(
	t *testing.T,
	closeAfterSingleResponse bool,
	expectedToken string,
	handler func(req roblox.Request) roblox.Response,
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
			go handleBridgeConn(conn, closeAfterSingleResponse, expectedToken, handler)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func handleBridgeConn(
	conn net.Conn,
	closeAfterSingleResponse bool,
	expectedToken string,
	handler func(req roblox.Request) roblox.Response,
) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	authenticated := false
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var req roblox.Request
		if err := json.Unmarshal(line, &req); err != nil {
			return
		}
		var resp roblox.Response
		if !authenticated {
			if req.Operation != roblox.OpAuth {
				resp = roblox.Response{
					RequestID:     req.RequestID,
					CorrelationID: req.CorrelationID,
					Success:       false,
					Error: &roblox.BridgeError{
						Code:    "ACCESS_DENIED",
						Message: "authentication required",
					},
					Timestamp: time.Now().UTC(),
				}
			} else {
				token, _ := req.Payload["token"].(string)
				if token != expectedToken {
					resp = roblox.Response{
						RequestID:     req.RequestID,
						CorrelationID: req.CorrelationID,
						Success:       false,
						Error: &roblox.BridgeError{
							Code:    "ACCESS_DENIED",
							Message: "invalid auth token",
						},
						Timestamp: time.Now().UTC(),
					}
				} else {
					authenticated = true
					resp = roblox.Response{
						RequestID:     req.RequestID,
						CorrelationID: req.CorrelationID,
						Success:       true,
						Payload: map[string]any{
							"authenticated": true,
							"sessionId":     "session-test",
						},
						Timestamp: time.Now().UTC(),
					}
				}
			}
		} else {
			if req.Operation == roblox.OpHello {
				resp = roblox.Response{
					RequestID:     req.RequestID,
					CorrelationID: req.CorrelationID,
					Success:       true,
					Payload: map[string]any{
						"protocolVersion": "1.1",
						"capabilities":    []any{"typed-values.v2", "instance-id", "idempotency-key"},
					},
					Timestamp: time.Now().UTC(),
				}
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
		if closeAfterSingleResponse && req.Operation != roblox.OpAuth && req.Operation != roblox.OpHello {
			return
		}
	}
}
