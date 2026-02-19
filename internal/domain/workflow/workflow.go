package workflow

import "context"

type Step struct {
	ID        string
	ActionID  string
	Input     map[string]any
	TimeoutMS int
	Retry     RetryPolicy
	When      *StepCondition
}

type Definition struct {
	ID    string
	Steps []Step
}

type RetryPolicy struct {
	MaxAttempts int
	BackoffMS   int
}

type StepCondition struct {
	StepID string
	Path   string
	Equals any
}

type Result struct {
	WorkflowID string
	Succeeded  bool
}

type Engine interface {
	Run(ctx context.Context, def Definition) (Result, error)
}
