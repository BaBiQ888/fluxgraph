package core

import (
	"fmt"
	"time"
)

// ErrorCategory categorizes runtime failures enabling robust resilience boundaries matching Phase 2 spec definitions.
type ErrorCategory string

const (
	ErrCategoryRetriable    ErrorCategory = "Retriable"
	ErrCategoryFatal        ErrorCategory = "Fatal"
	ErrCategoryHumanNeeded  ErrorCategory = "HumanNeeded"
	ErrCategoryStateCorrupt ErrorCategory = "StateCorrupted"
)

// AgentError implements custom structured diagnostics mapped dynamically resolving execution failure modes natively without reflection.
type AgentError struct {
	Category   ErrorCategory
	NodeID     string
	Cause      error
	RetryAfter time.Duration
}

func (e *AgentError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] Node %s: %v", e.Category, e.NodeID, e.Cause)
	}
	return fmt.Sprintf("[%s] Node %s: unknown error", e.Category, e.NodeID)
}

func (e *AgentError) Unwrap() error {
	return e.Cause
}

// NewRetriableError creates an explicitly retry-awaiting diagnostic block hinting the exponential back-off module effectively.
func NewRetriableError(nodeID string, cause error, retryAfter time.Duration) *AgentError {
	return &AgentError{Category: ErrCategoryRetriable, NodeID: nodeID, Cause: cause, RetryAfter: retryAfter}
}

// NewFatalError constructs an unrecoverable signal blocking node loop advancing indefinitely.
func NewFatalError(nodeID string, cause error) *AgentError {
	return &AgentError{Category: ErrCategoryFatal, NodeID: nodeID, Cause: cause}
}
