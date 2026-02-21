# Gox

Gox is an AI agent runtime for Roblox Studio, exposed over [MCP](https://modelcontextprotocol.io/).

It lets AI agents perform high-confidence Roblox game development through:
- **Actions** — atomic, safe, auditable operations
- **Workflows** — multi-step orchestration built from actions
- **Essentials** — validation, run auditing, and policy controls for safe local use

A typed Go client wrapper is available in [`pkg/client`](pkg/client).

## Quick Start

```bash
go build -o gox ./cmd/gox
./gox
```

Communicates over stdio using JSON-RPC 2.0 (MCP). Without bridge mode, `core.ping` and `project.workspace_stats` are available immediately.

## Tools

### MCP Protocol

| Method | Description |
|--------|-------------|
| `initialize` | Capability handshake |
| `tools/list` | List registered tools |
| `tools/call` | Execute a tool |

### Resources

| URI | Description |
|-----|-------------|
| `gox://status` | Server status |
| `gox://runs` | All run records |
| `gox://runs/{runId}` | Single run record |

### Core Actions

| Tool | Description | Bridge |
|------|-------------|--------|
| `core.ping` | Health check | No |
| `project.workspace_stats` | Workspace file stats | No |

### Roblox Bridge Actions

Requires `GOX_BRIDGE_ENABLED=true`.

| Tool | Description |
|------|-------------|
| `roblox.bridge_ping` | Bridge connectivity check |
| `roblox.script_create` | Create a script |
| `roblox.script_update` | Update script source |
| `roblox.script_delete` | Delete a script |
| `roblox.script_get_source` | Read script source |
| `roblox.script_execute` | Execute a script |
| `roblox.instance_create` | Create an instance |
| `roblox.instance_set_property` | Set instance property |
| `roblox.instance_delete` | Delete an instance |
| `roblox.instance_get` | Get instance details |
| `roblox.instance_list_children` | List child instances |
| `roblox.instance_find` | Find instances by query |
| `roblox.scene_snapshot` | Snapshot scene subtree metadata |
| `roblox.scene_plan` | Build deterministic mutation plan |
| `roblox.scene_apply` | Apply scene plan transactionally |
| `roblox.scene_validate` | Run scene quality gates |
| `roblox.scene_capture` | Capture scene thumbnail when supported |

### Workflow Tools

| Tool | Description |
|------|-------------|
| `workflow.run` | Start a multi-step workflow |
| `workflow.status` | Check workflow run status |
| `workflow.cancel` | Cancel a running workflow |
| `workflow.list` | List workflow runs |
| `workflow.logs` | Get workflow run logs |

Workflows support conditional step execution via per-step `when`, retry policies, and timeout configuration.

### Approval and Conditional Steps

Policy rules can mark allow-rules as `requiresApproval`. In that case, pass `approved=true` with the request.

For action tools, set `approved` in MCP request metadata.

For workflows, use the `approved` argument on `workflow.run` and optionally per-step `when` conditions:

```json
{
  "definition": {
    "id": "wf.example",
    "steps": [
      {
        "id": "step-1",
        "actionId": "core.ping",
        "input": {}
      },
      {
        "id": "step-2",
        "actionId": "roblox.script_update",
        "when": {
          "stepId": "step-1",
          "path": "ok",
          "equals": true
        },
        "input": {}
      }
    ]
  },
  "approved": true
}
```

`when` rules reference a previous step (`stepId`) and compare either the whole output or a dot-path (`path`) against `equals`.

## Configuration

All configuration is via environment variables with sensible defaults.

To connect to Roblox Studio, set the bridge variables:

```bash
export GOX_BRIDGE_ENABLED=true
export GOX_BRIDGE_NETWORK=relay-http
export GOX_BRIDGE_AUTH_TOKEN=replace-with-long-random-secret
```

Policy defaults to `deny-by-default`. To allow actions during local development:

```bash
export GOX_POLICY_RULES_PATH=policy/packs/local-dev.json
```

If no policy file is provided, Gox keeps a minimal baseline allowlist (`core.ping`, `project.workspace_stats`) and denies everything else.

Run history is stored in memory and exposed via MCP resources (`gox://runs`, `gox://runs/{runId}`).

Full environment variable reference: [`docs/configuration.md`](docs/configuration.md)

## Integration with AI Clients

Gox works with any MCP-compatible client over stdio. Build the binary first for faster startup:

```bash
go build -o gox ./cmd/gox
```

The examples below enable bridge mode with `relay-http`. Adjust or remove the `env` block if you don't need it.

<details>
<summary><b>Claude Desktop</b></summary>

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "gox": {
      "command": "/path/to/gox",
      "env": {
        "GOX_BRIDGE_ENABLED": "true",
        "GOX_BRIDGE_NETWORK": "relay-http",
        "GOX_BRIDGE_AUTH_TOKEN": "replace-with-long-random-secret"
      }
    }
  }
}
```
</details>

<details>
<summary><b>Claude Code</b></summary>

```bash
claude mcp add gox /path/to/gox
```

Or with environment variables:

```bash
claude mcp add gox -e GOX_BRIDGE_ENABLED=true -e GOX_BRIDGE_NETWORK=relay-http -e GOX_BRIDGE_AUTH_TOKEN=replace-with-long-random-secret -- /path/to/gox
```
</details>

<details>
<summary><b>Codex</b></summary>

Add to `~/.codex/config.toml` (or `.codex/config.toml` in your project):

```toml
[mcp_servers.gox]
command = "/path/to/gox"

