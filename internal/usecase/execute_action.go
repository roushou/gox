package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
	"github.com/roushou/gox/internal/domain/policy"
	"github.com/roushou/gox/internal/domain/runlog"
)

type ExecuteActionCommand struct {
	ActionID      string
	Actor         string
	CorrelationID string
	DryRun        bool
	Approved      bool
	Input         map[string]any
}

type ExecuteActionResult struct {
	RunID  string
	Result action.Result
}

type IDGenerator func() string

type ExecuteActionService struct {
	registry   action.Registry
	policy     policy.Evaluator
	runlogs    runlog.Store
	logger     *slog.Logger
	timeout    time.Duration
	newID      IDGenerator
	timeNowUTC func() time.Time
}

func NewExecuteActionService(
	registry action.Registry,
	policyEvaluator policy.Evaluator,
	runlogStore runlog.Store,
	logger *slog.Logger,
	timeout time.Duration,
	idGenerator IDGenerator,
) *ExecuteActionService {
	if logger == nil {
		logger = slog.Default()
	}
	if idGenerator == nil {
		idGenerator = defaultID
	}
	return &ExecuteActionService{
		registry: registry,
		policy:   policyEvaluator,
		runlogs:  runlogStore,
		logger:   logger,
		timeout:  timeout,
		newID:    idGenerator,
		timeNowUTC: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func defaultID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func (s *ExecuteActionService) Execute(ctx context.Context, cmd ExecuteActionCommand) (ExecuteActionResult, error) {
	start := s.timeNowUTC()

	if cmd.ActionID == "" {
		return ExecuteActionResult{}, fault.Validation("action id is required")
	}
	if cmd.CorrelationID == "" {
		cmd.CorrelationID = s.newID()
	}

	handler, found := s.registry.Get(cmd.ActionID)
	if !found {
		return ExecuteActionResult{}, fault.NotFound(fmt.Sprintf("action %q not found", cmd.ActionID)).WithCorrelationID(cmd.CorrelationID)
	}

	spec := handler.Spec()
	if err := action.ValidateInput(spec.InputSchema, cmd.Input); err != nil {
		if validationErr, ok := action.AsValidationError(err); ok {
			return ExecuteActionResult{}, fault.Validation(validationErr.Error()).
				WithDetails(map[string]any{
					"validation_path": validationErr.Path,
					"validation_rule": validationErr.Rule,
				}).
				WithCorrelationID(cmd.CorrelationID)
		}
		if schemaErr, ok := action.AsSchemaError(err); ok {
			return ExecuteActionResult{}, fault.Internal(schemaErr.Error()).WithCorrelationID(cmd.CorrelationID)
		}
		return ExecuteActionResult{}, fault.Validation(err.Error()).WithCorrelationID(cmd.CorrelationID)
	}

	if cmd.DryRun {
		if dryRunAware, ok := handler.(action.DryRunAware); ok && !dryRunAware.SupportsDryRun() {
			return ExecuteActionResult{}, fault.Validation(
				fmt.Sprintf("action %q does not support dry-run", spec.ID),
			).WithCorrelationID(cmd.CorrelationID)
		}
	}

	if validator, ok := handler.(action.PreconditionValidator); ok {
		if err := validator.Validate(ctx, action.Request{
			CorrelationID: cmd.CorrelationID,
			Actor:         cmd.Actor,
			DryRun:        cmd.DryRun,
			Input:         cmd.Input,
		}); err != nil {
			f := mapPreconditionError(err, cmd.CorrelationID)
			return ExecuteActionResult{}, f
		}
	}

	decision := s.policy.Evaluate(ctx, policy.DecisionInput{
		Actor:         cmd.Actor,
		ActionID:      spec.ID,
		SideEffect:    spec.SideEffectLevel,
		CorrelationID: cmd.CorrelationID,
		DryRun:        cmd.DryRun,
	})
	if !decision.Allowed {
		err := fault.Denied(decision.Reason)
		return ExecuteActionResult{}, err.WithCorrelationID(cmd.CorrelationID)
	}
	if decision.RequiresApproval && !cmd.Approved {
		reason := decision.Reason
		if strings.TrimSpace(reason) == "" {
			reason = "action requires explicit approval"
		}
		err := fault.Denied(reason)
		return ExecuteActionResult{}, err.WithCorrelationID(cmd.CorrelationID)
	}

	runID := s.newID()
	runningRecord := runlog.Record{
		RunID:         runID,
		CorrelationID: cmd.CorrelationID,
		Type:          runlog.RunTypeAction,
		Name:          spec.ID,
		Status:        runlog.StatusRunning,
		StartedAt:     start,
	}
	if err := s.runlogs.Upsert(ctx, runningRecord); err != nil {
		return ExecuteActionResult{}, mapRunlogPersistError(
			err,
			"runlog.upsert_start",
			cmd.CorrelationID,
			"failed to persist run start",
		)
	}

	actionCtx := ctx
	cancel := func() {}
	if s.timeout > 0 {
		actionCtx, cancel = context.WithTimeout(ctx, s.timeout)
	}
	defer cancel()

	result, execErr := func() (res action.Result, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fault.Internal(fmt.Sprintf("action panicked: %v", recovered))
			}
		}()
		return handler.Handle(actionCtx, action.Request{
			RunID:         runID,
			CorrelationID: cmd.CorrelationID,
			Actor:         cmd.Actor,
			DryRun:        cmd.DryRun,
			Input:         cmd.Input,
		})
	}()

	end := s.timeNowUTC()
	duration := end.Sub(start)
	logFields := actionObservabilityFields(spec.ID, cmd.Input)

	if execErr != nil {
		f, ok := fault.As(execErr)
		if !ok {
			f = fault.Internal("action execution failed")
		}
		f = f.WithCorrelationID(cmd.CorrelationID)

		if err := s.runlogs.Upsert(ctx, runlog.Record{
			RunID:         runID,
			CorrelationID: cmd.CorrelationID,
			Type:          runlog.RunTypeAction,
			Name:          spec.ID,
			Status:        runlog.StatusFailed,
			StartedAt:     start,
			EndedAt:       end,
			ErrorCode:     string(f.Code),
			ErrorMessage:  f.Message,
		}); err != nil {
			s.logger.Error(
				"failed to persist failed action runlog",
				"run_id", runID,
				"correlation_id", cmd.CorrelationID,
				"action_id", spec.ID,
				"error", err,
			)
		}
		errorFields := []any{
			"run_id", runID,
			"correlation_id", cmd.CorrelationID,
			"action_id", spec.ID,
			"error_code", f.Code,
			"error", f.Message,
		}
		errorFields = append(errorFields, logFields...)
		s.logger.Error("action execution failed",
			errorFields...,
		)
		return ExecuteActionResult{}, f
	}

	diffSummary := ""
	if result.Diff != nil {
		diffSummary = result.Diff.Summary
	}
	if err := s.runlogs.Upsert(ctx, runlog.Record{
		RunID:         runID,
		CorrelationID: cmd.CorrelationID,
		Type:          runlog.RunTypeAction,
		Name:          spec.ID,
		Status:        runlog.StatusSucceeded,
		StartedAt:     start,
		EndedAt:       end,
		DiffSummary:   diffSummary,
	}); err != nil {
		s.logger.Error(
			"failed to persist succeeded action runlog",
			"run_id", runID,
			"correlation_id", cmd.CorrelationID,
			"action_id", spec.ID,
			"error", err,
		)
	}

	successFields := []any{
		"run_id", runID,
		"correlation_id", cmd.CorrelationID,
		"action_id", spec.ID,
		"duration_ms", duration.Milliseconds(),
	}
	successFields = append(successFields, logFields...)
	s.logger.Info("action execution succeeded", successFields...)

	return ExecuteActionResult{
		RunID:  runID,
		Result: result,
	}, nil
}

func actionObservabilityFields(actionID string, input map[string]any) []any {
	if actionID != "roblox.script_execute" {
		return nil
	}
	fields := make([]any, 0, 6)
	if scriptPath, ok := input["scriptPath"].(string); ok && strings.TrimSpace(scriptPath) != "" {
		fields = append(fields, "script_path", scriptPath)
	}
	if scriptID, ok := input["scriptId"].(string); ok && strings.TrimSpace(scriptID) != "" {
		fields = append(fields, "script_id", scriptID)
	}
	if functionName, ok := input["functionName"].(string); ok && strings.TrimSpace(functionName) != "" {
		fields = append(fields, "function_name", functionName)
	}
	return fields
}

func mapPreconditionError(err error, correlationID string) error {
	if validationErr, ok := action.AsValidationError(err); ok {
		return fault.Validation(validationErr.Error()).
			WithDetails(map[string]any{
				"validation_path": validationErr.Path,
				"validation_rule": validationErr.Rule,
			}).
			WithCorrelationID(correlationID)
	}

	if f, ok := fault.As(err); ok {
		return f.WithCorrelationID(correlationID)
	}

	return fault.Validation(err.Error()).WithCorrelationID(correlationID)
}
