package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/fault"
	"github.com/roushou/gox/internal/domain/runlog"
	"github.com/roushou/gox/internal/domain/workflow"
)

const (
	defaultWorkflowMaxConcurrentRuns = 8
	defaultWorkflowMaxSteps          = 64
	defaultWorkflowMaxLogsPerRun     = 256
	defaultWorkflowStepTimeout       = 30 * time.Second
	defaultWorkflowMaxStepTimeout    = 5 * time.Minute
	defaultWorkflowMaxAttempts       = 5
	defaultWorkflowMaxBackoff        = 30 * time.Second
	defaultWorkflowRetentionMaxRuns  = 1000
	defaultWorkflowRetentionTTL      = 24 * time.Hour
)

type WorkflowRuntimeConfig struct {
	MaxConcurrentRuns  int
	MaxSteps           int
	MaxLogsPerRun      int
	DefaultStepTimeout time.Duration
	MaxStepTimeout     time.Duration
	MaxAttempts        int
	MaxBackoff         time.Duration
	RetentionMaxRuns   int
	RetentionTTL       time.Duration
}

type WorkflowActionCatalog interface {
	Get(id string) (action.Handler, bool)
}

type WorkflowActionExecutor interface {
	Execute(ctx context.Context, cmd ExecuteActionCommand) (ExecuteActionResult, error)
}

type WorkflowRuntime interface {
	Start(def workflow.Definition, opts WorkflowRunOptions) (WorkflowRunSnapshot, error)
	Status(runID string) (WorkflowRunSnapshot, error)
	List(opts WorkflowListOptions) ([]WorkflowRunSnapshot, string, error)
	Logs(runID string, opts WorkflowLogOptions) ([]WorkflowLogEntry, error)
	Cancel(runID string) (WorkflowRunSnapshot, bool, error)
	Close(ctx context.Context) error
}

func defaultWorkflowRuntimeConfig() WorkflowRuntimeConfig {
	return WorkflowRuntimeConfig{
		MaxConcurrentRuns:  defaultWorkflowMaxConcurrentRuns,
		MaxSteps:           defaultWorkflowMaxSteps,
		MaxLogsPerRun:      defaultWorkflowMaxLogsPerRun,
		DefaultStepTimeout: defaultWorkflowStepTimeout,
		MaxStepTimeout:     defaultWorkflowMaxStepTimeout,
		MaxAttempts:        defaultWorkflowMaxAttempts,
		MaxBackoff:         defaultWorkflowMaxBackoff,
		RetentionMaxRuns:   defaultWorkflowRetentionMaxRuns,
		RetentionTTL:       defaultWorkflowRetentionTTL,
	}
}

func normalizeWorkflowRuntimeConfig(cfg WorkflowRuntimeConfig) WorkflowRuntimeConfig {
	out := defaultWorkflowRuntimeConfig()
	if cfg.MaxConcurrentRuns > 0 {
		out.MaxConcurrentRuns = cfg.MaxConcurrentRuns
	}
	if cfg.MaxSteps > 0 {
		out.MaxSteps = cfg.MaxSteps
	}
	if cfg.MaxLogsPerRun > 0 {
		out.MaxLogsPerRun = cfg.MaxLogsPerRun
	}
	if cfg.DefaultStepTimeout > 0 {
		out.DefaultStepTimeout = cfg.DefaultStepTimeout
	}
	if cfg.MaxStepTimeout > 0 {
		out.MaxStepTimeout = cfg.MaxStepTimeout
	}
	if cfg.MaxAttempts > 0 {
		out.MaxAttempts = cfg.MaxAttempts
	}
	if cfg.MaxBackoff > 0 {
		out.MaxBackoff = cfg.MaxBackoff
	}
	if cfg.RetentionMaxRuns > 0 {
		out.RetentionMaxRuns = cfg.RetentionMaxRuns
	}
	if cfg.RetentionTTL > 0 {
		out.RetentionTTL = cfg.RetentionTTL
	}
	return out
}

type WorkflowRunOptions struct {
	Actor         string
	CorrelationID string
	DryRun        bool
	Approved      bool
}

type WorkflowListOptions struct {
	Status string
	Limit  int
	Cursor string
}

type WorkflowLogOptions struct {
	Limit  int
	Level  string
	StepID string
	Since  time.Time
}

type WorkflowRunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
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

type WorkflowLogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	StepID    string `json:"stepId,omitempty"`
	ActionID  string `json:"actionId,omitempty"`
}

type workflowRunState struct {
	RunID           string
	WorkflowID      string
	CorrelationID   string
	Actor           string
	DryRun          bool
	Approved        bool
	Status          runlog.Status
	StartedAt       time.Time
	EndedAt         time.Time
	TotalSteps      int
	CompletedSteps  int
	CancelRequested bool
	LastError       *WorkflowRunError
	cancel          context.CancelFunc
}

type normalizedWorkflowDefinition struct {
	ID    string
	Steps []normalizedWorkflowStep
}

type normalizedWorkflowStep struct {
	ID          string
	ActionID    string
	Input       map[string]any
	Timeout     time.Duration
	MaxAttempts int
	Backoff     time.Duration
	Condition   *normalizedWorkflowCondition
}

type normalizedWorkflowCondition struct {
	StepID string
	Path   string
	Equals any
}

type workflowRuntimeEngine struct {
	logger   *slog.Logger
	catalog  WorkflowActionCatalog
	executor WorkflowActionExecutor
	runlogs  runlog.Store
	opts     WorkflowRuntimeConfig
	idSeq    atomic.Uint64

	mu   sync.RWMutex
	runs map[string]*workflowRunState
	logs map[string][]WorkflowLogEntry
	wg   sync.WaitGroup
}

func NewWorkflowRuntime(
	logger *slog.Logger,
	catalog WorkflowActionCatalog,
	executor WorkflowActionExecutor,
	runlogs runlog.Store,
	cfg WorkflowRuntimeConfig,
) WorkflowRuntime {
	if logger == nil {
		logger = slog.Default()
	}
	return &workflowRuntimeEngine{
		logger:   logger,
		catalog:  catalog,
		executor: executor,
		runlogs:  runlogs,
		opts:     normalizeWorkflowRuntimeConfig(cfg),
		runs:     make(map[string]*workflowRunState),
		logs:     make(map[string][]WorkflowLogEntry),
	}
}

func (m *workflowRuntimeEngine) Start(def workflow.Definition, opts WorkflowRunOptions) (WorkflowRunSnapshot, error) {
	if m.executor == nil || m.catalog == nil {
		return WorkflowRunSnapshot{}, fault.New(
			fault.CodeUnimplemented,
			fault.CategoryRuntime,
			"workflow tools require action catalog and executor",
		)
	}
	normalized, err := m.normalizeDefinition(def)
	if err != nil {
		return WorkflowRunSnapshot{}, err
	}

	correlationID := strings.TrimSpace(opts.CorrelationID)
	if correlationID == "" {
		correlationID = m.nextID("corr")
	}

	runID := m.nextID("wf")
	state := &workflowRunState{
		RunID:          runID,
		WorkflowID:     normalized.ID,
		CorrelationID:  correlationID,
		Actor:          opts.Actor,
		DryRun:         opts.DryRun,
		Approved:       opts.Approved,
		Status:         runlog.StatusRunning,
		StartedAt:      time.Now().UTC(),
		TotalSteps:     len(normalized.Steps),
		CompletedSteps: 0,
	}
	runCtx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel

	m.mu.Lock()
	m.pruneLocked(time.Now().UTC())
	if m.runningCountLocked() >= m.opts.MaxConcurrentRuns {
		m.mu.Unlock()
		return WorkflowRunSnapshot{}, fault.Denied(
			fmt.Sprintf("workflow concurrency limit reached (max %d)", m.opts.MaxConcurrentRuns),
		)
	}
	m.runs[runID] = state
	m.appendLogLocked(runID, "info", "workflow started", "", "")
	record := recordFromState(state)
	m.mu.Unlock()
	m.persistRecord(record)

	m.logger.Info(
		"workflow started",
		"workflow_run_id", runID,
		"workflow_id", state.WorkflowID,
		"correlation_id", state.CorrelationID,
		"steps", state.TotalSteps,
	)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.execute(runCtx, state.RunID, normalized)
	}()
	return m.snapshot(runID)
}

func (m *workflowRuntimeEngine) Status(runID string) (WorkflowRunSnapshot, error) {
	m.prune()
	return m.snapshot(runID)
}

