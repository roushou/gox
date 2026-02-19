package runlog

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Status string
type RunType string

const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"

	RunTypeAction   RunType = "action"
	RunTypeWorkflow RunType = "workflow"
)

type Record struct {
	RunID         string
	CorrelationID string
	Type          RunType
	Name          string
	Status        Status
	StartedAt     time.Time
	EndedAt       time.Time
	ErrorCode     string
	ErrorMessage  string
	DiffSummary   string
}

type Store interface {
	Upsert(ctx context.Context, record Record) error
	Get(ctx context.Context, runID string) (Record, bool)
	List(ctx context.Context) []Record
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}

type ErrorAwareStore interface {
	Store
	GetWithError(ctx context.Context, runID string) (Record, bool, error)
	ListWithError(ctx context.Context) ([]Record, error)
}

type InMemoryStore struct {
	mu      sync.RWMutex
	records map[string]Record
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		records: make(map[string]Record),
	}
}

func (s *InMemoryStore) Upsert(_ context.Context, record Record) error {
	if record.RunID == "" {
		return errors.New("run id cannot be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.RunID] = record
	return nil
}

func (s *InMemoryStore) Get(_ context.Context, runID string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[runID]
	return record, ok
}

func (s *InMemoryStore) GetWithError(ctx context.Context, runID string) (Record, bool, error) {
	record, ok := s.Get(ctx, runID)
	return record, ok, nil
}

func (s *InMemoryStore) List(_ context.Context) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Record, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, record)
	}
	return out
}

func (s *InMemoryStore) ListWithError(ctx context.Context) ([]Record, error) {
	return s.List(ctx), nil
}

func (s *InMemoryStore) DeleteOlderThan(_ context.Context, cutoff time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	for id, record := range s.records {
		if record.Status == StatusRunning {
			continue
		}
		ts := record.EndedAt
		if ts.IsZero() {
			ts = record.StartedAt
		}
		if ts.Before(cutoff) {
			delete(s.records, id)
			deleted++
		}
	}
	return deleted, nil
}
