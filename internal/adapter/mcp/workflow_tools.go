package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/roushou/gox/internal/domain/workflow"
)

type workflowRunToolInput struct {
	Definition    workflowDefinitionInput `json:"definition"`
	Actor         string                  `json:"actor,omitempty"`
	CorrelationID string                  `json:"correlationId,omitempty"`
	DryRun        bool                    `json:"dryRun,omitempty"`
	Approved      bool                    `json:"approved,omitempty"`
}

type workflowDefinitionInput struct {
	ID    string              `json:"id"`
	Steps []workflowStepInput `json:"steps"`
}

type workflowStepInput struct {
	ID        string                 `json:"id,omitempty"`
	ActionID  string                 `json:"actionId"`
	Input     map[string]any         `json:"input,omitempty"`
	TimeoutMS int                    `json:"timeoutMs,omitempty"`
	Retry     workflowStepRetryInput `json:"retry,omitempty"`
	When      *workflowStepWhenInput `json:"when,omitempty"`
}

type workflowStepRetryInput struct {
	MaxAttempts int `json:"maxAttempts,omitempty"`
	BackoffMS   int `json:"backoffMs,omitempty"`
}

type workflowStepWhenInput struct {
	StepID string `json:"stepId"`
	Path   string `json:"path,omitempty"`
	Equals any    `json:"equals"`
}

type workflowStatusToolInput struct {
	RunID string `json:"runId"`
}

