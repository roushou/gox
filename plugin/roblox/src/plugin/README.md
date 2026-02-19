# Gox Roblox Plugin Example

This folder provides an example Studio plugin entrypoint for `relay-http` mode.

## File

- `PluginMain.server.luau`: opens a small settings UI, persists values via plugin settings, and starts the relay bridge loop. Named `.server.luau` so Rojo emits a `Script` instance (required for plugins).

## What It Configures

- Relay URL (default `http://127.0.0.1:47012`)
- Auth token (must match `GOX_BRIDGE_AUTH_TOKEN`)
- Client name

## Expected Gox Environment

```bash
export GOX_BRIDGE_ENABLED=true
export GOX_BRIDGE_NETWORK=relay-http
export GOX_BRIDGE_RELAY_ADDRESS=127.0.0.1:47012
export GOX_BRIDGE_AUTH=true
export GOX_BRIDGE_AUTH_TOKEN=replace-with-long-random-secret
```

## Notes

- `Start Bridge` launches a non-blocking session.
- `Stop Bridge` requests loop cancellation and closes the current session.
- The bridge modules are expected at `plugin/roblox/src/bridge`.
