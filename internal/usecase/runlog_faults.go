package usecase

import (
	"fmt"

	"github.com/roushou/gox/internal/domain/fault"
	"github.com/roushou/gox/internal/domain/runlog"
)

func mapRunlogPersistError(err error, op string, correlationID string, fallbackMessage string) fault.Fault {
	storeErr, ok := runlog.AsStoreError(err)
	if !ok {
		return fault.Internal(fallbackMessage).
			WithCorrelationID(correlationID).
			WithDetails(map[string]any{
				"storage_op": op,
			})
	}

	message := fallbackMessage
	code := fault.CodeInternal
	category := fault.CategoryPlatform
	retryable := false
	switch storeErr.Code {
	case runlog.StoreErrorNotFound:
		code = fault.CodeNotFound
		category = fault.CategoryClient
		message = "runlog record not found"
	case runlog.StoreErrorCorruptRow:
		message = "runlog store returned corrupted data"
	case runlog.StoreErrorUnavailable:
		message = "runlog store unavailable"
		retryable = true
	case runlog.StoreErrorInvalidData:
		code = fault.CodeValidation
		category = fault.CategoryClient
		message = "invalid runlog data"
	}
	details := map[string]any{
		"storage_code": storeErr.Code,
		"storage_op":   storeErr.Op,
	}
	if storeErr.Cause != nil {
		details["storage_cause"] = storeErr.Cause.Error()
	}
	if storeErr.Message != "" {
		details["storage_message"] = storeErr.Message
	}

	out := fault.New(code, category, message).
		WithCorrelationID(correlationID).
		WithDetails(details)
	out.Retryable = retryable
	return out
}

func mapRunlogReadError(err error, operation string) fault.Fault {
	if err == nil {
		return fault.Internal("unknown runlog read error").
			WithDetails(map[string]any{"storage_op": operation})
	}

	f := mapRunlogPersistError(err, operation, "", "runlog read failed")
	if f.Code == fault.CodeValidation {
		f.Code = fault.CodeInternal
		f.Category = fault.CategoryPlatform
		f.Message = fmt.Sprintf("runlog read failed: %s", f.Message)
	}
	return f
}
