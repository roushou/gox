# Roblox Bridge Module

This folder contains the Roblox Studio plugin-side bridge counterpart for Gox.

## Files

- `Protocol.luau`: request/response and helper constructors
- `GoxBridge.luau`: request dispatcher and operation handlers
- `TransportLoop.luau`: authenticated line-based transport loop
- `PluginBootstrap.luau`: bootstrap helper to start a bridge session
- `HttpRelayAdapter.luau`: concrete adapter for `relay-http` mode

## Supported Operations

- `bridge.ping`
- `bridge.auth` (enforced by `TransportLoop`)
- `bridge.hello` (required capability/version handshake after auth)
- `script.create`
- `script.update`
- `script.delete`
- `script.get_source`
- `script.execute`
- `instance.create`
- `instance.set_property`
- `instance.delete`
- `instance.get`
- `instance.list_children`
- `instance.find`

Pagination notes:
- `instance.list_children` accepts `limit` and `offset` and returns `hasMore` plus `nextOffset`.
- `instance.find` accepts `limit`/`offset` (or legacy `maxResults`) and returns `hasMore` plus `nextOffset`.

## Contract

The payload and operation names align with:
- `/Users/roushou/dev/gox/internal/adapter/roblox/protocol.go`

`GoxBridge` is transport-agnostic by design and exposes:
- `HandleJson(encodedRequest: string): string`
- `HandleRequest(request): response`

`TransportLoop` enforces `bridge.auth` before dispatching other operations and rejects unauthenticated requests by default.

A plugin transport adapter should implement line-based I/O (`ReadLine`/`WriteLine`/`Close`) and can then be used through `PluginBootstrap.Start(...)`.

Handshake and idempotency notes:
- `TransportLoop` requires `bridge.auth` then `bridge.hello` before forwarding other operations.
- Requests may include `idempotencyKey`; duplicate keys return the cached response.

For `relay-http` mode:
- start Gox with `GOX_BRIDGE_NETWORK=relay-http`
- configure `GOX_BRIDGE_RELAY_ADDRESS` and `GOX_BRIDGE_AUTH_TOKEN`
- in plugin code, use `PluginBootstrap.StartWithHttpRelay("http://127.0.0.1:47012", "<token>")`
- for non-blocking control (start/stop), use `PluginBootstrap.StartWithHttpRelayAsync(...)` and keep the returned session handle.
- plugin close now calls relay `DELETE /v1/bridge/session/{sessionId}/close` for explicit session teardown.

A full plugin entrypoint example is available at:
- `/Users/roushou/dev/gox/plugin/roblox/src/plugin/PluginMain.server.luau`
