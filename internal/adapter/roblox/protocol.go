package roblox

import (
	"errors"
	"fmt"
	"time"
)

type Operation string

const (
	OpAuth                 Operation = "bridge.auth"
	OpHello                Operation = "bridge.hello"
	OpPing                 Operation = "bridge.ping"
	OpScriptCreate         Operation = "script.create"
	OpScriptUpdate         Operation = "script.update"
	OpScriptDelete         Operation = "script.delete"
	OpScriptGetSource      Operation = "script.get_source"
	OpScriptExecute        Operation = "script.execute"
	OpInstanceCreate       Operation = "instance.create"
	OpInstanceSetProperty  Operation = "instance.set_property"
	OpInstanceDelete       Operation = "instance.delete"
	OpInstanceGet          Operation = "instance.get"
	OpInstanceListChildren Operation = "instance.list_children"
	OpInstanceFind         Operation = "instance.find"
)

type Request struct {
	RequestID      string         `json:"requestId"`
	CorrelationID  string         `json:"correlationId"`
	Operation      Operation      `json:"operation"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	Timestamp      time.Time      `json:"timestamp"`
}

type Response struct {
	RequestID     string         `json:"requestId"`
	CorrelationID string         `json:"correlationId"`
	Success       bool           `json:"success"`
	Payload       map[string]any `json:"payload,omitempty"`
	Error         *BridgeError   `json:"error,omitempty"`
	Timestamp     time.Time      `json:"timestamp"`
}

type BridgeError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

func (r Request) Validate() error {
	if r.RequestID == "" {
		return errors.New("requestId is required")
	}
	if r.CorrelationID == "" {
		return errors.New("correlationId is required")
	}
	if r.Operation == "" {
		return errors.New("operation is required")
	}
	if r.Timestamp.IsZero() {
		return errors.New("timestamp is required")
	}
	return nil
}

func (r Response) Validate() error {
	if r.RequestID == "" {
		return errors.New("requestId is required")
	}
	if r.CorrelationID == "" {
		return errors.New("correlationId is required")
	}
	if r.Timestamp.IsZero() {
		return errors.New("timestamp is required")
	}
	if r.Success && r.Error != nil {
		return errors.New("successful response cannot include error")
	}
	if !r.Success {
		if r.Error == nil {
			return errors.New("failed response must include error")
		}
		if r.Error.Code == "" {
			return errors.New("error.code is required")
		}
		if r.Error.Message == "" {
			return errors.New("error.message is required")
		}
	}
	return nil
}

func (r Response) ValidateAgainstRequest(req Request) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.RequestID != req.RequestID {
		return fmt.Errorf("response requestId mismatch: got %q want %q", r.RequestID, req.RequestID)
	}
	if r.CorrelationID != req.CorrelationID {
		return fmt.Errorf("response correlationId mismatch: got %q want %q", r.CorrelationID, req.CorrelationID)
	}
	return nil
}
