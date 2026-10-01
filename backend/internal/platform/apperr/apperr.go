// Package apperr defines transport-agnostic application errors. Services return
// them; each interface adapter (REST today, MCP later) maps them to its own
// error representation.
package apperr

import (
	"errors"
	"fmt"
)

// Kind classifies an application error.
type Kind int

const (
	// KindValidation means the input violates a domain rule.
	KindValidation Kind = iota + 1
	// KindNotFound means the addressed resource does not exist.
	KindNotFound
	// KindInvalidDocument means an uploaded document could not be read at all.
	KindInvalidDocument
)

// FieldError points a validation problem at a specific input field.
type FieldError struct {
	Field   string
	Message string
}

// Error is an application error with a kind, a human message and optional field errors.
type Error struct {
	Kind    Kind
	Message string
	Fields  []FieldError
}

func (e *Error) Error() string { return e.Message }

// ValidationFailed is the detail of every request validation error that lists field errors.
const ValidationFailed = "request validation failed"

// Validation builds a KindValidation error.
func Validation(message string, fields ...FieldError) error {
	return &Error{Kind: KindValidation, Message: message, Fields: fields}
}

// NotFound builds a KindNotFound error.
func NotFound(format string, args ...any) error {
	return &Error{Kind: KindNotFound, Message: fmt.Sprintf(format, args...)}
}

// InvalidDocument builds a KindInvalidDocument error.
func InvalidDocument(format string, args ...any) error {
	return &Error{Kind: KindInvalidDocument, Message: fmt.Sprintf(format, args...)}
}

// As extracts an *Error from err's chain.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// Validator accumulates field errors.
type Validator struct {
	fields []FieldError
}

// Check records message for field when ok is false.
func (v *Validator) Check(ok bool, field, message string) {
	if !ok {
		v.fields = append(v.fields, FieldError{Field: field, Message: message})
	}
}

// Err returns a validation error when any check failed, otherwise nil.
func (v *Validator) Err() error {
	if len(v.fields) == 0 {
		return nil
	}
	return Validation(ValidationFailed, v.fields...)
}