func (m *workflowRuntimeEngine) List(opts WorkflowListOptions) ([]WorkflowRunSnapshot, string, error) {
	m.prune()
	filter := strings.ToLower(strings.TrimSpace(opts.Status))
	dedup := make(map[string]WorkflowRunSnapshot)

	if m.runlogs != nil {
		records, err := m.listRunlogRecords(context.Background())
		if err != nil {
			return nil, "", mapRunlogReadError(err, "workflow.list")
		}
		for _, record := range records {
			if record.Type != runlog.RunTypeWorkflow {
				continue
			}
			snapshot := snapshotFromRecord(record)
			dedup[snapshot.RunID] = snapshot
		}
	}

	m.mu.RLock()
	for runID, state := range m.runs {
		dedup[runID] = snapshotFromState(state)
	}
	m.mu.RUnlock()

	items := make([]WorkflowRunSnapshot, 0, len(dedup))
	for _, snapshot := range dedup {
		if filter != "" && strings.ToLower(snapshot.Status) != filter {
			continue
		}
		items = append(items, snapshot)
	}
	sort.Slice(items, func(i, j int) bool {
		ti := parseSnapshotTime(items[i].StartedAt)
		tj := parseSnapshotTime(items[j].StartedAt)
		return ti.After(tj)
	})
	start, err := parseListCursor(opts.Cursor)
	if err != nil {
		return nil, "", fault.Validation(err.Error())
	}
	if start > len(items) {
		start = len(items)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	page := items[start:end]
	nextCursor := ""
	if end < len(items) {
		nextCursor = strconv.Itoa(end)
	}
	return page, nextCursor, nil
}

func (m *workflowRuntimeEngine) Logs(runID string, opts WorkflowLogOptions) ([]WorkflowLogEntry, error) {
	m.prune()
	m.mu.RLock()
	logs, ok := m.logs[runID]
	if ok {
		out := append([]WorkflowLogEntry(nil), logs...)
		m.mu.RUnlock()
		return applyWorkflowLogFilters(out, opts), nil
	}
	m.mu.RUnlock()

	if m.runlogs != nil {
		record, exists, err := m.getRunlogRecord(context.Background(), runID)
		if err != nil {
			return nil, mapRunlogReadError(err, "workflow.logs")
		}
		if exists && record.Type == runlog.RunTypeWorkflow {
			out := []WorkflowLogEntry{
				{
					Timestamp: record.StartedAt.UTC().Format(time.RFC3339Nano),
					Level:     "info",
					Message:   fmt.Sprintf("workflow %s", record.Status),
				},
			}
			if !record.EndedAt.IsZero() {
				out = append(out, WorkflowLogEntry{
					Timestamp: record.EndedAt.UTC().Format(time.RFC3339Nano),
					Level:     "info",
					Message:   "workflow finished",
				})
			}
			return applyWorkflowLogFilters(out, opts), nil
		}
	}
	return nil, fault.NotFound(fmt.Sprintf("workflow run %q not found", runID))
}

func (m *workflowRuntimeEngine) Cancel(runID string) (WorkflowRunSnapshot, bool, error) {
	m.prune()
	m.mu.Lock()
	state, ok := m.runs[runID]
	if !ok {
		m.mu.Unlock()
		if m.runlogs != nil {
			record, exists, err := m.getRunlogRecord(context.Background(), runID)
			if err != nil {
				return WorkflowRunSnapshot{}, false, mapRunlogReadError(err, "workflow.cancel")
			}
			if exists && record.Type == runlog.RunTypeWorkflow {
				return snapshotFromRecord(record), false, nil
			}
		}
		return WorkflowRunSnapshot{}, false, fault.NotFound(fmt.Sprintf("workflow run %q not found", runID))
	}

	acknowledged := false
	var cancel context.CancelFunc
	if state.Status == runlog.StatusRunning {
		state.CancelRequested = true
		acknowledged = true
		cancel = state.cancel
		m.appendLogLocked(runID, "warn", "workflow cancel requested", "", "")
	}
	snapshot := snapshotFromState(state)
	record := recordFromState(state)
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.persistRecord(record)
	return snapshot, acknowledged, nil
}

func (m *workflowRuntimeEngine) Close(ctx context.Context) error {
	m.mu.RLock()
	for _, state := range m.runs {
		if state.Status == runlog.StatusRunning && state.cancel != nil {
			state.cancel()
		}
	}
	m.mu.RUnlock()

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *workflowRuntimeEngine) execute(ctx context.Context, runID string, def normalizedWorkflowDefinition) {
	stepOutputs := make(map[string]any, len(def.Steps))
	for idx, step := range def.Steps {
		if ctx.Err() != nil {
			m.finishCanceled(runID)
			return
		}
		if !m.shouldRunStep(step, stepOutputs) {
			m.markStepSkipped(runID, idx+1, step)
			continue
		}

		m.addStepLog(runID, "info", fmt.Sprintf("step started (attempt 1/%d)", step.MaxAttempts), step.ID, step.ActionID)
		result, err := m.executeStep(ctx, runID, step)
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				m.finishCanceled(runID)
				return
			}
			m.finishFailed(runID, err)
			return
		}
		stepOutputs[step.ID] = result.Output
		m.markStepComplete(runID, idx+1, step)
	}
	m.finishSucceeded(runID)
}

