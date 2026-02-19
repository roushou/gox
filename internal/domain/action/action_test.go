package action

import (
	"context"
	"testing"
)

type testHandler struct {
	spec Spec
}

func (h testHandler) Spec() Spec { return h.spec }

func (h testHandler) Handle(_ context.Context, _ Request) (Result, error) {
	return Result{}, nil
}

func TestRegistryRegisterAndList(t *testing.T) {
	registry := NewInMemoryRegistry()
	err := registry.Register(testHandler{
		spec: Spec{
			ID:              "core.a",
			Name:            "A",
			Version:         "v1",
			SideEffectLevel: SideEffectNone,
		},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	err = registry.Register(testHandler{
		spec: Spec{
			ID:              "core.b",
			Name:            "B",
			Version:         "v1",
			SideEffectLevel: SideEffectRead,
		},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	specs := registry.List()
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}
	if specs[0].ID != "core.a" || specs[1].ID != "core.b" {
		t.Fatalf("expected sorted ids, got %#v", specs)
	}
}

func TestRegistryRejectsDuplicate(t *testing.T) {
	registry := NewInMemoryRegistry()
	handler := testHandler{
		spec: Spec{
			ID:              "core.ping",
			Name:            "Ping",
			Version:         "v1",
			SideEffectLevel: SideEffectNone,
		},
	}
	if err := registry.Register(handler); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.Register(handler); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}
