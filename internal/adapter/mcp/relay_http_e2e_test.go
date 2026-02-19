package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/app/actions"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/policy"
	"github.com/roushou/gox/internal/domain/runlog"
	"github.com/roushou/gox/internal/usecase"
)

func TestRelayHTTPBridgeE2E(t *testing.T) {
	const token = "relay-e2e-token"
	hub := roblox.NewRelayHub(token)
	relay := roblox.NewRelayHTTPServer("127.0.0.1:0", hub, token, slog.Default())
	httpSrv := httptest.NewServer(relay.Handler())
	defer httpSrv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	requests := make(chan roblox.Request, 4)
	errCh := make(chan error, 1)
	go runPluginRelayClient(ctx, httpSrv.URL, token, requests, errCh)

	waitForRelaySession(t, hub)

	registry := action.NewInMemoryRegistry()
	transport := roblox.NewRelayTransport(hub, roblox.TCPTransportOptions{
		AuthToken:   token,
		RequireAuth: true,
		ClientName:  "gox-relay-e2e",
	})
	bridgeClient := roblox.NewClient(transport, slog.Default(), 2*time.Second)
	defer func() { _ = bridgeClient.Close() }()

	if err := registry.Register(actions.NewBridgePingAction(bridgeClient)); err != nil {
		t.Fatalf("register bridge ping: %v", err)
	}
	if err := registry.Register(actions.NewScriptCreateAction(bridgeClient)); err != nil {
		t.Fatalf("register script create: %v", err)
	}
	if err := registry.Register(actions.NewScriptExecuteAction(bridgeClient)); err != nil {
		t.Fatalf("register script execute: %v", err)
	}

	executor := usecase.NewExecuteActionService(
		registry,
		policy.AllowAllEvaluator{},
		runlog.NewInMemoryStore(),
		slog.Default(),
		3*time.Second,
		idSequence("corr-1", "run-1", "corr-2", "run-2"),
	)
	server := NewServer("gox-relay-e2e", nil, nil, slog.Default(), registry, executor)
	clientSession, cleanup := connectMCPClient(t, server)
	defer cleanup()

	resp := callTool(t, clientSession, "roblox.script_create", map[string]any{
		"parentPath": "ServerScriptService",
		"name":       "Main",
		"scriptType": "Script",
		"source":     "print('relay')",
	}, "corr-relay-1", false)
	if resp.Error != nil {
		t.Fatalf("unexpected tools/call error: %#v", resp.Error)
	}

	// We expect auth and operation requests to have traversed relay.
	seenAuth := false
	seenScriptCreate := false
	deadline := time.After(3 * time.Second)
	for !seenAuth || !seenScriptCreate {
		select {
		case req := <-requests:
			if req.Operation == roblox.OpAuth {
				seenAuth = true
			}
			if req.Operation == roblox.OpScriptCreate {
				seenScriptCreate = true
			}
		case err := <-errCh:
			if err != nil {
				t.Fatalf("plugin relay client error: %v", err)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for relay operations auth=%v scriptCreate=%v", seenAuth, seenScriptCreate)
		}
	}
}

func TestRelayHTTPBridgeE2EScriptExecuteVariants(t *testing.T) {
	const token = "relay-e2e-token-execute"
	hub := roblox.NewRelayHub(token)
	relay := roblox.NewRelayHTTPServer("127.0.0.1:0", hub, token, slog.Default())
	httpSrv := httptest.NewServer(relay.Handler())
	defer httpSrv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	requests := make(chan roblox.Request, 16)
	errCh := make(chan error, 1)
	go runPluginRelayClient(ctx, httpSrv.URL, token, requests, errCh)

	waitForRelaySession(t, hub)

	registry := action.NewInMemoryRegistry()
	transport := roblox.NewRelayTransport(hub, roblox.TCPTransportOptions{
		AuthToken:   token,
		RequireAuth: true,
		ClientName:  "gox-relay-e2e-execute",
	})
	bridgeClient := roblox.NewClient(transport, slog.Default(), 2*time.Second)
	defer func() { _ = bridgeClient.Close() }()

	if err := registry.Register(actions.NewScriptExecuteAction(bridgeClient)); err != nil {
		t.Fatalf("register script execute: %v", err)
	}

	executor := usecase.NewExecuteActionService(
		registry,
		policy.AllowAllEvaluator{},
		runlog.NewInMemoryStore(),
		slog.Default(),
		3*time.Second,
		idSequence("run-exec-1", "run-exec-2", "run-exec-3", "run-exec-4"),
	)
	server := NewServer("gox-relay-e2e-execute", nil, nil, slog.Default(), registry, executor)
	clientSession, cleanup := connectMCPClient(t, server)
	defer cleanup()

	moduleResp := callTool(t, clientSession, "roblox.script_execute", map[string]any{
		"scriptPath": "ServerScriptService.BuildOlympus",
	}, "corr-relay-exec-1", false)
	if moduleResp.Error != nil {
		t.Fatalf("script_execute module variant failed: %#v", moduleResp.Error)
	}
	moduleOut, _ := structuredMap(t, moduleResp.Result)["output"].(map[string]any)
	if moduleOut["called"] != false {
		t.Fatalf("expected called=false, got %#v", moduleOut)
	}
	if moduleOut["returnValue"] != "module-export" {
		t.Fatalf("unexpected module return value: %#v", moduleOut)
	}

	functionResp := callTool(t, clientSession, "roblox.script_execute", map[string]any{
		"scriptPath":   "ServerScriptService.BuildOlympus",
		"functionName": "run",
		"args":         []any{"Olympus"},
		"expectReturn": true,
	}, "corr-relay-exec-2", false)
	if functionResp.Error != nil {
		t.Fatalf("script_execute function variant failed: %#v", functionResp.Error)
	}
	functionOut, _ := structuredMap(t, functionResp.Result)["output"].(map[string]any)
	if functionOut["called"] != true {
		t.Fatalf("expected called=true, got %#v", functionOut)
	}
	returnValue, _ := functionOut["returnValue"].(map[string]any)
	if returnValue["built"] != true {
		t.Fatalf("unexpected function return value: %#v", functionOut)
	}

	var seenModule, seenFunction bool
	deadline := time.After(3 * time.Second)
	for !seenModule || !seenFunction {
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
		case err := <-errCh:
			if err != nil {
				t.Fatalf("plugin relay client error: %v", err)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for execute variants: module=%v function=%v", seenModule, seenFunction)
		}
	}
}

func TestRelayHTTPBridgeE2EReadQueryOperations(t *testing.T) {
	const token = "relay-e2e-token-read"
	hub := roblox.NewRelayHub(token)
	relay := roblox.NewRelayHTTPServer("127.0.0.1:0", hub, token, slog.Default())
	httpSrv := httptest.NewServer(relay.Handler())
	defer httpSrv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	requests := make(chan roblox.Request, 12)
	errCh := make(chan error, 1)
	go runPluginRelayClient(ctx, httpSrv.URL, token, requests, errCh)

	waitForRelaySession(t, hub)

	registry := action.NewInMemoryRegistry()
	transport := roblox.NewRelayTransport(hub, roblox.TCPTransportOptions{
		AuthToken:   token,
		RequireAuth: true,
		ClientName:  "gox-relay-e2e-read",
	})
	bridgeClient := roblox.NewClient(transport, slog.Default(), 2*time.Second)
	defer func() { _ = bridgeClient.Close() }()

	if err := registry.Register(actions.NewScriptGetSourceAction(bridgeClient)); err != nil {
		t.Fatalf("register script get source: %v", err)
	}
	if err := registry.Register(actions.NewInstanceGetAction(bridgeClient)); err != nil {
		t.Fatalf("register instance get: %v", err)
	}
	if err := registry.Register(actions.NewInstanceListChildrenAction(bridgeClient)); err != nil {
		t.Fatalf("register instance list children: %v", err)
	}
	if err := registry.Register(actions.NewInstanceFindAction(bridgeClient)); err != nil {
		t.Fatalf("register instance find: %v", err)
	}

	executor := usecase.NewExecuteActionService(
		registry,
		policy.AllowAllEvaluator{},
		runlog.NewInMemoryStore(),
		slog.Default(),
		3*time.Second,
		idSequence(
			"run-1", "run-2", "run-3", "run-4",
			"run-5", "run-6", "run-7", "run-8",
		),
	)
	server := NewServer("gox-relay-e2e-read", nil, nil, slog.Default(), registry, executor)
	clientSession, cleanup := connectMCPClient(t, server)
	defer cleanup()

	scriptResp := callTool(t, clientSession, "roblox.script_get_source", map[string]any{
		"scriptPath": "ServerScriptService.Main",
	}, "corr-relay-read-1", false)
	if scriptResp.Error != nil {
		t.Fatalf("script_get_source failed: %#v", scriptResp.Error)
	}
	scriptOut, _ := structuredMap(t, scriptResp.Result)["output"].(map[string]any)
	if scriptOut["path"] != "ServerScriptService.Main" {
		t.Fatalf("unexpected script_get_source output: %#v", scriptOut)
	}

	instanceResp := callTool(t, clientSession, "roblox.instance_get", map[string]any{
		"instancePath": "Workspace.Baseplate",
	}, "corr-relay-read-2", false)
	if instanceResp.Error != nil {
		t.Fatalf("instance_get failed: %#v", instanceResp.Error)
	}
	instanceOut, _ := structuredMap(t, instanceResp.Result)["output"].(map[string]any)
	if _, ok := instanceOut["instance"].(map[string]any); !ok {
		t.Fatalf("unexpected instance_get output: %#v", instanceOut)
	}

	listResp := callTool(t, clientSession, "roblox.instance_list_children", map[string]any{
		"parentPath": "Workspace",
		"limit":      2,
		"offset":     1,
	}, "corr-relay-read-3", false)
	if listResp.Error != nil {
		t.Fatalf("instance_list_children failed: %#v", listResp.Error)
	}
	listOut, _ := structuredMap(t, listResp.Result)["output"].(map[string]any)
	if listOut["hasMore"] != true {
		t.Fatalf("expected hasMore=true, got %#v", listOut)
	}
	if listOut["nextOffset"] != float64(3) {
		t.Fatalf("expected nextOffset=3, got %#v", listOut)
	}

	findResp := callTool(t, clientSession, "roblox.instance_find", map[string]any{
		"rootPath":     "Workspace",
		"nameContains": "spawn",
		"limit":        1,
		"offset":       3,
	}, "corr-relay-read-4", false)
	if findResp.Error != nil {
		t.Fatalf("instance_find failed: %#v", findResp.Error)
	}
	findOut, _ := structuredMap(t, findResp.Result)["output"].(map[string]any)
	if findOut["hasMore"] != true {
		t.Fatalf("expected hasMore=true, got %#v", findOut)
	}
	if findOut["nextOffset"] != float64(4) {
		t.Fatalf("expected nextOffset=4, got %#v", findOut)
	}

	observed := map[roblox.Operation]roblox.Request{}
	deadline := time.After(3 * time.Second)
	for {
		if _, ok := observed[roblox.OpScriptGetSource]; ok {
			if _, ok := observed[roblox.OpInstanceGet]; ok {
				if _, ok := observed[roblox.OpInstanceListChildren]; ok {
					if _, ok := observed[roblox.OpInstanceFind]; ok {
						break
					}
				}
			}
		}
		select {
		case req := <-requests:
			observed[req.Operation] = req
		case err := <-errCh:
			if err != nil {
				t.Fatalf("plugin relay client error: %v", err)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for relay read/query operations: %#v", observed)
		}
	}

	listReq, ok := observed[roblox.OpInstanceListChildren]
	if !ok {
		t.Fatalf("missing instance.list_children request: %#v", observed)
	}
	if listReq.Payload["limit"] != float64(2) || listReq.Payload["offset"] != float64(1) {
		t.Fatalf("unexpected instance.list_children payload: %#v", listReq.Payload)
	}
	findReq, ok := observed[roblox.OpInstanceFind]
	if !ok {
		t.Fatalf("missing instance.find request: %#v", observed)
	}
	if findReq.Payload["limit"] != float64(1) || findReq.Payload["offset"] != float64(3) {
		t.Fatalf("unexpected instance.find payload: %#v", findReq.Payload)
	}
}

func waitForRelaySession(t *testing.T, hub *roblox.RelayHub) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hub.ActiveSessionID() != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for active relay session")
}

func runPluginRelayClient(
	ctx context.Context,
	baseURL string,
	authToken string,
	requests chan<- roblox.Request,
	errCh chan<- error,
) {
	sessionID, err := relayOpenSession(baseURL, authToken, "plugin-relay-e2e")
	if err != nil {
		errCh <- err
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line, hasLine, err := relayRead(baseURL, authToken, sessionID, 500)
		if err != nil {
			errCh <- err
			return
		}
		if !hasLine {
			continue
		}

		var req roblox.Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			errCh <- err
			return
		}
		requests <- req

		var resp roblox.Response
		switch req.Operation {
		case roblox.OpAuth:
			token, _ := req.Payload["token"].(string)
			if token != authToken {
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
				resp = roblox.Response{
					RequestID:     req.RequestID,
					CorrelationID: req.CorrelationID,
					Success:       true,
					Payload:       map[string]any{"authenticated": true},
					Timestamp:     time.Now().UTC(),
				}
			}
		case roblox.OpHello:
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
		case roblox.OpScriptCreate:
			resp = roblox.Response{
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
		case roblox.OpScriptGetSource:
			resp = roblox.Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload: map[string]any{
					"path":      "ServerScriptService.Main",
					"className": "Script",
					"source":    "print('relay')",
				},
				Timestamp: time.Now().UTC(),
			}
		case roblox.OpScriptExecute:
			functionName, _ := req.Payload["functionName"].(string)
			if functionName != "" {
				resp = roblox.Response{
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
			} else {
				resp = roblox.Response{
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
			}
		case roblox.OpInstanceGet:
			resp = roblox.Response{
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
			resp = roblox.Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload: map[string]any{
					"parentPath": req.Payload["parentPath"],
					"limit":      limit,
					"offset":     offset,
					"total":      5,
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
			offset, _ := req.Payload["offset"].(float64)
			resp = roblox.Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload: map[string]any{
					"rootPath":   req.Payload["rootPath"],
					"limit":      req.Payload["limit"],
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
			resp = roblox.Response{
				RequestID:     req.RequestID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Payload:       map[string]any{"ok": true},
				Timestamp:     time.Now().UTC(),
			}
		}

		b, _ := json.Marshal(resp)
		if err := relayWrite(baseURL, authToken, sessionID, string(b)); err != nil {
			errCh <- err
			return
		}
	}
}

func relayOpenSession(baseURL, authToken, clientName string) (string, error) {
	payload := map[string]any{
		"clientName": clientName,
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/bridge/session", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gox-Auth", authToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	sessionID, _ := out["sessionId"].(string)
	if sessionID == "" {
		return "", fmt.Errorf("missing session id")
	}
	return sessionID, nil
}

func relayRead(baseURL, authToken, sessionID string, timeoutMS int) (string, bool, error) {
	req, _ := http.NewRequest(
		http.MethodGet,
		baseURL+"/v1/bridge/session/"+sessionID+"/read?timeout_ms="+strconv.Itoa(timeoutMS),
		nil,
	)
	req.Header.Set("X-Gox-Auth", authToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", false, err
	}
	line, _ := out["line"].(string)
	return line, line != "", nil
}

func relayWrite(baseURL, authToken, sessionID, line string) error {
	b, _ := json.Marshal(map[string]any{
		"line": line,
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/bridge/session/"+sessionID+"/write", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gox-Auth", authToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
