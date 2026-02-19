package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Client struct {
	session toolSession
}

func New(session *sdkmcp.ClientSession) *Client {
	return newWithSession(session)
}

type toolSession interface {
	CallTool(context.Context, *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error)
	ListResources(context.Context, *sdkmcp.ListResourcesParams) (*sdkmcp.ListResourcesResult, error)
	ReadResource(context.Context, *sdkmcp.ReadResourceParams) (*sdkmcp.ReadResourceResult, error)
}

func newWithSession(session toolSession) *Client {
	return &Client{session: session}
}

type CallOptions struct {
	Actor         string
	CorrelationID string
	DryRun        bool
}

type RetryOptions struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

type ActionResult struct {
	RunID  string
	Output map[string]any
	Diff   map[string]any
}

type WorkflowDefinition struct {
	ID    string
	Steps []WorkflowStep
}

type WorkflowStep struct {
	ID        string
	ActionID  string
	Input     map[string]any
	TimeoutMS int
	Retry     WorkflowRetry
}

type WorkflowRetry struct {
	MaxAttempts int
	BackoffMS   int
}

type WorkflowRunSnapshot struct {
	RunID           string            `json:"runId"`
	WorkflowID      string            `json:"workflowId"`
	CorrelationID   string            `json:"correlationId"`
	Status          string            `json:"status"`
	StartedAt       string            `json:"startedAt"`
	EndedAt         string            `json:"endedAt,omitempty"`
	TotalSteps      int               `json:"totalSteps"`
	CompletedSteps  int               `json:"completedSteps"`
	CancelRequested bool              `json:"cancelRequested"`
	LastError       *WorkflowRunError `json:"lastError,omitempty"`
}

type WorkflowRunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type WorkflowCancelResult struct {
	WorkflowRunSnapshot
	Acknowledged bool `json:"acknowledged"`
}

