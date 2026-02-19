package action

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

type SideEffectLevel string

const (
	SideEffectNone  SideEffectLevel = "none"
	SideEffectRead  SideEffectLevel = "read"
	SideEffectWrite SideEffectLevel = "write"
)

type Spec struct {
	ID              string
	Name            string
	Version         string
	Description     string
	SideEffectLevel SideEffectLevel
	InputSchema     map[string]any
	OutputSchema    map[string]any
}

type Request struct {
	RunID         string
	CorrelationID string
	Actor         string
	DryRun        bool
	Input         map[string]any
}

type Diff struct {
	Summary string
	Entries []string
}

type Result struct {
	Output map[string]any
	Diff   *Diff
}

type Handler interface {
	Spec() Spec
	Handle(ctx context.Context, request Request) (Result, error)
}

// PreconditionValidator can be optionally implemented by handlers that need
// request-level validation beyond schema checks before execution starts.
type PreconditionValidator interface {
	Validate(ctx context.Context, request Request) error
}

// DryRunAware can be optionally implemented to explicitly signal dry-run
// support. Handlers that do not implement this are assumed to support dry-run.
type DryRunAware interface {
	SupportsDryRun() bool
}

// Compensable can be optionally implemented by mutating actions that provide a
// best-effort compensation hook for workflow rollback handling.
type Compensable interface {
	Compensate(ctx context.Context, request Request) error
}

// BridgeBound can be optionally implemented by actions that require a Roblox
// bridge connection.
type BridgeBound interface {
	RequiresBridge() bool
}

type Registry interface {
	Register(handler Handler) error
	Get(id string) (Handler, bool)
	List() []Spec
}

type InMemoryRegistry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func NewInMemoryRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{
		handlers: make(map[string]Handler),
	}
}

func (r *InMemoryRegistry) Register(handler Handler) error {
	spec := handler.Spec()
	if spec.ID == "" {
		return errors.New("action id is required")
	}
	if spec.Version == "" {
		return fmt.Errorf("action %q version is required", spec.ID)
	}
	if spec.SideEffectLevel == "" {
		return fmt.Errorf("action %q side effect level is required", spec.ID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[spec.ID]; exists {
		return fmt.Errorf("action %q already registered", spec.ID)
	}
	r.handlers[spec.ID] = handler
	return nil
}

func (r *InMemoryRegistry) Get(id string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, ok := r.handlers[id]
	return handler, ok
}

func (r *InMemoryRegistry) List() []Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	specs := make([]Spec, 0, len(r.handlers))
	for _, handler := range r.handlers {
		specs = append(specs, handler.Spec())
	}
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].ID < specs[j].ID
	})
	return specs
}
