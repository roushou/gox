package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/roushou/gox/internal/domain/action"
)

const CurrentPackVersion = "v1"

type Pack struct {
	Version           string     `json:"version"`
	Mode              Mode       `json:"mode"`
	DefaultDenyReason string     `json:"defaultDenyReason,omitempty"`
	Rules             []PackRule `json:"rules"`
}

type PackRule struct {
	Name             string        `json:"name,omitempty"`
	Match            PackRuleMatch `json:"match"`
	Effect           Effect        `json:"effect,omitempty"`
	Reason           string        `json:"reason,omitempty"`
	RequiresApproval bool          `json:"requiresApproval,omitempty"`
	Priority         int           `json:"priority,omitempty"`
}

type PackRuleMatch struct {
	Actor        string                   `json:"actor,omitempty"`
	ActorPrefix  string                   `json:"actorPrefix,omitempty"`
	ActionID     string                   `json:"actionId,omitempty"`
	ActionPrefix string                   `json:"actionPrefix,omitempty"`
	SideEffects  []action.SideEffectLevel `json:"sideEffects,omitempty"`
	DryRun       *bool                    `json:"dryRun,omitempty"`
}

func ParseRuleSetJSON(data []byte, fallbackMode Mode) (RuleSet, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return RuleSet{}, fmt.Errorf("empty policy file")
	}
	if isPackPayload([]byte(trimmed)) {
		return parsePackPayload([]byte(trimmed), fallbackMode)
	}
	return parseLegacyRuleSetPayload([]byte(trimmed), fallbackMode)
}

func LoadRuleSetFile(path string, fallbackMode Mode) (RuleSet, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return RuleSet{}, fmt.Errorf("read policy rules file %q: %w", path, err)
	}
	set, err := ParseRuleSetJSON(payload, fallbackMode)
	if err != nil {
		return RuleSet{}, fmt.Errorf("parse policy rules file %q: %w", path, err)
	}
	return set, nil
}

func parseLegacyRuleSetPayload(data []byte, fallbackMode Mode) (RuleSet, error) {
	var out RuleSet
	if err := json.Unmarshal(data, &out); err != nil {
		return RuleSet{}, err
	}
	if out.Mode == "" {
		out.Mode = fallbackMode
	}
	return out, nil
}

func parsePackPayload(data []byte, fallbackMode Mode) (RuleSet, error) {
	var pack Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		return RuleSet{}, err
	}

	version := strings.TrimSpace(pack.Version)
	if version == "" {
		return RuleSet{}, fmt.Errorf("policy pack version is required")
	}
	if version != CurrentPackVersion {
		return RuleSet{}, fmt.Errorf("unsupported policy pack version %q", pack.Version)
	}

	mode := pack.Mode
	if mode == "" {
		mode = fallbackMode
	}
	rules := make([]Rule, 0, len(pack.Rules))
	for _, entry := range pack.Rules {
		rules = append(rules, Rule{
			Name:             entry.Name,
			Actor:            entry.Match.Actor,
			ActorPrefix:      entry.Match.ActorPrefix,
			ActionID:         entry.Match.ActionID,
			ActionPrefix:     entry.Match.ActionPrefix,
			SideEffects:      entry.Match.SideEffects,
			DryRun:           entry.Match.DryRun,
			RequiresApproval: entry.RequiresApproval,
			Effect:           entry.Effect,
			Reason:           entry.Reason,
			Priority:         entry.Priority,
		})
	}

	return RuleSet{
		Mode:              mode,
		DefaultDenyReason: pack.DefaultDenyReason,
		Rules:             rules,
	}, nil
}

func isPackPayload(data []byte) bool {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return false
	}
	if _, ok := envelope["version"]; ok {
		return true
	}

	rawRules, ok := envelope["rules"]
	if !ok {
		return false
	}
	var rules []map[string]json.RawMessage
	if err := json.Unmarshal(rawRules, &rules); err != nil {
		return false
	}
	for _, rule := range rules {
		if _, ok := rule["match"]; ok {
			return true
		}
	}
	return false
}
