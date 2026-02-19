# Policy Pack Format (v1)

Gox supports two JSON policy file shapes:
- legacy `RuleSet` JSON (flat rule fields)
- versioned policy pack JSON (recommended)

Use `GOX_POLICY_RULES_PATH` to load a policy file at startup.

## Versioned Pack Schema

Schema file:
- `/Users/roushou/dev/gox/policy/schema/policy-pack.schema.json`

Top-level fields:
- `version` (required): currently `v1`
- `mode` (optional): `allow-all` or `deny-by-default`
- `defaultDenyReason` (optional)
- `rules` (required array)

Each rule:
- `name` (optional)
- `match` (required)
- `effect` (required): `allow` or `deny`
- `reason` (optional, used for deny decisions)
- `requiresApproval` (optional bool, for allow rules)
- `priority` (optional, higher wins; stable sort for ties)

`match` selectors:
- `actor`
- `actorPrefix`
- `actionId`
- `actionPrefix`
- `sideEffects` (`none`, `read`, `write`)
- `dryRun`

## Sample Packs

- Local development profile:
  - `/Users/roushou/dev/gox/policy/packs/local-dev.json`
- Quarantine profile:
  - `/Users/roushou/dev/gox/policy/packs/quarantine.json`

Example:

```json
{
  "version": "v1",
  "mode": "deny-by-default",
  "defaultDenyReason": "action blocked by policy",
  "rules": [
    {
      "name": "allow ping",
      "match": { "actionId": "core.ping" },
      "effect": "allow",
      "priority": 100
    }
  ]
}
```
