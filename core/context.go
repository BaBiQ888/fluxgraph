package core

import (
	"context"
	"errors"
	"time"
)

var ErrMissingTenantID = errors.New("tenantID is required")

type ContextKey string

const (
	SessionContextKey ContextKey = "session_id"
	TenantContextKey  ContextKey = "tenant_id"
)

// ExecutionContext wraps standard context with framework-specific routing and tracing metadata.
type ExecutionContext struct {
	context.Context
	cancelFunc context.CancelFunc

	TraceID   string
	SpanID    string
	TenantID  string
	SessionID string
	Metadata  map[string]any
}

// NewContext creates a new ExecutionContext. TenantID is required.
func NewContext(parent context.Context, tenantID string, timeout time.Duration) (*ExecutionContext, error) {
	if tenantID == "" {
		return nil, ErrMissingTenantID
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	return &ExecutionContext{
		Context:    ctx,
		cancelFunc: cancel,
		TenantID:   tenantID,
		Metadata:   make(map[string]any),
	}, nil
}

// Derive creates a child ExecutionContext inheriting TenantID and Trace context.
func (c *ExecutionContext) Derive(timeout time.Duration) *ExecutionContext {
	var ctx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(c.Context, timeout)
	} else {
		ctx, cancel = context.WithCancel(c.Context)
	}
	
	newMetadata := make(map[string]any, len(c.Metadata))
	for k, v := range c.Metadata {
		newMetadata[k] = v
	}

	return &ExecutionContext{
		Context:    ctx,
		cancelFunc: cancel,
		TraceID:    c.TraceID,
		SpanID:     c.SpanID,
		TenantID:   c.TenantID,
		SessionID:  c.SessionID,
		Metadata:   newMetadata,
	}
}

// Cancel manually triggers context cancellation.
func (c *ExecutionContext) Cancel() {
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
}
