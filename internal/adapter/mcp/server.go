package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
	"github.com/roushou/gox/internal/domain/runlog"
	"github.com/roushou/gox/internal/usecase"
)

const serverVersion = "0.1.0"

const (
	errorCodeUnimplemented int64 = -32041
	errorCodeDenied        int64 = -32043
	errorCodeNotFound      int64 = -32044
)

type ActionExecutor interface {
	Execute(ctx context.Context, cmd usecase.ExecuteActionCommand) (usecase.ExecuteActionResult, error)
}

type ActionCatalog interface {
	List() []action.Spec
	Get(id string) (action.Handler, bool)
}

type Server struct {
	reader   io.Reader
	writer   io.Writer
	logger   *slog.Logger
	executor ActionExecutor
	catalog  ActionCatalog
	workflow usecase.WorkflowRuntime
	runlogs  runlog.Store
	guards   *requestGuards
	server   *sdkmcp.Server
}

func NewServer(name string, reader io.Reader, writer io.Writer, logger *slog.Logger, catalog ActionCatalog, executor ActionExecutor) *Server {
	return NewServerWithRunlog(name, reader, writer, logger, catalog, executor, runlog.NewInMemoryStore())
}

func NewServerWithRunlog(
	name string,
	reader io.Reader,
	writer io.Writer,
	logger *slog.Logger,
	catalog ActionCatalog,
	executor ActionExecutor,
	runlogs runlog.Store,
) *Server {
	return NewServerWithRuntimeConfig(
		name,
		reader,
		writer,
		logger,
		catalog,
		executor,
		runlogs,
		defaultWorkflowRuntimeConfig(),
		defaultServerRuntimeConfig(),
	)
}

func NewServerWithRunlogAndWorkflowConfig(
	name string,
	reader io.Reader,
	writer io.Writer,
	logger *slog.Logger,
	catalog ActionCatalog,
	executor ActionExecutor,
	runlogs runlog.Store,
	workflowCfg WorkflowRuntimeConfig,
) *Server {
	return NewServerWithRuntimeConfig(
		name,
		reader,
		writer,
		logger,
		catalog,
		executor,
		runlogs,
		workflowCfg,
		defaultServerRuntimeConfig(),
	)
}

func NewServerWithRuntimeConfig(
	name string,
	reader io.Reader,
	writer io.Writer,
	logger *slog.Logger,
	catalog ActionCatalog,
	executor ActionExecutor,
	runlogs runlog.Store,
	workflowCfg WorkflowRuntimeConfig,
	serverCfg ServerRuntimeConfig,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if runlogs == nil {
		runlogs = runlog.NewInMemoryStore()
	}
	s := &Server{
		reader:   reader,
		writer:   writer,
		logger:   logger,
		executor: executor,
		catalog:  catalog,
		workflow: usecase.NewWorkflowRuntime(logger, catalog, executor, runlogs, mapWorkflowRuntimeConfig(workflowCfg)),
		runlogs:  runlogs,
		guards:   newRequestGuards(serverCfg),
		server: sdkmcp.NewServer(&sdkmcp.Implementation{
			Name:    name,
			Version: serverVersion,
		}, &sdkmcp.ServerOptions{
			Logger: logger,
		}),
	}
	s.registerTools()
	return s
}

func (s *Server) WorkflowRuntime() usecase.WorkflowRuntime {
	return s.workflow
}

func (s *Server) Serve(ctx context.Context) error {
	reader := s.reader
	if reader == nil {
		reader = os.Stdin
	}
	writer := s.writer
	if writer == nil {
		writer = os.Stdout
	}
	transport := &sdkmcp.IOTransport{
		Reader: io.NopCloser(reader),
		Writer: nopWriteCloser{Writer: writer},
	}
	return s.server.Run(ctx, transport)
}

func (s *Server) registerTools() {
	s.registerActionTools()
	s.registerWorkflowTools()
	s.registerResources()
}

func (s *Server) registerActionTools() {
	if s.catalog == nil {
		return
	}
	for _, spec := range s.catalog.List() {
		spec := spec
		supportsDryRun, requiresBridge := s.toolFlags(spec.ID)
		var outputSchema any
		if spec.OutputSchema != nil {
			outputSchema = spec.OutputSchema
		}
		tool := &sdkmcp.Tool{
			Name:         spec.ID,
			Title:        spec.Name,
			Description:  spec.Description,
			InputSchema:  schemaOrDefault(spec.InputSchema),
			OutputSchema: outputSchema,
			Annotations: &sdkmcp.ToolAnnotations{
				ReadOnlyHint: spec.SideEffectLevel != action.SideEffectWrite,
			},
			Meta: sdkmcp.Meta{
				"gox:version":         spec.Version,
				"gox:sideEffectLevel": string(spec.SideEffectLevel),
				"gox:supportsDryRun":  supportsDryRun,
				"gox:requiresBridge":  requiresBridge,
			},
		}
		s.server.AddTool(tool, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return s.executeAction(ctx, spec.ID, req)
		})
	}
}