func (m *workflowRuntimeEngine) executeStep(ctx context.Context, runID string, step normalizedWorkflowStep) (action.Result, error) {
	var lastErr error
	var lastResult action.Result
	for attempt := 1; attempt <= step.MaxAttempts; attempt++ {
		if attempt > 1 {
			m.addStepLog(
				runID,
				"info",
				fmt.Sprintf("retry attempt %d/%d", attempt, step.MaxAttempts),
				step.ID,
				step.ActionID,
			)
		}

		attemptCtx := ctx
		cancel := func() {}
		if step.Timeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, step.Timeout)
		}
		result, err := m.executor.Execute(attemptCtx, ExecuteActionCommand{
			ActionID:      step.ActionID,
			Actor:         m.actor(runID),
			CorrelationID: m.correlationID(runID),
			DryRun:        m.dryRun(runID),
			Approved:      m.approved(runID),
			Input:         step.Input,
		})
		cancel()

		if err == nil {
			return result.Result, nil
		}
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return action.Result{}, ctx.Err()
		}

		lastResult = result.Result
		lastErr = err
		shouldRetry := m.shouldRetry(err) && attempt < step.MaxAttempts
		if shouldRetry {
			m.addStepLog(
				runID,
				"warn",
				fmt.Sprintf("step attempt failed, retrying: %v", err),
				step.ID,
				step.ActionID,
			)
			if step.Backoff > 0 {
				timer := time.NewTimer(step.Backoff)
				select {
				case <-ctx.Done():
					timer.Stop()
					return action.Result{}, ctx.Err()
				case <-timer.C:
				}
			}
			continue
		}

		m.addStepLog(
			runID,
			"error",
			fmt.Sprintf("step failed: %v", err),
			step.ID,
			step.ActionID,
		)
		break
	}
	return lastResult, lastErr
}

func (m *workflowRuntimeEngine) shouldRetry(err error) bool {
	if f, ok := fault.As(err); ok {
		switch f.Code {
		case fault.CodeValidation, fault.CodeNotFound, fault.CodeDenied:
			return false
		}
		if f.Retryable {
			return true
		}
		return f.Code == fault.CodeInternal
	}
	return true
}

func (m *workflowRuntimeEngine) actor(runID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.runs[runID]
	if !ok {
		return ""
	}
	return state.Actor
}

func (m *workflowRuntimeEngine) correlationID(runID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.runs[runID]
	if !ok {
		return ""
	}
	return state.CorrelationID
}

func (m *workflowRuntimeEngine) dryRun(runID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.runs[runID]
	if !ok {
		return false
	}
	return state.DryRun
}

func (m *workflowRuntimeEngine) approved(runID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.runs[runID]
	if !ok {
		return false
	}
	return state.Approved
}

func (m *workflowRuntimeEngine) addStepLog(runID string, level string, message string, stepID string, actionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendLogLocked(runID, level, message, stepID, actionID)
}

func (m *workflowRuntimeEngine) markStepComplete(runID string, completed int, step normalizedWorkflowStep) {
	m.mu.Lock()
	state, ok := m.runs[runID]
	if !ok || state.Status != runlog.StatusRunning {
		m.mu.Unlock()
		return
	}
	state.CompletedSteps = completed
	m.appendLogLocked(runID, "info", "step completed", step.ID, step.ActionID)
	record := recordFromState(state)
	m.mu.Unlock()
	m.persistRecord(record)
}

