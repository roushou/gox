package roblox

import (
	"errors"
	"fmt"
)

var (
	ErrNotConnected = errors.New("roblox bridge is not connected")
)

type ProtocolError struct {
	Message string
}

func (e ProtocolError) Error() string {
	return "protocol error: " + e.Message
}

type BridgeCallError struct {
	Code          string
	Message       string
	Retryable     bool
	CorrelationID string
	Details       map[string]any
}

func (e BridgeCallError) Error() string {
	return fmt.Sprintf("bridge call failed (%s): %s", e.Code, e.Message)
}