func (s *Server) toolFlags(actionID string) (supportsDryRun bool, requiresBridge bool) {
	supportsDryRun = true
	if s.catalog == nil {
		return supportsDryRun, false
	}
	handler, ok := s.catalog.Get(actionID)
	if !ok {
		return supportsDryRun, false
	}
	if dryRunAware, ok := handler.(action.DryRunAware); ok {
		supportsDryRun = dryRunAware.SupportsDryRun()
	}
	if bridgeBound, ok := handler.(action.BridgeBound); ok {
		requiresBridge = bridgeBound.RequiresBridge()
	}
	return supportsDryRun, requiresBridge
}

func (s *Server) executeAction(ctx context.Context, actionID string, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	if err := s.guards.checkAndConsume(req.Params.Arguments); err != nil {
		return nil, err
	}
	input := map[string]any{}
	if len(req.Params.Arguments) > 0 {
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return nil, wireError(
				jsonrpc.CodeInvalidParams,
				"invalid tool arguments",
				map[string]any{"details": err.Error()},
			)
		}
	}

	cmd := usecase.ExecuteActionCommand{
		ActionID: actionID,
		Input:    input,
	}
	if req.Params.Meta != nil {
		if actor, ok := req.Params.Meta["actor"].(string); ok {
			cmd.Actor = actor
		}
		if correlationID, ok := req.Params.Meta["correlationId"].(string); ok {
			cmd.CorrelationID = correlationID
		}
		if dryRun, ok := asBool(req.Params.Meta["dryRun"]); ok {
			cmd.DryRun = dryRun
		}
		if approved, ok := asBool(req.Params.Meta["approved"]); ok {
			cmd.Approved = approved
		}
	}

	result, err := s.executor.Execute(ctx, cmd)
	if err != nil {
		return nil, s.toCallToolError(err)
	}

	output := map[string]any{
		"runId":  result.RunID,
		"output": result.Result.Output,
		"diff":   result.Result.Diff,
	}
	return s.toolResultWithStructured(output)
}

func (s *Server) toolResultWithStructured(output any) (*sdkmcp.CallToolResult, error) {
	rawOutput, err := json.Marshal(output)
	if err != nil {
		return nil, wireError(
			jsonrpc.CodeInternalError,
			"failed to encode tool output",
			map[string]any{"details": err.Error()},
		)
	}

	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: string(rawOutput)},
		},
		StructuredContent: output,
	}, nil
}

func (s *Server) toCallToolError(err error) error {
	if f, ok := fault.As(err); ok {
		data := map[string]any{
			"code":           f.Code,
			"category":       f.Category,
			"retryable":      f.Retryable,
			"correlation_id": f.CorrelationID,
			"details":        f.Details,
		}
		if f.Code == fault.CodeValidation && f.Details != nil {
			if path, ok := f.Details["validation_path"]; ok {
				data["validation_path"] = path
			}
			if rule, ok := f.Details["validation_rule"]; ok {
				data["validation_rule"] = rule
			}
		}
		return wireError(mapFaultCode(f.Code), f.Message, data)
	}
	return wireError(
		jsonrpc.CodeInternalError,
		"internal error",
		map[string]any{"details": err.Error()},
	)
}

func mapFaultCode(code fault.Code) int64 {
	switch code {
	case fault.CodeValidation:
		return jsonrpc.CodeInvalidParams
	case fault.CodeNotFound:
		return errorCodeNotFound
	case fault.CodeDenied:
		return errorCodeDenied
	case fault.CodeUnimplemented:
		return errorCodeUnimplemented
	default:
		return jsonrpc.CodeInternalError
	}
}

func wireError(code int64, message string, data any) *jsonrpc.Error {
	out := &jsonrpc.Error{
		Code:    code,
		Message: message,
	}
	if data == nil {
		return out
	}
	raw, err := json.Marshal(data)
	if err != nil {
		raw = []byte(fmt.Sprintf(`{"details":%q}`, err.Error()))
	}
	out.Data = json.RawMessage(raw)
	return out
}

func schemaOrDefault(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object"}
	}
	return schema
}

func asBool(v any) (bool, bool) {
	switch value := v.(type) {
	case bool:
		return value, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return false, false
		}
		return parsed, true
	default:
		return false, false
	}
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}