func (m *workflowRuntimeEngine) markStepSkipped(runID string, completed int, step normalizedWorkflowStep) {
	m.mu.Lock()
	state, ok := m.runs[runID]
	if !ok || state.Status != runlog.StatusRunning {
		m.mu.Unlock()
		return
	}
	state.CompletedSteps = completed
	m.appendLogLocked(runID, "info", "step skipped (condition not met)", step.ID, step.ActionID)
	record := recordFromState(state)
	m.mu.Unlock()
	m.persistRecord(record)
}

func (m *workflowRuntimeEngine) finishSucceeded(runID string) {
	m.mu.Lock()
	state, ok := m.runs[runID]
	if !ok || state.Status != runlog.StatusRunning {
		m.mu.Unlock()
		return
	}
	state.Status = runlog.StatusSucceeded
	state.EndedAt = time.Now().UTC()
	state.cancel = nil
	m.appendLogLocked(runID, "info", "workflow succeeded", "", "")
	record := recordFromState(state)
	m.mu.Unlock()
	m.persistRecord(record)

	m.logger.Info(
		"workflow succeeded",
		"workflow_run_id", state.RunID,
		"workflow_id", state.WorkflowID,
		"correlation_id", state.CorrelationID,
		"steps", state.TotalSteps,
	)
}

func (m *workflowRuntimeEngine) finishCanceled(runID string) {
	m.mu.Lock()
	state, ok := m.runs[runID]
	if !ok || state.Status != runlog.StatusRunning {
		m.mu.Unlock()
		return
	}
	state.Status = runlog.StatusCanceled
	state.EndedAt = time.Now().UTC()
	state.cancel = nil
	m.appendLogLocked(runID, "warn", "workflow canceled", "", "")
	record := recordFromState(state)
	m.mu.Unlock()
	m.persistRecord(record)

	m.logger.Info(
		"workflow canceled",
		"workflow_run_id", state.RunID,
		"workflow_id", state.WorkflowID,
		"correlation_id", state.CorrelationID,
		"completed_steps", state.CompletedSteps,
		"steps", state.TotalSteps,
	)
}

func (m *workflowRuntimeEngine) finishFailed(runID string, err error) {
	m.mu.Lock()
	state, ok := m.runs[runID]
	if !ok || state.Status != runlog.StatusRunning {
		m.mu.Unlock()
		return
	}
	state.Status = runlog.StatusFailed
	state.EndedAt = time.Now().UTC()
	state.cancel = nil
	state.LastError = workflowErrorFromError(err)
	m.appendLogLocked(runID, "error", "workflow failed", "", "")
	record := recordFromState(state)
	m.mu.Unlock()
	m.persistRecord(record)

	m.logger.Error(
		"workflow failed",
		"workflow_run_id", state.RunID,
		"workflow_id", state.WorkflowID,
		"correlation_id", state.CorrelationID,
		"error", state.LastError,
	)
}

func (m *workflowRuntimeEngine) snapshot(runID string) (WorkflowRunSnapshot, error) {
	m.mu.RLock()
	state, ok := m.runs[runID]
	if ok {
		snapshot := snapshotFromState(state)
		m.mu.RUnlock()
		return snapshot, nil
	}
	m.mu.RUnlock()

	if m.runlogs != nil {
		record, exists, err := m.getRunlogRecord(context.Background(), runID)
		if err != nil {
			return WorkflowRunSnapshot{}, mapRunlogReadError(err, "workflow.status")
		}
		if exists && record.Type == runlog.RunTypeWorkflow {
			return snapshotFromRecord(record), nil
		}
	}
	return WorkflowRunSnapshot{}, fault.NotFound(fmt.Sprintf("workflow run %q not found", runID))
}

func (m *workflowRuntimeEngine) runningCountLocked() int {
	count := 0
	for _, state := range m.runs {
		if state.Status == runlog.StatusRunning {
			count++
		}
	}
	return count
}

func (m *workflowRuntimeEngine) appendLogLocked(runID string, level string, message string, stepID string, actionID string) {
	entry := WorkflowLogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level,
		Message:   message,
		StepID:    stepID,
		ActionID:  actionID,
	}
	logs := append(m.logs[runID], entry)
	if m.opts.MaxLogsPerRun > 0 && len(logs) > m.opts.MaxLogsPerRun {
		logs = logs[len(logs)-m.opts.MaxLogsPerRun:]
	}
	m.logs[runID] = logs
}

