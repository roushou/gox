package mcp

import (
	"time"

	"github.com/roushou/gox/internal/usecase"
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

type workflowRunOptions = usecase.WorkflowRunOptions
type workflowListOptions = usecase.WorkflowListOptions
type workflowLogOptions = usecase.WorkflowLogOptions
type workflowRunSnapshot = usecase.WorkflowRunSnapshot
type workflowLogEntry = usecase.WorkflowLogEntry

func mapWorkflowRuntimeConfig(cfg WorkflowRuntimeConfig) usecase.WorkflowRuntimeConfig {
	normalized := normalizeWorkflowRuntimeConfig(cfg)
	return usecase.WorkflowRuntimeConfig{
		MaxConcurrentRuns:  normalized.MaxConcurrentRuns,
		MaxSteps:           normalized.MaxSteps,
		MaxLogsPerRun:      normalized.MaxLogsPerRun,
		DefaultStepTimeout: normalized.DefaultStepTimeout,
		MaxStepTimeout:     normalized.MaxStepTimeout,
		MaxAttempts:        normalized.MaxAttempts,
		MaxBackoff:         normalized.MaxBackoff,
		RetentionMaxRuns:   normalized.RetentionMaxRuns,
		RetentionTTL:       normalized.RetentionTTL,
	}
}