type WorkflowListResult struct {
	Runs       []WorkflowRunSnapshot `json:"runs"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

type WorkflowLogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	StepID    string `json:"stepId,omitempty"`
	ActionID  string `json:"actionId,omitempty"`
}

type WorkflowLogsResult struct {
	RunID string             `json:"runId"`
	Logs  []WorkflowLogEntry `json:"logs"`
}

type WorkflowListRequest struct {
	Status string
	Limit  int
	Cursor string
}

type WorkflowLogsRequest struct {
	RunID  string
	Limit  int
	Level  string
	StepID string
	Since  string
}

type CallError struct {
	Code    int64
	Message string
	Data    map[string]any
	Cause   error
}

const (
	defaultRetryMaxAttempts    = 3
	defaultRetryInitialBackoff = 200 * time.Millisecond
	defaultRetryMaxBackoff     = 2 * time.Second
)

func (e *CallError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return fmt.Sprintf("mcp call error (%d): %s", e.Code, e.Message)
	}
	return fmt.Sprintf("mcp call error (%d): %s: %v", e.Code, e.Message, e.Cause)
}

func (c *Client) CallAction(ctx context.Context, actionID string, input map[string]any, opts CallOptions) (ActionResult, error) {
	callResult, err := c.callTool(ctx, actionID, input, opts)
	if err != nil {
		return ActionResult{}, err
	}
	return parseActionResult(callResult)
}

func (c *Client) CallActionWithRetry(ctx context.Context, actionID string, input map[string]any, opts CallOptions, retry RetryOptions) (ActionResult, error) {
	callResult, err := c.CallToolWithRetry(ctx, actionID, input, opts, retry)
	if err != nil {
		return ActionResult{}, err
	}
	return parseActionResult(callResult)
}

func parseActionResult(callResult *sdkmcp.CallToolResult) (ActionResult, error) {
	root, err := structuredMap(callResult)
	if err != nil {
		return ActionResult{}, err
	}
	out := ActionResult{}
	if runID, ok := root["runId"].(string); ok {
		out.RunID = runID
	}
	if output, ok := root["output"].(map[string]any); ok {
		out.Output = output
	}
	if diff, ok := root["diff"].(map[string]any); ok {
		out.Diff = diff
	}
	return out, nil
}

func (c *Client) WorkflowRun(ctx context.Context, def WorkflowDefinition, opts CallOptions) (WorkflowRunSnapshot, error) {
	args := workflowRunArgs(def, opts)
	callResult, err := c.callTool(ctx, "workflow.run", args, CallOptions{})
	if err != nil {
		return WorkflowRunSnapshot{}, err
	}
	return decodeStructured[WorkflowRunSnapshot](callResult)
}

func (c *Client) WorkflowRunWithRetry(ctx context.Context, def WorkflowDefinition, opts CallOptions, retry RetryOptions) (WorkflowRunSnapshot, error) {
	args := workflowRunArgs(def, opts)
	callResult, err := c.CallToolWithRetry(ctx, "workflow.run", args, CallOptions{}, retry)
	if err != nil {
		return WorkflowRunSnapshot{}, err
	}
	return decodeStructured[WorkflowRunSnapshot](callResult)
}

func (c *Client) WorkflowStatus(ctx context.Context, runID string) (WorkflowRunSnapshot, error) {
	callResult, err := c.callTool(ctx, "workflow.status", map[string]any{"runId": runID}, CallOptions{})
	if err != nil {
		return WorkflowRunSnapshot{}, err
	}
	return decodeStructured[WorkflowRunSnapshot](callResult)
}

func (c *Client) WorkflowCancel(ctx context.Context, runID string) (WorkflowCancelResult, error) {
	callResult, err := c.callTool(ctx, "workflow.cancel", map[string]any{"runId": runID}, CallOptions{})
	if err != nil {
		return WorkflowCancelResult{}, err
	}
	return decodeStructured[WorkflowCancelResult](callResult)
}

func (c *Client) WorkflowList(ctx context.Context, req WorkflowListRequest) (WorkflowListResult, error) {
	args := map[string]any{}
	if req.Status != "" {
		args["status"] = req.Status
	}
	if req.Limit > 0 {
		args["limit"] = req.Limit
	}
	if req.Cursor != "" {
		args["cursor"] = req.Cursor
	}
	callResult, err := c.callTool(ctx, "workflow.list", args, CallOptions{})
	if err != nil {
		return WorkflowListResult{}, err
	}
	return decodeStructured[WorkflowListResult](callResult)
}

func (c *Client) WorkflowListAll(ctx context.Context, req WorkflowListRequest) ([]WorkflowRunSnapshot, error) {
	pageReq := req
	pageReq.Cursor = req.Cursor
	var out []WorkflowRunSnapshot
	seenCursor := map[string]struct{}{}
	for {
		page, err := c.WorkflowList(ctx, pageReq)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Runs...)
		if page.NextCursor == "" {
			return out, nil
		}
		if _, ok := seenCursor[page.NextCursor]; ok {
			return nil, fmt.Errorf("workflow.list repeated nextCursor %q", page.NextCursor)
		}
		seenCursor[page.NextCursor] = struct{}{}
		pageReq.Cursor = page.NextCursor
	}
}

func (c *Client) WorkflowLogs(ctx context.Context, req WorkflowLogsRequest) (WorkflowLogsResult, error) {
	args := map[string]any{
		"runId": req.RunID,
	}
	if req.Limit > 0 {
		args["limit"] = req.Limit
	}
	if req.Level != "" {
		args["level"] = req.Level
	}
	if req.StepID != "" {
		args["stepId"] = req.StepID
	}
	if req.Since != "" {
		args["since"] = req.Since
	}
	callResult, err := c.callTool(ctx, "workflow.logs", args, CallOptions{})
	if err != nil {
		return WorkflowLogsResult{}, err
	}
	return decodeStructured[WorkflowLogsResult](callResult)
}

func workflowRunArgs(def WorkflowDefinition, opts CallOptions) map[string]any {
	args := map[string]any{
		"definition": map[string]any{
			"id":    def.ID,
			"steps": make([]any, 0, len(def.Steps)),
		},
	}
	defMap := args["definition"].(map[string]any)
	steps := defMap["steps"].([]any)
	for _, step := range def.Steps {
		steps = append(steps, map[string]any{
			"id":        step.ID,
			"actionId":  step.ActionID,
			"input":     step.Input,
			"timeoutMs": step.TimeoutMS,
			"retry": map[string]any{
				"maxAttempts": step.Retry.MaxAttempts,
				"backoffMs":   step.Retry.BackoffMS,
			},
		})
	}
	defMap["steps"] = steps
	if opts.Actor != "" {
		args["actor"] = opts.Actor
	}
	if opts.CorrelationID != "" {
		args["correlationId"] = opts.CorrelationID
	}
	if opts.DryRun {
		args["dryRun"] = true
	}
	return args
}

func (c *Client) ListResources(ctx context.Context) ([]*sdkmcp.Resource, error) {
	return c.ListResourcesAll(ctx)
}

func (c *Client) ListResourcesAll(ctx context.Context) ([]*sdkmcp.Resource, error) {
	cursor := ""
	var out []*sdkmcp.Resource
	seenCursor := map[string]struct{}{}
	for {
		resources, nextCursor, err := c.ListResourcesPage(ctx, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, resources...)
		if nextCursor == "" {
			return out, nil
		}
		if _, ok := seenCursor[nextCursor]; ok {
			return nil, fmt.Errorf("resources/list repeated nextCursor %q", nextCursor)
		}
		seenCursor[nextCursor] = struct{}{}
		cursor = nextCursor
	}
}

func (c *Client) ListResourcesPage(ctx context.Context, cursor string) ([]*sdkmcp.Resource, string, error) {
	params := &sdkmcp.ListResourcesParams{Cursor: cursor}
	resp, err := c.session.ListResources(ctx, params)
	if err != nil {
		return nil, "", toCallError(err)
	}
	return resp.Resources, resp.NextCursor, nil
}

func (c *Client) ReadResource(ctx context.Context, uri string) (string, error) {
	resp, err := c.session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		return "", toCallError(err)
	}
	if len(resp.Contents) == 0 {
		return "", nil
	}
	return resp.Contents[0].Text, nil
}

func (c *Client) CallToolWithRetry(
	ctx context.Context,
	name string,
	args map[string]any,
	opts CallOptions,
	retry RetryOptions,
) (*sdkmcp.CallToolResult, error) {
	policy := retry.normalize()
	backoff := policy.InitialBackoff
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		result, err := c.callTool(ctx, name, args, opts)
		if err == nil {
			return result, nil
		}
		if attempt >= policy.MaxAttempts || !IsRetryable(err) {
			return nil, err
		}
		if err := sleepWithContext(ctx, backoff); err != nil {
			return nil, err
		}
		backoff = nextBackoff(backoff, policy.MaxBackoff)
	}
	return nil, fmt.Errorf("unreachable retry loop for tool %q", name)
}

func (c *Client) callTool(ctx context.Context, name string, args map[string]any, opts CallOptions) (*sdkmcp.CallToolResult, error) {
	params := &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	}
	if opts.Actor != "" || opts.CorrelationID != "" || opts.DryRun {
		meta := sdkmcp.Meta{}
		if opts.Actor != "" {
			meta["actor"] = opts.Actor
		}
		if opts.CorrelationID != "" {
			meta["correlationId"] = opts.CorrelationID
		}
		if opts.DryRun {
			meta["dryRun"] = true
		}
		params.Meta = meta
	}
	result, err := c.session.CallTool(ctx, params)
	if err != nil {
		return nil, toCallError(err)
	}
	return result, nil
}

func (o RetryOptions) normalize() RetryOptions {
	out := o
	if out.MaxAttempts <= 0 {
		out.MaxAttempts = defaultRetryMaxAttempts
	}
	if out.InitialBackoff <= 0 {
		out.InitialBackoff = defaultRetryInitialBackoff
	}
	if out.MaxBackoff <= 0 {
		out.MaxBackoff = defaultRetryMaxBackoff
	}
	if out.MaxBackoff < out.InitialBackoff {
		out.MaxBackoff = out.InitialBackoff
	}
	return out
}

func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var callErr *CallError
	if !errors.As(err, &callErr) {
		return false
	}

	if callErr.Data != nil {
		if retryable, ok := asBool(callErr.Data["retryable"]); ok {
			return retryable
		}
	}
	return callErr.Code == jsonrpc.CodeInternalError
}

func asBool(v any) (bool, bool) {
	switch typed := v.(type) {
	case bool:
		return typed, true
	case string:
		parsed, err := strconv.ParseBool(typed)
		if err != nil {
			return false, false
		}
		return parsed, true
	default:
		return false, false
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextBackoff(current, max time.Duration) time.Duration {
	if current >= max {
		return max
	}
	next := current * 2
	if next < 0 || next > max {
		return max
	}
	return next
}

func toCallError(err error) error {
	var wireErr *jsonrpc.Error
	if errors.As(err, &wireErr) {
		out := &CallError{
			Code:    wireErr.Code,
			Message: wireErr.Message,
			Cause:   err,
		}
		if len(wireErr.Data) > 0 {
			var data map[string]any
			if unmarshalErr := json.Unmarshal(wireErr.Data, &data); unmarshalErr == nil {
				out.Data = data
			}
		}
		return out
	}
	return err
}

func structuredMap(result *sdkmcp.CallToolResult) (map[string]any, error) {
	if result == nil {
		return nil, errors.New("nil tool result")
	}
	root, ok := result.StructuredContent.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected structured content type %T", result.StructuredContent)
	}
	return root, nil
}

func decodeStructured[T any](result *sdkmcp.CallToolResult) (T, error) {
	var out T
	root, err := structuredMap(result)
	if err != nil {
		return out, err
	}
	raw, err := json.Marshal(root)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}