func (m *workflowRuntimeEngine) persistRecord(record runlog.Record) {
	if m.runlogs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.runlogs.Upsert(ctx, record); err != nil {
		m.logger.Error(
			"persist workflow runlog failed",
			"run_id", record.RunID,
			"error", mapRunlogPersistError(err, "workflow.persist", record.CorrelationID, "failed to persist workflow runlog"),
		)
	}
}

func (m *workflowRuntimeEngine) getRunlogRecord(ctx context.Context, runID string) (runlog.Record, bool, error) {
	if m.runlogs == nil {
		return runlog.Record{}, false, nil
	}
	if aware, ok := m.runlogs.(runlog.ErrorAwareStore); ok {
		return aware.GetWithError(ctx, runID)
	}
	record, exists := m.runlogs.Get(ctx, runID)
	return record, exists, nil
}

func (m *workflowRuntimeEngine) listRunlogRecords(ctx context.Context) ([]runlog.Record, error) {
	if m.runlogs == nil {
		return nil, nil
	}
	if aware, ok := m.runlogs.(runlog.ErrorAwareStore); ok {
		return aware.ListWithError(ctx)
	}
	return m.runlogs.List(ctx), nil
}

func (m *workflowRuntimeEngine) nextID(prefix string) string {
	n := m.idSeq.Add(1)
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UTC().UnixNano(), n)
}

func (m *workflowRuntimeEngine) prune() {
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(now)
}

func (m *workflowRuntimeEngine) pruneLocked(now time.Time) {
	if m.opts.RetentionMaxRuns <= 0 && m.opts.RetentionTTL <= 0 {
		return
	}

	type candidate struct {
		runID  string
		ended  time.Time
		remove bool
	}
	completed := make([]candidate, 0, len(m.runs))
	for runID, state := range m.runs {
		if state.Status == runlog.StatusRunning {
			continue
		}
		endedAt := state.EndedAt
		if endedAt.IsZero() {
			endedAt = state.StartedAt
		}
		removeByTTL := m.opts.RetentionTTL > 0 && now.Sub(endedAt) > m.opts.RetentionTTL
		completed = append(completed, candidate{
			runID:  runID,
			ended:  endedAt,
			remove: removeByTTL,
		})
	}

	for _, item := range completed {
		if item.remove {
			delete(m.runs, item.runID)
			delete(m.logs, item.runID)
		}
	}

	if m.opts.RetentionMaxRuns <= 0 {
		return
	}
	remaining := make([]candidate, 0, len(completed))
	for _, item := range completed {
		if _, ok := m.runs[item.runID]; ok {
			remaining = append(remaining, item)
		}
	}
	if len(remaining) <= m.opts.RetentionMaxRuns {
		return
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i].ended.Before(remaining[j].ended)
	})
	excess := len(remaining) - m.opts.RetentionMaxRuns
	for i := 0; i < excess; i++ {
		delete(m.runs, remaining[i].runID)
		delete(m.logs, remaining[i].runID)
	}
}

func (m *workflowRuntimeEngine) normalizeDefinition(def workflow.Definition) (normalizedWorkflowDefinition, error) {
	workflowID := strings.TrimSpace(def.ID)
	if workflowID == "" {
		return normalizedWorkflowDefinition{}, fault.Validation("workflow id is required")
	}
	if len(def.Steps) == 0 {
		return normalizedWorkflowDefinition{}, fault.Validation("workflow must include at least one step")
	}
	if len(def.Steps) > m.opts.MaxSteps {
		return normalizedWorkflowDefinition{}, fault.Validation(
			fmt.Sprintf("workflow has %d steps, max allowed is %d", len(def.Steps), m.opts.MaxSteps),
		)
	}

	normalized := normalizedWorkflowDefinition{
		ID:    workflowID,
		Steps: make([]normalizedWorkflowStep, 0, len(def.Steps)),
	}
	seenStepIDs := make(map[string]struct{}, len(def.Steps))
	for idx, step := range def.Steps {
		stepID := strings.TrimSpace(step.ID)
		if stepID == "" {
			stepID = fmt.Sprintf("step-%d", idx+1)
		}
		if _, exists := seenStepIDs[stepID]; exists {
			return normalizedWorkflowDefinition{}, fault.Validation(fmt.Sprintf("duplicate step id %q", stepID))
		}
		actionID := strings.TrimSpace(step.ActionID)
		if actionID == "" {
			return normalizedWorkflowDefinition{}, fault.Validation(fmt.Sprintf("step %q actionId is required", stepID))
		}
		if _, ok := m.catalog.Get(actionID); !ok {
			return normalizedWorkflowDefinition{}, fault.NotFound(fmt.Sprintf("action %q not found for step %q", actionID, stepID))
		}
		timeout, err := m.normalizeStepTimeout(step.TimeoutMS, stepID)
		if err != nil {
			return normalizedWorkflowDefinition{}, err
		}
		attempts, backoff, err := m.normalizeStepRetry(step.Retry, stepID)
		if err != nil {
			return normalizedWorkflowDefinition{}, err
		}
		condition, err := m.normalizeStepCondition(step.When, stepID, seenStepIDs)
		if err != nil {
			return normalizedWorkflowDefinition{}, err
		}

		input := step.Input
		if input == nil {
			input = map[string]any{}
		}
		normalized.Steps = append(normalized.Steps, normalizedWorkflowStep{
			ID:          stepID,
			ActionID:    actionID,
			Input:       input,
			Timeout:     timeout,
			MaxAttempts: attempts,
			Backoff:     backoff,
			Condition:   condition,
		})
		seenStepIDs[stepID] = struct{}{}
	}
	return normalized, nil
}

