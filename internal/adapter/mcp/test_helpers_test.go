package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type toolCallResponse struct {
	Result *sdkmcp.CallToolResult
	Error  *jsonrpc.Error
}

func connectMCPClient(t *testing.T, server *Server) (*sdkmcp.ClientSession, func()) {
	t.Helper()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

	serverSession, err := server.server.Connect(ctx, serverTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect server transport: %v", err)
	}

	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "gox-test-client",
		Version: "v0.1.0",
	}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		cancel()
		t.Fatalf("connect client session: %v", err)
	}

	cleanup := func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
		cancel()
	}
	return clientSession, cleanup
}

func callTool(t *testing.T, client *sdkmcp.ClientSession, name string, arguments map[string]any, correlationID string, dryRun bool) toolCallResponse {
	t.Helper()
	params := &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: arguments,
	}
	meta := sdkmcp.Meta{}
	if correlationID != "" {
		meta["correlationId"] = correlationID
	}
	if dryRun {
		meta["dryRun"] = true
	}
	if len(meta) > 0 {
		params.Meta = meta
	}

	result, err := client.CallTool(context.Background(), params)
	if err == nil {
		return toolCallResponse{Result: result}
	}

	var wireErr *jsonrpc.Error
	if errors.As(err, &wireErr) {
		return toolCallResponse{Error: wireErr}
	}
	return toolCallResponse{
		Error: &jsonrpc.Error{
			Code:    jsonrpc.CodeInternalError,
			Message: err.Error(),
		},
	}
}

func decodeErrorData(t *testing.T, err *jsonrpc.Error) map[string]any {
	t.Helper()
	if err == nil || len(err.Data) == 0 {
		return nil
	}
	var data map[string]any
	if unmarshalErr := json.Unmarshal(err.Data, &data); unmarshalErr != nil {
		t.Fatalf("unmarshal error data: %v", unmarshalErr)
	}
	return data
}
