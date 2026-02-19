package runlog

import (
	"errors"
	"fmt"
)

type StoreErrorCode string

const (
	StoreErrorNotFound    StoreErrorCode = "not_found"
	StoreErrorCorruptRow  StoreErrorCode = "corrupt_row"
	StoreErrorUnavailable StoreErrorCode = "unavailable"
	StoreErrorInvalidData StoreErrorCode = "invalid_data"
)

type StoreError struct {
	Code    StoreErrorCode
	Op      string
	Message string
	Cause   error
}

func (e StoreError) Error() string {
	if e.Op == "" {
		if e.Message != "" {
			return e.Message
		}
		return string(e.Code)
	}
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Op, e.Message)
	}
	return e.Op
}

func (e StoreError) Unwrap() error {
	return e.Cause
}

func NewStoreError(code StoreErrorCode, op string, message string, cause error) StoreError {
	return StoreError{
		Code:    code,
		Op:      op,
		Message: message,
		Cause:   cause,
	}
}

func AsStoreError(err error) (StoreError, bool) {
	var out StoreError
	if errors.As(err, &out) {
		return out, true
	}
	return StoreError{}, false
}

func IsStoreErrorCode(err error, code StoreErrorCode) bool {
	storeErr, ok := AsStoreError(err)
	if !ok {
		return false
	}
	return storeErr.Code == code
}
