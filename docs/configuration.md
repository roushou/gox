# Configuration Reference

All configuration is via environment variables. Every variable has a sensible default.

## General

| Variable | Default | Description |
|----------|---------|-------------|
| `GOX_SERVER_NAME` | `gox` | Server name reported in MCP handshake |
| `GOX_LOG_LEVEL` | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `GOX_SHUTDOWN_TIMEOUT_SECONDS` | `10` | Graceful shutdown timeout |
| `GOX_ACTION_TIMEOUT_SECONDS` | `30` | Per-action execution timeout |

## Bridge

| Variable | Default | Description |
|----------|---------|-------------|
| `GOX_BRIDGE_ENABLED` | `false` | Enable Roblox bridge |
| `GOX_BRIDGE_NETWORK` | `tcp` | Transport: `tcp` or `relay-http` |
| `GOX_BRIDGE_ADDRESS` | `127.0.0.1:47011` | TCP bridge address |
| `GOX_BRIDGE_RELAY_ADDRESS` | `127.0.0.1:47012` | Relay HTTP address |
| `GOX_BRIDGE_TIMEOUT_SECONDS` | `5` | Bridge request timeout |
| `GOX_BRIDGE_AUTH` | `true` | Require auth token |
| `GOX_BRIDGE_AUTH_TOKEN` | — | Shared secret for bridge auth |

## Runlog

Run history is in-memory (local process only). These controls tune retention:

| Variable | Default | Description |
|----------|---------|-------------|
| `GOX_RUNLOG_RETENTION_MAX_AGE_SECONDS` | `259200` (72h) | Auto-prune records older than this (`0` disables pruning) |

## Workflow Runtime

| Variable | Default | Description |
|----------|---------|-------------|
| `GOX_WORKFLOW_MAX_CONCURRENT_RUNS` | `8` | Max parallel workflow runs |
| `GOX_WORKFLOW_MAX_STEPS` | `64` | Max steps per workflow |
| `GOX_WORKFLOW_MAX_LOGS_PER_RUN` | `256` | Max log entries per run |
| `GOX_WORKFLOW_DEFAULT_STEP_TIMEOUT_SECONDS` | `30` | Default step timeout |
| `GOX_WORKFLOW_MAX_STEP_TIMEOUT_SECONDS` | `300` | Max allowed step timeout |
| `GOX_WORKFLOW_MAX_ATTEMPTS` | `5` | Max retry attempts per step |
| `GOX_WORKFLOW_MAX_BACKOFF_SECONDS` | `30` | Max retry backoff |
| `GOX_WORKFLOW_RETENTION_MAX_RUNS` | `1000` | Max completed runs retained in memory |
| `GOX_WORKFLOW_RETENTION_TTL_SECONDS` | `86400` (24h) | TTL for completed workflow state |

## Policy

| Variable | Default | Description |
|----------|---------|-------------|
| `GOX_POLICY_MODE` | `deny-by-default` | Policy mode: `allow-all` or `deny-by-default` |
| `GOX_POLICY_RULES_PATH` | — | Path to JSON rules file |

In `deny-by-default` mode without a rules file, Gox still allows:
- `core.ping`
- `project.workspace_stats`

All other actions are denied.

Policy files:
- [`policy/packs/local-dev.json`](../policy/packs/local-dev.json) - permissive local development rules
- [`policy/packs/quarantine.json`](../policy/packs/quarantine.json) - restrictive quarantine rules
- [`policy/schema/policy-pack.schema.json`](../policy/schema/policy-pack.schema.json) - JSON schema for policy packs

See [`policy_pack.md`](policy_pack.md) for full policy pack format.

## MCP Request Guardrail

| Variable | Default | Description |
|----------|---------|-------------|
| `GOX_MCP_MAX_PAYLOAD_BYTES` | `1048576` (1 MB) | Max request payload size accepted by `tools/call` |