type workflowListToolInput struct {
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type workflowLogsToolInput struct {
	RunID  string `json:"runId"`
	Limit  int    `json:"limit,omitempty"`
	Level  string `json:"level,omitempty"`
	StepID string `json:"stepId,omitempty"`
	Since  string `json:"since,omitempty"`
}

type workflowCancelToolOutput struct {
	workflowRunSnapshot
	Acknowledged bool `json:"acknowledged"`
}

type workflowListToolOutput struct {
	Runs       []workflowRunSnapshot `json:"runs"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

type workflowLogsToolOutput struct {
	RunID string             `json:"runId"`
	Logs  []workflowLogEntry `json:"logs"`
}

func (s *Server) registerWorkflowTools() {
	s.server.AddTool(&sdkmcp.Tool{
		Name:         "workflow.run",
		Title:        "Workflow Run",
		Description:  "Starts a workflow run and returns its run state",
		InputSchema:  workflowRunInputSchema(),
		OutputSchema: workflowStateOutputSchema(),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint: false,
		},
		Meta: sdkmcp.Meta{
			"gox:type": "workflow",
		},
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return s.handleWorkflowRun(ctx, req)
	})

	s.server.AddTool(&sdkmcp.Tool{
		Name:         "workflow.status",
		Title:        "Workflow Status",
		Description:  "Returns current status for a workflow run",
		InputSchema:  workflowRunIDInputSchema(),
		OutputSchema: workflowStateOutputSchema(),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
		Meta: sdkmcp.Meta{
			"gox:type": "workflow",
		},
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return s.handleWorkflowStatus(ctx, req)
	})

	s.server.AddTool(&sdkmcp.Tool{
		Name:         "workflow.cancel",
		Title:        "Workflow Cancel",
		Description:  "Requests cancellation for a workflow run",
		InputSchema:  workflowRunIDInputSchema(),
		OutputSchema: workflowCancelOutputSchema(),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint: false,
		},
		Meta: sdkmcp.Meta{
			"gox:type": "workflow",
		},
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return s.handleWorkflowCancel(ctx, req)
	})

	s.server.AddTool(&sdkmcp.Tool{
		Name:         "workflow.list",
		Title:        "Workflow List",
		Description:  "Lists workflow runs",
		InputSchema:  workflowListInputSchema(),
		OutputSchema: workflowListOutputSchema(),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
		Meta: sdkmcp.Meta{
			"gox:type": "workflow",
		},
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return s.handleWorkflowList(ctx, req)
	})

	s.server.AddTool(&sdkmcp.Tool{
		Name:         "workflow.logs",
		Title:        "Workflow Logs",
		Description:  "Returns workflow logs for a run",
		InputSchema:  workflowLogsInputSchema(),
		OutputSchema: workflowLogsOutputSchema(),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
		Meta: sdkmcp.Meta{
			"gox:type": "workflow",
		},
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return s.handleWorkflowLogs(ctx, req)
	})
}

func (s *Server) handleWorkflowRun(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	if err := s.guards.checkAndConsume(req.Params.Arguments); err != nil {
		return nil, err
	}
	var input workflowRunToolInput
	if err := decodeToolArguments(req, &input); err != nil {
		return nil, wireError(
			jsonrpc.CodeInvalidParams,
			"invalid workflow.run arguments",
			map[string]any{"details": err.Error()},
		)
	}

	def := workflow.Definition{
		ID:    input.Definition.ID,
		Steps: make([]workflow.Step, 0, len(input.Definition.Steps)),
	}
	for _, step := range input.Definition.Steps {
		var when *workflow.StepCondition
		if step.When != nil {
			when = &workflow.StepCondition{
				StepID: step.When.StepID,
				Path:   step.When.Path,
				Equals: step.When.Equals,
			}
		}
		def.Steps = append(def.Steps, workflow.Step{
			ID:        step.ID,
			ActionID:  step.ActionID,
			Input:     step.Input,
			TimeoutMS: step.TimeoutMS,
			When:      when,
			Retry: workflow.RetryPolicy{
				MaxAttempts: step.Retry.MaxAttempts,
				BackoffMS:   step.Retry.BackoffMS,
			},
		})
	}
	snapshot, err := s.workflow.Start(def, workflowRunOptions{
		Actor:         input.Actor,
		CorrelationID: input.CorrelationID,
		DryRun:        input.DryRun,
		Approved:      input.Approved,
	})
	if err != nil {
		return nil, s.toCallToolError(err)
	}
	return s.toolResultWithStructured(snapshot)
}

func (s *Server) handleWorkflowStatus(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	if err := s.guards.checkAndConsume(req.Params.Arguments); err != nil {
		return nil, err
	}
	var input workflowStatusToolInput
	if err := decodeToolArguments(req, &input); err != nil {
		return nil, wireError(
			jsonrpc.CodeInvalidParams,
			"invalid workflow.status arguments",
			map[string]any{"details": err.Error()},
		)
	}
	snapshot, err := s.workflow.Status(input.RunID)
	if err != nil {
		return nil, s.toCallToolError(err)
	}
	return s.toolResultWithStructured(snapshot)
}

func (s *Server) handleWorkflowCancel(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	if err := s.guards.checkAndConsume(req.Params.Arguments); err != nil {
		return nil, err
	}
	var input workflowStatusToolInput
	if err := decodeToolArguments(req, &input); err != nil {
		return nil, wireError(
			jsonrpc.CodeInvalidParams,
			"invalid workflow.cancel arguments",
			map[string]any{"details": err.Error()},
		)
	}
	snapshot, acknowledged, err := s.workflow.Cancel(input.RunID)
	if err != nil {
		return nil, s.toCallToolError(err)
	}
	return s.toolResultWithStructured(workflowCancelToolOutput{
		workflowRunSnapshot: snapshot,
		Acknowledged:        acknowledged,
	})
}

func (s *Server) handleWorkflowList(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	if err := s.guards.checkAndConsume(req.Params.Arguments); err != nil {
		return nil, err
	}
	var input workflowListToolInput
	if err := decodeToolArguments(req, &input); err != nil {
		return nil, wireError(
			jsonrpc.CodeInvalidParams,
			"invalid workflow.list arguments",
			map[string]any{"details": err.Error()},
		)
	}
	runs, nextCursor, err := s.workflow.List(workflowListOptions{
		Status: input.Status,
		Limit:  input.Limit,
		Cursor: input.Cursor,
	})
	if err != nil {
		return nil, s.toCallToolError(err)
	}
	return s.toolResultWithStructured(workflowListToolOutput{
		Runs:       runs,
		NextCursor: nextCursor,
	})
}

func (s *Server) handleWorkflowLogs(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	if err := s.guards.checkAndConsume(req.Params.Arguments); err != nil {
		return nil, err
	}
	var input workflowLogsToolInput
	if err := decodeToolArguments(req, &input); err != nil {
		return nil, wireError(
			jsonrpc.CodeInvalidParams,
			"invalid workflow.logs arguments",
			map[string]any{"details": err.Error()},
		)
	}
	since, err := parseLogsSince(input.Since)
	if err != nil {
		return nil, wireError(
			jsonrpc.CodeInvalidParams,
			"invalid workflow.logs arguments",
			map[string]any{"details": err.Error()},
		)
	}
	logs, err := s.workflow.Logs(input.RunID, workflowLogOptions{
		Limit:  input.Limit,
		Level:  input.Level,
		StepID: input.StepID,
		Since:  since,
	})
	if err != nil {
		return nil, s.toCallToolError(err)
	}
	return s.toolResultWithStructured(workflowLogsToolOutput{
		RunID: input.RunID,
		Logs:  logs,
	})
}

func decodeToolArguments(req *sdkmcp.CallToolRequest, target any) error {
	if len(req.Params.Arguments) == 0 {
		return json.Unmarshal([]byte(`{}`), target)
	}
	return json.Unmarshal(req.Params.Arguments, target)
}

func workflowRunInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"definition": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
					"steps": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"id":       map[string]any{"type": "string"},
								"actionId": map[string]any{"type": "string"},
								"input":    map[string]any{"type": "object"},
								"timeoutMs": map[string]any{
									"type":    "integer",
									"minimum": 1,
								},
								"retry": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"maxAttempts": map[string]any{
											"type":    "integer",
											"minimum": 1,
										},
										"backoffMs": map[string]any{
											"type":    "integer",
											"minimum": 0,
										},
									},
									"additionalProperties": false,
								},
								"when": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"stepId": map[string]any{"type": "string"},
										"path":   map[string]any{"type": "string"},
										"equals": map[string]any{},
									},
									"required":             []string{"stepId", "equals"},
									"additionalProperties": false,
								},
							},
							"required":             []string{"actionId"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"id", "steps"},
				"additionalProperties": false,
			},
			"actor":         map[string]any{"type": "string"},
			"correlationId": map[string]any{"type": "string"},
			"dryRun":        map[string]any{"type": "boolean"},
			"approved":      map[string]any{"type": "boolean"},
		},
		"required":             []string{"definition"},
		"additionalProperties": false,
	}
}

func workflowRunIDInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"runId": map[string]any{"type": "string"},
		},
		"required":             []string{"runId"},
		"additionalProperties": false,
	}
}

func workflowListInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{"type": "string"},
			"limit":  map[string]any{"type": "integer", "minimum": 1},
			"cursor": map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	}
}

func workflowLogsInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"runId":  map[string]any{"type": "string"},
			"limit":  map[string]any{"type": "integer", "minimum": 1},
			"level":  map[string]any{"type": "string"},
			"stepId": map[string]any{"type": "string"},
			"since":  map[string]any{"type": "string"},
		},
		"required":             []string{"runId"},
		"additionalProperties": false,
	}
}

func workflowStateOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"runId":           map[string]any{"type": "string"},
			"workflowId":      map[string]any{"type": "string"},
			"correlationId":   map[string]any{"type": "string"},
			"status":          map[string]any{"type": "string"},
			"startedAt":       map[string]any{"type": "string"},
			"endedAt":         map[string]any{"type": "string"},
			"totalSteps":      map[string]any{"type": "integer"},
			"completedSteps":  map[string]any{"type": "integer"},
			"cancelRequested": map[string]any{"type": "boolean"},
			"lastError": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"code":    map[string]any{"type": "string"},
					"message": map[string]any{"type": "string"},
				},
				"required":             []string{"code", "message"},
				"additionalProperties": false,
			},
		},
		"required": []string{
			"runId",
			"workflowId",
			"correlationId",
			"status",
			"startedAt",
			"totalSteps",
			"completedSteps",
			"cancelRequested",
		},
		"additionalProperties": false,
	}
}

func workflowCancelOutputSchema() map[string]any {
	out := workflowStateOutputSchema()
	properties, _ := out["properties"].(map[string]any)
	properties["acknowledged"] = map[string]any{"type": "boolean"}
	required, _ := out["required"].([]string)
	out["required"] = append(required, "acknowledged")
	return out
}

func workflowListOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"runs": map[string]any{
				"type":  "array",
				"items": workflowStateOutputSchema(),
			},
			"nextCursor": map[string]any{"type": "string"},
		},
		"required":             []string{"runs"},
		"additionalProperties": false,
	}
}

func workflowLogsOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"runId": map[string]any{"type": "string"},
			"logs": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"timestamp": map[string]any{"type": "string"},
						"level":     map[string]any{"type": "string"},
						"message":   map[string]any{"type": "string"},
						"stepId":    map[string]any{"type": "string"},
						"actionId":  map[string]any{"type": "string"},
					},
					"required":             []string{"timestamp", "level", "message"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"runId", "logs"},
		"additionalProperties": false,
	}
}

func parseLogsSince(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	out, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("since must be RFC3339 timestamp")
	}
	return out, nil
}
