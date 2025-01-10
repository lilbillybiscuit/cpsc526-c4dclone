package src

import (
	"fmt"
	"time"
)

// Custom error types
type ErrorType int

const (
	ErrorTypeConfiguration ErrorType = iota
	ErrorTypeConnection
	ErrorTypeState
	ErrorTypeValidation
	ErrorTypeInternal
)

type C4DError struct {
	Type    ErrorType
	Message string
	Cause   error
	Time    time.Time
}

func (e *C4DError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func NewError(errType ErrorType, message string, cause error) *C4DError {
	return &C4DError{
		Type:    errType,
		Message: message,
		Cause:   cause,
		Time:    time.Now(),
	}
}

// Error constructors
func NewConfigurationError(message string, cause error) error {
	return NewError(ErrorTypeConfiguration, message, cause)
}

func NewConnectionError(message string, cause error) error {
	return NewError(ErrorTypeConnection, message, cause)
}

func NewStateError(message string, cause error) error {
	return NewError(ErrorTypeState, message, cause)
}

func NewValidationError(message string, cause error) error {
	return NewError(ErrorTypeValidation, message, cause)
}

func NewInternalError(message string, cause error) error {
	return NewError(ErrorTypeInternal, message, cause)
}

// Error checking helpers
func IsConfigurationError(err error) bool {
	if c4dErr, ok := err.(*C4DError); ok {
		return c4dErr.Type == ErrorTypeConfiguration
	}
	return false
}

func IsConnectionError(err error) bool {
	if c4dErr, ok := err.(*C4DError); ok {
		return c4dErr.Type == ErrorTypeConnection
	}
	return false
}

func IsStateError(err error) bool {
	if c4dErr, ok := err.(*C4DError); ok {
		return c4dErr.Type == ErrorTypeState
	}
	return false
}
