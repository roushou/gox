package roblox

import (
	"errors"
	"fmt"

	"github.com/roushou/gox/internal/domain/fault"
)

func ToFault(err error, correlationID string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotConnected) {
		return fault.New(
			fault.CodeInternal,
			fault.CategoryPlatform,
			"roblox bridge not connected",
		).WithCorrelationID(correlationID)
	}

	var protocolErr ProtocolError
	if errors.As(err, &protocolErr) {
		return fault.New(
			fault.CodeInternal,
			fault.CategoryPlatform,
			protocolErr.Message,
		).WithCorrelationID(correlationID)
	}

	var bridgeErr BridgeCallError
	if errors.As(err, &bridgeErr) {
		mapped := mapBridgeCode(bridgeErr.Code)
		f := fault.New(mapped, fault.CategoryRuntime, bridgeErr.Message)
		f.Retryable = bridgeErr.Retryable
		if bridgeErr.Code == "TRANSIENT_ERROR" && !f.Retryable {
			f.Retryable = true
		}
		f.Details = bridgeErr.Details
		if bridgeErr.CorrelationID != "" {
			return f.WithCorrelationID(bridgeErr.CorrelationID)
		}
		return f.WithCorrelationID(correlationID)
	}

	return fault.New(
		fault.CodeInternal,
		fault.CategoryPlatform,
		fmt.Sprintf("roblox bridge call failed: %v", err),
	).WithCorrelationID(correlationID)
}

func mapBridgeCode(code string) fault.Code {
	switch code {
	case "VALIDATION_ERROR":
		return fault.CodeValidation
	case "ACCESS_DENIED":
		return fault.CodeDenied
	case "NOT_FOUND":
		return fault.CodeNotFound
	case "CONFLICT":
		return fault.CodeDenied
	case "TRANSIENT_ERROR":
		return fault.CodeInternal
	default:
		return fault.CodeInternal
	}
}
