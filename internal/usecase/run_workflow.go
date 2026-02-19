package usecase

import (
	"context"

	"github.com/roushou/gox/internal/domain/fault"
	"github.com/roushou/gox/internal/domain/workflow"
)

type RunWorkflowService struct {
	runtime WorkflowRuntime
}

func NewRunWorkflowService(runtime WorkflowRuntime) *RunWorkflowService {
	return &RunWorkflowService{runtime: runtime}
}

func (s *RunWorkflowService) Start(def workflow.Definition, opts WorkflowRunOptions) (WorkflowRunSnapshot, error) {
	if s.runtime == nil {
		return WorkflowRunSnapshot{}, fault.New(
			fault.CodeUnimplemented,
			fault.CategoryRuntime,
			"workflow runtime is not configured",
		)
	}
	return s.runtime.Start(def, opts)
}

func (s *RunWorkflowService) Run(ctx context.Context, def workflow.Definition) (workflow.Result, error) {
	_ = ctx
	snapshot, err := s.Start(def, WorkflowRunOptions{})
	if err != nil {
		return workflow.Result{}, err
	}
	return workflow.Result{
		WorkflowID: snapshot.WorkflowID,
		Succeeded:  snapshot.Status == "succeeded",
	}, nil
}
