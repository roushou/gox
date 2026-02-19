# MCP Contract (v0.1.x)

This document defines the current stable MCP behavior exposed by Gox.

## Protocol and Transport

- Protocol: MCP over JSON-RPC 2.0
- Server implementation: `github.com/modelcontextprotocol/go-sdk`
- Typical transport: stdio (MCP server process)

## Tool Catalog

Always available tools:
- `core.ping`
- `project.workspace_stats`
- `workflow.run`
- `workflow.status`
- `workflow.cancel`
- `workflow.list`
- `workflow.logs`

Bridge-gated tools (`GOX_BRIDGE_ENABLED=true`):
- `roblox.bridge_ping`
- `roblox.script_create`
- `roblox.script_update`
- `roblox.script_delete`
- `roblox.script_get_source`
- `roblox.script_execute`
- `roblox.instance_create`
- `roblox.instance_set_property`
- `roblox.instance_delete`
- `roblox.instance_get`
- `roblox.instance_list_children`
- `roblox.instance_find`

## Resource Catalog

Available resources:
- `gox://status` (server status summary)
- `gox://runs` (recent runlog records)
- `gox://runs/{runId}` (single runlog record by id)

## Action Tool Envelope

For action tools (non-`workflow.*`), successful `tools/call` structured output is:

```json
{
  "runId": "string",
  "output": {},
  "diff": {
    "summary": "string",
    "entries": ["string"]
  }
}
```

Notes:
- `diff` may be `null`.
- Read/query actions typically return `diff: null`.

## `roblox.script_execute` Output Contract

Input:
- one of `scriptPath` or `scriptId` (required)
- `functionName` (optional)
- `args` (optional array, only used with `functionName`)
- `forceRequire` (optional bool)
- `expectReturn` (optional bool)

Output (`output`):
- `executed` (boolean)
- `called` (boolean): `true` when `functionName` was invoked
- `path` (string): resolved Roblox path
- `instanceId` (string)
- `returnValue` (any JSON-safe value)

Dry-run output:
- `dryRun` (boolean)
- `path` (string)
- `instanceId` (string)

Validation behavior:
- `expectReturn=true` fails with validation error if `returnValue` is `nil`.
- `functionName` fails with not-found error if it is not exported by the required module value.

Use action id `roblox.script_execute` (underscore), not `roblox.script.execute`.

Workflow step condition support:
- Each step may include optional `when`:
  - `stepId` (string, required): previous step id to read from
  - `path` (string, optional): dot-path inside referenced step output
  - `equals` (any JSON value, required): expected value
- When condition does not match, the step is skipped.

## Workflow Tool Output Shapes

- `workflow.run` and `workflow.status` return a workflow run snapshot:
  - `runId`, `workflowId`, `correlationId`, `status`
  - `startedAt`, `endedAt`
  - `totalSteps`, `completedSteps`
  - `cancelRequested`
  - optional `lastError` (`code`, `message`)
- `workflow.cancel` returns the snapshot plus:
  - `acknowledged` (bool)
- `workflow.list` returns:
  - `runs` (array of snapshots)
  - optional `nextCursor`
- `workflow.logs` returns:
  - `runId`
  - `logs` (array with `timestamp`, `level`, `message`, optional `stepId`, `actionId`)

## Request Metadata

Supported request metadata for action tools:
- `actor` (string)
- `correlationId` (string)
- `dryRun` (bool or bool-like string)
- `approved` (bool or bool-like string)

Workflow run control fields are passed in arguments for `workflow.run`:
- `actor`
- `correlationId`
- `dryRun`
- `approved`

## Tool Metadata Keys

Action tools include:
- `gox:version`
- `gox:sideEffectLevel`
- `gox:supportsDryRun`
- `gox:requiresBridge`

Workflow tools include:
- `gox:type = "workflow"`

## Error Contract

Error code mapping:
- validation faults -> JSON-RPC `-32602` (`invalid params`)
- not found faults -> `-32044`
- denied faults -> `-32043`
- unimplemented faults -> `-32041`
- everything else -> JSON-RPC internal error

Error `data` payload fields:
- `code`
- `category`
- `retryable`
- `correlation_id`
- `details`
- validation-only (when available):
  - `validation_path`
  - `validation_rule`

## Runtime Guardrails

Server-side request guards:
- max payload size per `tools/call`

When exceeded:
- payload violations return `-32602` (`invalid params`)

## Compatibility Expectations

- Additive schema changes are preferred (new optional fields).
- Existing tool IDs and required fields should not be changed in `v0.1.x`.
- Legacy compatibility currently supported:
  - `roblox.instance_find.maxResults` as alias of `limit` (do not send both).
