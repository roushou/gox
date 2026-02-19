package runlog

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryStoreDeleteOlderThan(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryStore()
	now := time.Now().UTC()

	old := Record{
		RunID:     "old-1",
		Type:      RunTypeAction,
		Name:      "core.ping",
		Status:    StatusSucceeded,
		StartedAt: now.Add(-48 * time.Hour),
		EndedAt:   now.Add(-47 * time.Hour),
	}
	recent := Record{
		RunID:     "recent-1",
		Type:      RunTypeAction,
		Name:      "core.ping",
		Status:    StatusSucceeded,
		StartedAt: now.Add(-1 * time.Hour),
		EndedAt:   now.Add(-30 * time.Minute),
	}
	running := Record{
		RunID:     "running-1",
		Type:      RunTypeWorkflow,
		Name:      "deploy",
		Status:    StatusRunning,
		StartedAt: now.Add(-72 * time.Hour),
	}
	oldNoEndedAt := Record{
		RunID:     "old-no-end",
		Type:      RunTypeAction,
		Name:      "core.ping",
		Status:    StatusFailed,
		StartedAt: now.Add(-50 * time.Hour),
	}

	for _, r := range []Record{old, recent, running, oldNoEndedAt} {
		if err := store.Upsert(ctx, r); err != nil {
			t.Fatalf("upsert %s: %v", r.RunID, err)
		}
	}

	cutoff := now.Add(-24 * time.Hour)
	deleted, err := store.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("delete older than: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}

	if _, ok := store.Get(ctx, "old-1"); ok {
		t.Fatal("old record should have been deleted")
	}
	if _, ok := store.Get(ctx, "old-no-end"); ok {
		t.Fatal("old record without EndedAt should have been deleted")
	}
	if _, ok := store.Get(ctx, "recent-1"); !ok {
		t.Fatal("recent record should still exist")
	}
	if _, ok := store.Get(ctx, "running-1"); !ok {
		t.Fatal("running record should never be deleted")
	}

	remaining := store.List(ctx)
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining records, got %d", len(remaining))
	}
}