func (m *workflowRuntimeEngine) normalizeStepTimeout(timeoutMS int, stepID string) (time.Duration, error) {
	if timeoutMS < 0 {
		return 0, fault.Validation(fmt.Sprintf("step %q timeoutMs must be >= 0", stepID))
	}
	timeout := m.opts.DefaultStepTimeout
	if timeoutMS > 0 {
		timeout = time.Duration(timeoutMS) * time.Millisecond
	}
	if timeout > m.opts.MaxStepTimeout {
		return 0, fault.Validation(
			fmt.Sprintf(
				"step %q timeout exceeds max (%dms > %dms)",
				stepID,
				timeout.Milliseconds(),
				m.opts.MaxStepTimeout.Milliseconds(),
			),
		)
	}
	return timeout, nil
}

func (m *workflowRuntimeEngine) normalizeStepRetry(retry workflow.RetryPolicy, stepID string) (int, time.Duration, error) {
	attempts := retry.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	if attempts > m.opts.MaxAttempts {
		return 0, 0, fault.Validation(
			fmt.Sprintf("step %q maxAttempts exceeds max (%d > %d)", stepID, attempts, m.opts.MaxAttempts),
		)
	}
	if retry.BackoffMS < 0 {
		return 0, 0, fault.Validation(fmt.Sprintf("step %q backoffMs must be >= 0", stepID))
	}
	backoff := time.Duration(retry.BackoffMS) * time.Millisecond
	if backoff > m.opts.MaxBackoff {
		return 0, 0, fault.Validation(
			fmt.Sprintf(
				"step %q backoff exceeds max (%dms > %dms)",
				stepID,
				backoff.Milliseconds(),
				m.opts.MaxBackoff.Milliseconds(),
			),
		)
	}
	return attempts, backoff, nil
}

func (m *workflowRuntimeEngine) normalizeStepCondition(
	condition *workflow.StepCondition,
	stepID string,
	seenStepIDs map[string]struct{},
) (*normalizedWorkflowCondition, error) {
	if condition == nil {
		return nil, nil
	}
	refStepID := strings.TrimSpace(condition.StepID)
	if refStepID == "" {
		return nil, fault.Validation(fmt.Sprintf("step %q when.stepId is required", stepID))
	}
	if _, ok := seenStepIDs[refStepID]; !ok {
		return nil, fault.Validation(
			fmt.Sprintf("step %q when.stepId must reference a previous step, got %q", stepID, refStepID),
		)
	}
	return &normalizedWorkflowCondition{
		StepID: refStepID,
		Path:   strings.TrimSpace(condition.Path),
		Equals: condition.Equals,
	}, nil
}

func (m *workflowRuntimeEngine) shouldRunStep(step normalizedWorkflowStep, stepOutputs map[string]any) bool {
	if step.Condition == nil {
		return true
	}
	source, ok := stepOutputs[step.Condition.StepID]
	if !ok {
		return false
	}
	actual, ok := resolveConditionPath(source, step.Condition.Path)
	if !ok {
		return false
	}
	return conditionValuesEqual(actual, step.Condition.Equals)
}

