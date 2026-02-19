# Roblox Plugin Workspace

This directory contains the Roblox Studio plugin source, Rojo project, and built plugin artifact.

## Structure

- `src/bridge`: reusable bridge modules (`Protocol`, `TransportLoop`, `GoxBridge`, relay adapter, bootstrap)
- `src/plugin`: plugin entrypoint and UI (`PluginMain.server.luau`)
- `default.project.json`: Rojo project mapping
- `dist/GoxBridge.rbxmx`: generated plugin artifact

## Build and Install

With `mise` tasks:

```bash
mise run plugin:build
mise run plugin:install
```

Direct Rojo build:

```bash
mkdir -p plugin/roblox/dist
rojo build plugin/roblox/default.project.json -o plugin/roblox/dist/GoxBridge.rbxmx
```
