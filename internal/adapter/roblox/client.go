package roblox

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/roushou/gox/internal/platform/identity"
)

type Health struct {
	Connected           bool
	LastSuccessAt       time.Time
	LastFailureAt       time.Time
	LastFailureMessage  string
	ConsecutiveFailures int
}

type Client struct {
	transport      Transport
	logger         *slog.Logger
	requestTimeout time.Duration

	mu     sync.RWMutex
	health Health
}

func NewClient(transport Transport, logger *slog.Logger, requestTimeout time.Duration) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	if requestTimeout <= 0 {
		requestTimeout = 5 * time.Second
	}

	return &Client{
		transport:      transport,
		logger:         logger,
		requestTimeout: requestTimeout,
		health: Health{
			Connected: transport != nil,
		},
	}
}

func (c *Client) Execute(ctx context.Context, correlationID string, operation Operation, payload map[string]any) (Response, error) {
	if c.transport == nil {
		c.setFailure(ErrNotConnected)
		return Response{}, ErrNotConnected
	}
	if correlationID == "" {
		correlationID = identity.NewID()
	}

	req := Request{
		RequestID:     identity.NewID(),
		CorrelationID: correlationID,
		Operation:     operation,
		Payload:       payload,
		Timestamp:     time.Now().UTC(),
	}
	if err := req.Validate(); err != nil {
		c.setFailure(err)
		return Response{}, ProtocolError{Message: err.Error()}
	}

	callCtx := ctx
	cancel := func() {}
	if c.requestTimeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, c.requestTimeout)
	}
	defer cancel()

	resp, err := c.transport.Call(callCtx, req)
	if err != nil {
		c.setFailure(err)
		return Response{}, err
	}

	if err := resp.ValidateAgainstRequest(req); err != nil {
		c.setFailure(err)
		return Response{}, ProtocolError{Message: err.Error()}
	}
	if !resp.Success && resp.Error != nil {
		bridgeErr := BridgeCallError{
			Code:          resp.Error.Code,
			Message:       resp.Error.Message,
			Retryable:     resp.Error.Retryable,
			CorrelationID: resp.CorrelationID,
			Details:       resp.Error.Details,
		}
		c.setFailure(bridgeErr)
		return Response{}, bridgeErr
	}

	c.setSuccess()
	return resp, nil
}

func (c *Client) Ping(ctx context.Context) error {
	if c.transport == nil {
		c.setFailure(ErrNotConnected)
		return ErrNotConnected
	}

	pingCtx := ctx
	cancel := func() {}
	if c.requestTimeout > 0 {
		pingCtx, cancel = context.WithTimeout(ctx, c.requestTimeout)
	}
	defer cancel()

	if err := c.transport.Ping(pingCtx); err != nil {
		c.setFailure(err)
		return err
	}
	c.setSuccess()
	return nil
}

func (c *Client) StartHealthChecks(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Ping(ctx); err != nil {
				c.logger.Warn("roblox bridge health check failed", "error", err)
			}
		}
	}
}

func (c *Client) Health() Health {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.health
}

func (c *Client) Close() error {
	if c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

func (c *Client) setSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.health.Connected = true
	c.health.LastSuccessAt = time.Now().UTC()
	c.health.LastFailureMessage = ""
	c.health.ConsecutiveFailures = 0
}

func (c *Client) setFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.health.Connected = false
	c.health.LastFailureAt = time.Now().UTC()
	c.health.LastFailureMessage = err.Error()
	c.health.ConsecutiveFailures++
}