func resolveConditionPath(source any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return source, true
	}
	current := source
	for _, part := range strings.Split(path, ".") {
		key := strings.TrimSpace(part)
		if key == "" {
			return nil, false
		}
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, exists := obj[key]
		if !exists {
			return nil, false
		}
		current = value
	}
	return current, true
}

func conditionValuesEqual(actual any, expected any) bool {
	encodedActual, errActual := json.Marshal(actual)
	encodedExpected, errExpected := json.Marshal(expected)
	if errActual != nil || errExpected != nil {
		return false
	}
	return bytes.Equal(encodedActual, encodedExpected)
}

func snapshotFromState(state *workflowRunState) WorkflowRunSnapshot {
	out := WorkflowRunSnapshot{
		RunID:           state.RunID,
		WorkflowID:      state.WorkflowID,
		CorrelationID:   state.CorrelationID,
		Status:          string(state.Status),
		StartedAt:       state.StartedAt.UTC().Format(time.RFC3339Nano),
		TotalSteps:      state.TotalSteps,
		CompletedSteps:  state.CompletedSteps,
		CancelRequested: state.CancelRequested,
		LastError:       state.LastError,
	}
	if !state.EndedAt.IsZero() {
		out.EndedAt = state.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func snapshotFromRecord(record runlog.Record) WorkflowRunSnapshot {
	completed, total := parseProgressSummary(record.DiffSummary)
	out := WorkflowRunSnapshot{
		RunID:           record.RunID,
		WorkflowID:      record.Name,
		CorrelationID:   record.CorrelationID,
		Status:          string(record.Status),
		StartedAt:       record.StartedAt.UTC().Format(time.RFC3339Nano),
		TotalSteps:      total,
		CompletedSteps:  completed,
		CancelRequested: record.Status == runlog.StatusCanceled,
	}
	if !record.EndedAt.IsZero() {
		out.EndedAt = record.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	if record.ErrorCode != "" || record.ErrorMessage != "" {
		out.LastError = &WorkflowRunError{
			Code:    record.ErrorCode,
			Message: record.ErrorMessage,
		}
	}
	return out
}

func recordFromState(state *workflowRunState) runlog.Record {
	record := runlog.Record{
		RunID:         state.RunID,
		CorrelationID: state.CorrelationID,
		Type:          runlog.RunTypeWorkflow,
		Name:          state.WorkflowID,
		Status:        state.Status,
		StartedAt:     state.StartedAt,
		EndedAt:       state.EndedAt,
		DiffSummary:   progressSummary(state.CompletedSteps, state.TotalSteps),
	}
	if state.LastError != nil {
		record.ErrorCode = state.LastError.Code
		record.ErrorMessage = state.LastError.Message
	}
	return record
}

func progressSummary(completed int, total int) string {
	return fmt.Sprintf("steps=%d/%d", completed, total)
}

func parseProgressSummary(raw string) (int, int) {
	const prefix = "steps="
	if !strings.HasPrefix(raw, prefix) {
		return 0, 0
	}
	parts := strings.SplitN(strings.TrimPrefix(raw, prefix), "/", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	completed, err1 := strconv.Atoi(parts[0])
	total, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return completed, total
}

func parseSnapshotTime(raw string) time.Time {
	out, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return out
}

func parseListCursor(cursor string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid workflow list cursor %q", cursor)
	}
	return offset, nil
}

func applyWorkflowLogFilters(logs []WorkflowLogEntry, opts WorkflowLogOptions) []WorkflowLogEntry {
	level := strings.ToLower(strings.TrimSpace(opts.Level))
	stepID := strings.TrimSpace(opts.StepID)
	filtered := make([]WorkflowLogEntry, 0, len(logs))
	for _, entry := range logs {
		if level != "" && strings.ToLower(entry.Level) != level {
			continue
		}
		if stepID != "" && entry.StepID != stepID {
			continue
		}
		if !opts.Since.IsZero() {
			ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
			if err != nil || ts.Before(opts.Since) {
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	if opts.Limit <= 0 || len(filtered) <= opts.Limit {
		return filtered
	}
	return filtered[len(filtered)-opts.Limit:]
}

func workflowErrorFromError(err error) *WorkflowRunError {
	if f, ok := fault.As(err); ok {
		return &WorkflowRunError{
			Code:    string(f.Code),
			Message: f.Message,
		}
	}
	return &WorkflowRunError{
		Code:    string(fault.CodeInternal),
		Message: err.Error(),
	}
}
