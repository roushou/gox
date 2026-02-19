package mcp

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

const (
	defaultServerMaxPayloadBytes = 1 << 20
)

type ServerRuntimeConfig struct {
	MaxPayloadBytes int
}

func defaultServerRuntimeConfig() ServerRuntimeConfig {
	return ServerRuntimeConfig{
		MaxPayloadBytes: defaultServerMaxPayloadBytes,
	}
}

func normalizeServerRuntimeConfig(cfg ServerRuntimeConfig) ServerRuntimeConfig {
	out := defaultServerRuntimeConfig()
	if cfg.MaxPayloadBytes > 0 {
		out.MaxPayloadBytes = cfg.MaxPayloadBytes
	}
	return out
}

type requestGuards struct {
	maxPayloadBytes int
}

func newRequestGuards(cfg ServerRuntimeConfig) *requestGuards {
	normalized := normalizeServerRuntimeConfig(cfg)
	return &requestGuards{
		maxPayloadBytes: normalized.MaxPayloadBytes,
	}
}

func (g *requestGuards) checkAndConsume(payload []byte) error {
	if g.maxPayloadBytes > 0 && len(payload) > g.maxPayloadBytes {
		return wireError(
			jsonrpc.CodeInvalidParams,
			"request payload exceeds configured limit",
			map[string]any{
				"payload_bytes": len(payload),
				"max_bytes":     g.maxPayloadBytes,
			},
		)
	}
	return nil
}

func (c ServerRuntimeConfig) Validate() error {
	if c.MaxPayloadBytes < 0 {
		return fmt.Errorf("max payload bytes must be >= 0")
	}
	return nil
}
