package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/roushou/gox/internal/domain/runlog"
)

const (
	resourceURIStatus      = "gox://status"
	resourceURIRuns        = "gox://runs"
	resourceTemplateRunURI = "gox://runs/{runId}"
)

func (s *Server) registerResources() {
	s.server.AddResource(&sdkmcp.Resource{
		Name:        "server_status",
		Title:       "Server Status",
		Description: "Current MCP server status and capabilities summary",
		MIMEType:    "application/json",
		URI:         resourceURIStatus,
	}, s.handleServerStatusResource)

	s.server.AddResource(&sdkmcp.Resource{
		Name:        "runlog_list",
		Title:       "Run Logs",
		Description: "List of recent action/workflow run records",
		MIMEType:    "application/json",
		URI:         resourceURIRuns,
	}, s.handleRunsResource)

	s.server.AddResourceTemplate(&sdkmcp.ResourceTemplate{
		Name:        "runlog_item",
		Title:       "Run Log Record",
		Description: "Single run record identified by runId",
		MIMEType:    "application/json",
		URITemplate: resourceTemplateRunURI,
	}, s.handleRunByIDResource)
}

func (s *Server) handleServerStatusResource(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	if req.Params.URI != resourceURIStatus {
		return nil, sdkmcp.ResourceNotFoundError(req.Params.URI)
	}
	actionsCount := 0
	if s.catalog != nil {
		actionsCount = len(s.catalog.List())
	}
	status := map[string]any{
		"version":      serverVersion,
		"actionsCount": actionsCount,
		"serverTime":   time.Now().UTC().Format(time.RFC3339Nano),
	}
	return resourceJSON(req.Params.URI, status)
}

func (s *Server) handleRunsResource(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	if req.Params.URI != resourceURIRuns {
		return nil, sdkmcp.ResourceNotFoundError(req.Params.URI)
	}
	records := s.runlogs.List(context.Background())
	sort.Slice(records, func(i, j int) bool {
		return records[i].StartedAt.After(records[j].StartedAt)
	})
	if len(records) > 100 {
		records = records[:100]
	}
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, runRecordJSON(record))
	}
	return resourceJSON(req.Params.URI, map[string]any{"runs": out})
}

func (s *Server) handleRunByIDResource(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	uri := strings.TrimSpace(req.Params.URI)
	prefix := "gox://runs/"
	if !strings.HasPrefix(uri, prefix) {
		return nil, sdkmcp.ResourceNotFoundError(uri)
	}
	runID := strings.TrimPrefix(uri, prefix)
	if runID == "" || strings.Contains(runID, "/") {
		return nil, sdkmcp.ResourceNotFoundError(uri)
	}
	record, ok := s.runlogs.Get(context.Background(), runID)
	if !ok {
		return nil, sdkmcp.ResourceNotFoundError(uri)
	}
	return resourceJSON(uri, runRecordJSON(record))
}

func resourceJSON(uri string, value any) (*sdkmcp.ReadResourceResult, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal resource %q: %w", uri, err)
	}
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{
				URI:      uri,
				MIMEType: "application/json",
				Text:     string(payload),
			},
		},
	}, nil
}

func runRecordJSON(record runlog.Record) map[string]any {
	out := map[string]any{
		"runId":         record.RunID,
		"correlationId": record.CorrelationID,
		"type":          string(record.Type),
		"name":          record.Name,
		"status":        string(record.Status),
		"startedAt":     record.StartedAt.UTC().Format(time.RFC3339Nano),
		"diffSummary":   record.DiffSummary,
	}
	if !record.EndedAt.IsZero() {
		out["endedAt"] = record.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	if record.ErrorCode != "" {
		out["errorCode"] = record.ErrorCode
	}
	if record.ErrorMessage != "" {
		out["errorMessage"] = record.ErrorMessage
	}
	return out
}
