package policy

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/roushou/gox/internal/domain/action"
)

type DecisionInput struct {
	Actor         string
	ActionID      string
	SideEffect    action.SideEffectLevel
	CorrelationID string
	DryRun        bool
}

type Decision struct {
	Allowed          bool
	Reason           string
	RequiresApproval bool
}

type Evaluator interface {
	Evaluate(ctx context.Context, input DecisionInput) Decision
}

type Mode string

const (
	ModeAllowAll      Mode = "allow-all"
	ModeDenyByDefault Mode = "deny-by-default"
)

type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

type Rule struct {
	Name         string
	Actor        string
	ActorPrefix  string
	ActionID     string
	ActionPrefix string
	SideEffects  []action.SideEffectLevel
	DryRun       *bool
	// RequiresApproval marks an otherwise-allowed rule as approval-gated.
	RequiresApproval bool
	Effect           Effect
	Reason           string
	Priority         int
}

type RuleSet struct {
	Mode              Mode
	DefaultDenyReason string
	Rules             []Rule
}

type AllowAllEvaluator struct{}

func (AllowAllEvaluator) Evaluate(_ context.Context, _ DecisionInput) Decision {
	return Decision{Allowed: true}
}

type ruleEvaluator struct {
	mode              Mode
	defaultDenyReason string
	rules             []Rule
}

func NewRuleEvaluator(set RuleSet) (Evaluator, error) {
	mode := set.Mode
	if mode == "" {
		mode = ModeAllowAll
	}
	switch mode {
	case ModeAllowAll, ModeDenyByDefault:
	default:
		return nil, fmt.Errorf("unsupported policy mode %q", mode)
	}

	rules := append([]Rule(nil), set.Rules...)
	for i := range rules {
		if err := validateRule(rules[i]); err != nil {
			return nil, fmt.Errorf("invalid policy rule %d (%q): %w", i, rules[i].Name, err)
		}
	}
	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Priority > rules[j].Priority
	})

	defaultReason := strings.TrimSpace(set.DefaultDenyReason)
	if defaultReason == "" {
		defaultReason = "blocked by policy"
	}
	return ruleEvaluator{
		mode:              mode,
		defaultDenyReason: defaultReason,
		rules:             rules,
	}, nil
}

func (e ruleEvaluator) Evaluate(_ context.Context, input DecisionInput) Decision {
	for _, rule := range e.rules {
		if matchesRule(rule, input) {
			switch rule.Effect {
			case EffectAllow:
				return Decision{
					Allowed:          true,
					Reason:           strings.TrimSpace(rule.Reason),
					RequiresApproval: rule.RequiresApproval,
				}
			default:
				reason := strings.TrimSpace(rule.Reason)
				if reason == "" {
					reason = e.defaultDenyReason
				}
				return Decision{
					Allowed: false,
					Reason:  reason,
				}
			}
		}
	}

	if e.mode == ModeDenyByDefault {
		return Decision{
			Allowed: false,
			Reason:  e.defaultDenyReason,
		}
	}
	return Decision{Allowed: true}
}

func validateRule(rule Rule) error {
	effect := rule.Effect
	if effect == "" {
		effect = EffectDeny
	}
	switch effect {
	case EffectAllow, EffectDeny:
	default:
		return fmt.Errorf("unsupported effect %q", effect)
	}
	for _, side := range rule.SideEffects {
		switch side {
		case action.SideEffectNone, action.SideEffectRead, action.SideEffectWrite:
		default:
			return fmt.Errorf("unsupported side effect %q", side)
		}
	}
	return nil
}

func matchesRule(rule Rule, input DecisionInput) bool {
	if actor := strings.TrimSpace(rule.Actor); actor != "" && actor != input.Actor {
		return false
	}
	if actorPrefix := strings.TrimSpace(rule.ActorPrefix); actorPrefix != "" && !strings.HasPrefix(input.Actor, actorPrefix) {
		return false
	}
	if actionID := strings.TrimSpace(rule.ActionID); actionID != "" && actionID != input.ActionID {
		return false
	}
	if actionPrefix := strings.TrimSpace(rule.ActionPrefix); actionPrefix != "" && !strings.HasPrefix(input.ActionID, actionPrefix) {
		return false
	}
	if len(rule.SideEffects) > 0 {
		matched := false
		for _, side := range rule.SideEffects {
			if side == input.SideEffect {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if rule.DryRun != nil && *rule.DryRun != input.DryRun {
		return false
	}
	return true
}