[mcp_servers.gox.env]
GOX_BRIDGE_ENABLED = "true"
GOX_BRIDGE_NETWORK = "relay-http"
GOX_BRIDGE_AUTH_TOKEN = "replace-with-long-random-secret"
```

Or via CLI:

```bash
codex mcp add gox -- /path/to/gox
```
</details>

<details>
<summary><b>Cursor</b></summary>

Add to `.cursor/mcp.json` in your project root:

```json
{
  "mcpServers": {
    "gox": {
      "command": "/path/to/gox",
      "env": {
        "GOX_BRIDGE_ENABLED": "true",
        "GOX_BRIDGE_NETWORK": "relay-http",
        "GOX_BRIDGE_AUTH_TOKEN": "replace-with-long-random-secret"
      }
    }
  }
}
```
</details>

<details>
<summary><b>VS Code (GitHub Copilot)</b></summary>

Add to `.vscode/mcp.json` in your workspace:

```json
{
  "servers": {
    "gox": {
      "type": "stdio",
      "command": "/path/to/gox",
      "env": {
        "GOX_BRIDGE_ENABLED": "true",
        "GOX_BRIDGE_NETWORK": "relay-http",
        "GOX_BRIDGE_AUTH_TOKEN": "replace-with-long-random-secret"
      }
    }
  }
}
```
</details>

## Roblox Plugin

The Roblox Studio plugin that connects to the bridge lives in [`plugin/roblox`](plugin/roblox):

- Bridge modules: `plugin/roblox/src/bridge`
- Plugin entrypoint: `plugin/roblox/src/plugin/PluginMain.server.luau`
- Rojo project: `plugin/roblox/default.project.json`
- Built artifact: `plugin/roblox/dist/GoxBridge.rbxmx`

## Test

```bash
go test ./...
go test -race ./...
```

## Error Format

When an action input fails validation, `tools/call` returns a JSON-RPC error with machine-readable fields:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32602,
    "message": "input.scriptType: must be one of the allowed values",
    "data": {
      "code": "VALIDATION_ERROR",
      "category": "client",
      "correlation_id": "abc123",
      "validation_path": "input.scriptType",
      "validation_rule": "enum"
    }
  }
}
```

## Docs

- [Configuration](docs/configuration.md)
- [MCP contract](docs/mcp_contract.md)
- [Roblox bridge compatibility](docs/roblox_bridge_compat.md)
- [Policy pack format](docs/policy_pack.md)
