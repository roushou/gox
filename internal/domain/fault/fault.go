package fault

import (
	"errors"
	"fmt"
)

type Code string
type Category string

const (
	CodeValidation    Code = "VALIDATION_ERROR"
	CodeNotFound      Code = "NOT_FOUND"
	CodeDenied        Code = "ACCESS_DENIED"
	CodeInternal      Code = "INTERNAL_ERROR"
	CodeUnimplemented Code = "UNIMPLEMENTED"
)

const (
	CategoryClient   Category = "client"
	CategoryPolicy   Category = "policy"
	CategoryRuntime  Category = "runtime"
	CategoryPlatform Category = "platform"
)

type Fault struct {
	Code          Code
	Category      Category
	Message       string
	Retryable     bool
	CorrelationID string
	Details       map[string]any
}

func (f Fault) Error() string {
	return fmt.Sprintf("%s: %s", f.Code, f.Message)
}

func New(code Code, category Category, message string) Fault {
	return Fault{
		Code:     code,
		Category: category,
		Message:  message,
	}
}

func Validation(message string) Fault {
	return New(CodeValidation, CategoryClient, message)
}

func NotFound(message string) Fault {
	return New(CodeNotFound, CategoryClient, message)
}

func Denied(message string) Fault {
	return New(CodeDenied, CategoryPolicy, message)
}

func Internal(message string) Fault {
	return New(CodeInternal, CategoryRuntime, message)
}

func As(err error) (Fault, bool) {
	var target Fault
	if errors.As(err, &target) {
		return target, true
	}
	return Fault{}, false
}

func (f Fault) WithCorrelationID(id string) Fault {
	f.CorrelationID = id
	return f
}

func (f Fault) WithDetails(details map[string]any) Fault {
	f.Details = details
	return f
}
