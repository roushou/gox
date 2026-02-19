package policy

import (
	"testing"

	"github.com/roushou/gox/internal/domain/action"
)

func TestParseRuleSetJSONPack(t *testing.T) {
	data := []byte(`{
		"version": "v1",
		"mode": "deny-by-default",
		"defaultDenyReason": "explicit allow required",
		"rules": [
			{
				"name": "allow core ping",
				"match": {"actionId": "core.ping"},
				"effect": "allow",
				"requiresApproval": true,
				"reason": "manual approval required",
				"priority": 10
			},
			{
				"name": "deny roblox writes",
				"match": {"actionPrefix": "roblox.", "sideEffects": ["write"]},
				"effect": "deny",
				"reason": "write blocked"
			}
		]
	}`)

	set, err := ParseRuleSetJSON(data, ModeAllowAll)
	if err != nil {
		t.Fatalf("parse pack: %v", err)
	}
	if set.Mode != ModeDenyByDefault {
		t.Fatalf("unexpected mode: %q", set.Mode)
	}
	if set.DefaultDenyReason != "explicit allow required" {
		t.Fatalf("unexpected default deny reason: %q", set.DefaultDenyReason)
	}
	if len(set.Rules) != 2 {
		t.Fatalf("unexpected rule count: %d", len(set.Rules))
	}
	if set.Rules[0].ActionID != "core.ping" {
		t.Fatalf("unexpected action id: %#v", set.Rules[0])
	}
	if !set.Rules[0].RequiresApproval {
		t.Fatalf("expected requiresApproval on first rule: %#v", set.Rules[0])
	}
	if set.Rules[0].Reason != "manual approval required" {
		t.Fatalf("unexpected reason on first rule: %#v", set.Rules[0])
	}
	if len(set.Rules[1].SideEffects) != 1 || set.Rules[1].SideEffects[0] != action.SideEffectWrite {
		t.Fatalf("unexpected side effects: %#v", set.Rules[1].SideEffects)
	}
}

func TestParseRuleSetJSONPackRejectsUnknownVersion(t *testing.T) {
	data := []byte(`{
		"version": "v2",
		"rules": []
	}`)
	if _, err := ParseRuleSetJSON(data, ModeAllowAll); err == nil {
		t.Fatal("expected parse failure for unsupported version")
	}
}

func TestParseRuleSetJSONLegacyRuleSet(t *testing.T) {
	data := []byte(`{
		"mode": "deny-by-default",
		"rules": [
			{
				"name": "allow ping",
				"actionId": "core.ping",
				"effect": "allow"
			}
		]
	}`)

	set, err := ParseRuleSetJSON(data, ModeAllowAll)
	if err != nil {
		t.Fatalf("parse legacy rules: %v", err)
	}
	if set.Mode != ModeDenyByDefault {
		t.Fatalf("unexpected mode: %q", set.Mode)
	}
	if len(set.Rules) != 1 || set.Rules[0].ActionID != "core.ping" {
		t.Fatalf("unexpected rules: %#v", set.Rules)
	}
}

func TestParseRuleSetJSONUsesFallbackMode(t *testing.T) {
	data := []byte(`{
		"version": "v1",
		"rules": []
	}`)

	set, err := ParseRuleSetJSON(data, ModeDenyByDefault)
	if err != nil {
		t.Fatalf("parse pack: %v", err)
	}
	if set.Mode != ModeDenyByDefault {
		t.Fatalf("unexpected fallback mode: %q", set.Mode)
	}
}
