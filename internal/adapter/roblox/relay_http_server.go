package roblox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type RelayHTTPServer struct {
	addr      string
	hub       *RelayHub
	authToken string
	logger    *slog.Logger
	server    *http.Server
}

func NewRelayHTTPServer(addr string, hub *RelayHub, authToken string, logger *slog.Logger) *RelayHTTPServer {
	if logger == nil {
		logger = slog.Default()
	}
	s := &RelayHTTPServer{
		addr:      addr,
		hub:       hub,
		authToken: authToken,
		logger:    logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/bridge/session", s.handleSession)
	mux.HandleFunc("/v1/bridge/session/", s.handleSessionPath)
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.logRequests(mux),
	}
	return s
}

func (s *RelayHTTPServer) Handler() http.Handler {
	return s.server.Handler
}

func (s *RelayHTTPServer) Start() error {
	s.logger.Info("starting bridge relay http server", "addr", s.addr)
	err := s.server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *RelayHTTPServer) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *RelayHTTPServer) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := readAuthToken(r)
	if s.authToken != "" && token != s.authToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload struct {
		ClientName string `json:"clientName"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)

	sessionID, err := s.hub.OpenSession(token, payload.ClientName)
	if err != nil {
		if errors.Is(err, ErrRelayAuthFailure) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "failed to open session", http.StatusInternalServerError)
		return
	}
	s.logger.Info("relay session opened", "session_id", sessionID, "client_name", payload.ClientName)
	writeJSON(w, map[string]any{
		"sessionId": sessionID,
	})
}

func (s *RelayHTTPServer) handleSessionPath(w http.ResponseWriter, r *http.Request) {
	if s.authToken != "" {
		token := readAuthToken(r)
		if token != s.authToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/bridge/session/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	sessionID := parts[0]
	action := parts[1]

	switch action {
	case "read":
		s.handleRead(w, r, sessionID)
	case "write":
		s.handleWrite(w, r, sessionID)
	case "close":
		s.handleClose(w, r, sessionID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *RelayHTTPServer) handleRead(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	timeout := 25 * time.Second
	if raw := r.URL.Query().Get("timeout_ms"); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			timeout = time.Duration(ms) * time.Millisecond
		}
	}
	s.logger.Debug("relay read poll", "session_id", sessionID, "timeout", timeout)
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	line, err := s.hub.ReadForPlugin(ctx, sessionID)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			s.logger.Debug("relay read poll timeout (no data)", "session_id", sessionID)
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, ErrRelayNoSession), errors.Is(err, ErrRelayBadSession), errors.Is(err, ErrRelayExpired):
			s.logger.Warn("relay read session error", "session_id", sessionID, "error", err)
			http.Error(w, "session not found", http.StatusNotFound)
		default:
			http.Error(w, "read failed", http.StatusInternalServerError)
		}
		return
	}
	s.logger.Debug("relay read delivered", "session_id", sessionID, "line_len", len(line))
	writeJSON(w, map[string]any{
		"line": line,
	})
}

func (s *RelayHTTPServer) handleWrite(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Line string `json:"line"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if payload.Line == "" {
		http.Error(w, "line is required", http.StatusBadRequest)
		return
	}
	if err := s.hub.WriteFromPlugin(r.Context(), sessionID, payload.Line); err != nil {
		if errors.Is(err, ErrRelayNoSession) || errors.Is(err, ErrRelayBadSession) || errors.Is(err, ErrRelayExpired) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("write failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *RelayHTTPServer) handleClose(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ok := s.hub.CloseSession(sessionID); !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *RelayHTTPServer) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.logger.Info("relay http request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func readAuthToken(r *http.Request) string {
	if token := r.Header.Get("X-Gox-Auth"); token != "" {
		return token
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
