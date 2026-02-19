package action

import (
	"errors"
	"fmt"
)

type ValidationError struct {
	Path    string
	Rule    string
	Message string
}

func (e ValidationError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

type SchemaError struct {
	Message string
}

func (e SchemaError) Error() string {
	return "invalid schema: " + e.Message
}

func AsValidationError(err error) (ValidationError, bool) {
	var target ValidationError
	if errors.As(err, &target) {
		return target, true
	}
	return ValidationError{}, false
}

func AsSchemaError(err error) (SchemaError, bool) {
	var target SchemaError
	if errors.As(err, &target) {
		return target, true
	}
	return SchemaError{}, false
}
