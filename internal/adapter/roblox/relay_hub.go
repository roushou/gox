package roblox

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/roushou/gox/internal/platform/identity"
)

var (
	ErrRelayNoSession   = errors.New("relay has no active session")
	ErrRelayBadSession  = errors.New("relay session is invalid")
	ErrRelayExpired     = errors.New("relay session expired")
	ErrRelayAuthFailure = errors.New("relay authentication failed")
)

type relaySession struct {
	id           string
	clientName   string
	createdAt    time.Time
	lastActivity time.Time
	toPlugin     chan string
	fromPlugin   chan string
}

type RelayHubOptions struct {
	SessionTTL  time.Duration
	IdleTimeout time.Duration
	BufferSize  int
}

func defaultRelayHubOptions() RelayHubOptions {
	return RelayHubOptions{
		SessionTTL:  30 * time.Minute,
		IdleTimeout: 5 * time.Minute,
		BufferSize:  128,
	}
}

type RelayHub struct {
	authToken string
	opts      RelayHubOptions

	mu      sync.RWMutex
	session *relaySession
}

func NewRelayHub(authToken string) *RelayHub {
	return NewRelayHubWithOptions(authToken, RelayHubOptions{})
}

func NewRelayHubWithOptions(authToken string, opts RelayHubOptions) *RelayHub {
	normalized := defaultRelayHubOptions()
	if opts.SessionTTL > 0 {
		normalized.SessionTTL = opts.SessionTTL
	}
	if opts.IdleTimeout > 0 {
		normalized.IdleTimeout = opts.IdleTimeout
	}
	if opts.BufferSize > 0 {
		normalized.BufferSize = opts.BufferSize
	}
	return &RelayHub{
		authToken: authToken,
		opts:      normalized,
	}
}

func (h *RelayHub) OpenSession(token, clientName string) (string, error) {
	if h.authToken != "" && token != h.authToken {
		return "", ErrRelayAuthFailure
	}
	if clientName == "" {
		clientName = "unknown"
	}

	session := &relaySession{
		id:           identity.NewID(),
		clientName:   clientName,
		createdAt:    time.Now().UTC(),
		lastActivity: time.Now().UTC(),
		toPlugin:     make(chan string, h.opts.BufferSize),
		fromPlugin:   make(chan string, h.opts.BufferSize),
	}

	h.mu.Lock()
	h.session = session
	h.mu.Unlock()
	return session.id, nil
}

func (h *RelayHub) ActiveSessionID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.session == nil || h.expiredLocked(h.session, time.Now().UTC()) {
		h.session = nil
		return ""
	}
	return h.session.id
}

func (h *RelayHub) ReadForPlugin(ctx context.Context, sessionID string) (string, error) {
	s, err := h.requireSession(sessionID)
	if err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case line := <-s.toPlugin:
		h.touchSession(s.id)
		return line, nil
	}
}

func (h *RelayHub) WriteFromPlugin(ctx context.Context, sessionID, line string) error {
	s, err := h.requireSession(sessionID)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.fromPlugin <- line:
		h.touchSession(s.id)
		return nil
	}
}

func (h *RelayHub) SendToPlugin(ctx context.Context, line string) error {
	s, err := h.activeSession()
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.toPlugin <- line:
		h.touchSession(s.id)
		return nil
	}
}

func (h *RelayHub) ReceiveFromPlugin(ctx context.Context) (string, error) {
	s, err := h.activeSession()
	if err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case line := <-s.fromPlugin:
		h.touchSession(s.id)
		return line, nil
	}
}

func (h *RelayHub) CloseSession(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.session == nil {
		return false
	}
	if h.session.id != sessionID {
		return false
	}
	h.session = nil
	return true
}

func (h *RelayHub) activeSession() (*relaySession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.session == nil {
		return nil, ErrRelayNoSession
	}
	now := time.Now().UTC()
	if h.expiredLocked(h.session, now) {
		h.session = nil
		return nil, ErrRelayExpired
	}
	return h.session, nil
}

func (h *RelayHub) requireSession(sessionID string) (*relaySession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.session == nil {
		return nil, ErrRelayNoSession
	}
	now := time.Now().UTC()
	if h.expiredLocked(h.session, now) {
		h.session = nil
		return nil, ErrRelayExpired
	}
	if sessionID == "" || h.session.id != sessionID {
		return nil, ErrRelayBadSession
	}
	return h.session, nil
}

func (h *RelayHub) touchSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.session == nil {
		return
	}
	if h.session.id != sessionID {
		return
	}
	h.session.lastActivity = time.Now().UTC()
}

func (h *RelayHub) expiredLocked(session *relaySession, now time.Time) bool {
	if session == nil {
		return true
	}
	if h.opts.SessionTTL > 0 && now.Sub(session.createdAt) > h.opts.SessionTTL {
		return true
	}
	if h.opts.IdleTimeout > 0 && now.Sub(session.lastActivity) > h.opts.IdleTimeout {
		return true
	}
	return false
}
