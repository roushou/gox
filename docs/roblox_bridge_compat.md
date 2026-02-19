# Roblox Bridge Compatibility Contract

This document defines compatibility expectations between:
- Go adapter: `internal/adapter/roblox`
- Roblox Studio plugin bridge: `plugin/roblox/src/bridge`

## Handshake

1. Client must authenticate with `bridge.auth` when auth is enabled.
2. Client must negotiate `bridge.hello` before any non-auth operation.
3. `bridge.hello` response includes:
   - `protocolVersion`
   - `capabilities` (string array)

Current protocol version: `1.1`.

## Addressing Model

Operations support path-based and ID-based targeting.

- Path fields: `parentPath`, `scriptPath`, `instancePath`, `rootPath`
- ID fields: `parentId`, `scriptId`, `instanceId`, `rootId`

Plugin responses include stable `instanceId` where applicable.

Current script operations:
- `script.create`
- `script.update`
- `script.delete`
- `script.get_source`
- `script.execute`

## Idempotency

Requests may include `idempotencyKey`.

- If duplicated, plugin transport returns the cached response for that key.
- If omitted, request ID is used as fallback.

## Typed Property Values

Primitive values are passed directly. Structured values use tagged objects with `_type`.

Supported typed values include:
- `Vector2`, `Vector3`, `Ray`
- `Color3` (`fromRGB`) and `Color3Float`
- `CFrame` (position or 12-component matrix)
- `UDim`, `UDim2`, `Rect`, `RectFloat`
- `BrickColor`
- `Enum`
- `Ref` (by `instanceId` or `path`)
- `NumberRange`, `NumberSequence`, `ColorSequence`
- `PhysicalProperties`
- `Axes`, `Faces`

## Relay Session Lifecycle

Relay sessions support:
- open: `POST /v1/bridge/session`
- read: `GET /v1/bridge/session/{sessionId}/read`
- write: `POST /v1/bridge/session/{sessionId}/write`
- close: `DELETE /v1/bridge/session/{sessionId}/close`

The hub enforces TTL and idle timeout; expired sessions are invalidated.

## Error Taxonomy

Preferred bridge error codes:
- `VALIDATION_ERROR`
- `NOT_FOUND`
- `ACCESS_DENIED`
- `CONFLICT`
- `TRANSIENT_ERROR`
- `INTERNAL_ERROR`

Go fault mapping treats `TRANSIENT_ERROR` as retryable internal failure by default.
