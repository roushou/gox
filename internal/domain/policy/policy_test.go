package policy

import (
	"context"
	"testing"

	"github.com/roushou/gox/internal/domain/action"
)

func TestRuleEvaluatorAllowAllModeWithoutRules(t *testing.T) {
	evaluator, err := NewRuleEvaluator(RuleSet{Mode: ModeAllowAll})
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}
	decision := evaluator.Evaluate(context.Background(), DecisionInput{
		Actor:      "dev",
		ActionID:   "roblox.script_update",
		SideEffect: action.SideEffectWrite,
	})
	if !decision.Allowed {
		t.Fatalf("expected allowed decision, got %#v", decision)
	}
}

func TestRuleEvaluatorDenyByDefault(t *testing.T) {
	evaluator, err := NewRuleEvaluator(RuleSet{
		Mode:              ModeDenyByDefault,
		DefaultDenyReason: "explicit allow required",
	})
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}
	decision := evaluator.Evaluate(context.Background(), DecisionInput{
		Actor:      "dev",
		ActionID:   "roblox.script_update",
		SideEffect: action.SideEffectWrite,
	})
	if decision.Allowed {
		t.Fatalf("expected denied decision, got %#v", decision)
	}
	if decision.Reason != "explicit allow required" {
		t.Fatalf("unexpected reason: %#v", decision)
	}
}

func TestRuleEvaluatorPriority(t *testing.T) {
	evaluator, err := NewRuleEvaluator(RuleSet{
		Mode: ModeDenyByDefault,
		Rules: []Rule{
			{
				Name:         "allow all writes",
				ActionPrefix: "roblox.",
				SideEffects:  []action.SideEffectLevel{action.SideEffectWrite},
				Effect:       EffectAllow,
				Priority:     1,
			},
			{
				Name:     "deny delete",
				ActionID: "roblox.instance_delete",
				Effect:   EffectDeny,
				Reason:   "delete blocked",
				Priority: 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}
	decision := evaluator.Evaluate(context.Background(), DecisionInput{
		Actor:      "dev",
		ActionID:   "roblox.instance_delete",
		SideEffect: action.SideEffectWrite,
	})
	if decision.Allowed {
		t.Fatalf("expected denied decision, got %#v", decision)
	}
	if decision.Reason != "delete blocked" {
		t.Fatalf("unexpected reason: %#v", decision)
	}
}

func TestRuleEvaluatorDryRunMatch(t *testing.T) {
	dryRunTrue := true
	evaluator, err := NewRuleEvaluator(RuleSet{
		Mode: ModeDenyByDefault,
		Rules: []Rule{
			{
				Name:         "allow dry-run only",
				ActionPrefix: "roblox.",
				DryRun:       &dryRunTrue,
				Effect:       EffectAllow,
			},
		},
	})
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}

	allowed := evaluator.Evaluate(context.Background(), DecisionInput{
		Actor:      "dev",
		ActionID:   "roblox.script_update",
		SideEffect: action.SideEffectWrite,
		DryRun:     true,
	})
	if !allowed.Allowed {
		t.Fatalf("expected dry-run allowed, got %#v", allowed)
	}

	denied := evaluator.Evaluate(context.Background(), DecisionInput{
		Actor:      "dev",
		ActionID:   "roblox.script_update",
		SideEffect: action.SideEffectWrite,
		DryRun:     false,
	})
	if denied.Allowed {
		t.Fatalf("expected non-dry-run denied, got %#v", denied)
	}
}

func TestRuleEvaluatorRequiresApproval(t *testing.T) {
	evaluator, err := NewRuleEvaluator(RuleSet{
		Mode: ModeDenyByDefault,
		Rules: []Rule{
			{
				Name:             "allow script update with approval",
				ActionID:         "roblox.script_update",
				Effect:           EffectAllow,
				RequiresApproval: true,
				Reason:           "script updates require explicit approval",
			},
		},
	})
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}

	decision := evaluator.Evaluate(context.Background(), DecisionInput{
		Actor:      "dev",
		ActionID:   "roblox.script_update",
		SideEffect: action.SideEffectWrite,
	})
	if !decision.Allowed {
		t.Fatalf("expected allowed decision, got %#v", decision)
	}
	if !decision.RequiresApproval {
		t.Fatalf("expected requires approval decision, got %#v", decision)
	}
	if decision.Reason != "script updates require explicit approval" {
		t.Fatalf("unexpected reason: %#v", decision)
	}
}
