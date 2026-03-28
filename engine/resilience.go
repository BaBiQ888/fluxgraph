package engine

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
)

// RetryPolicy executes fn up to MaxAttempts times using exponential back-off
// with jitter. Only core.AgentError with Category=Retriable triggers a retry.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Multiplier  float64
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   200 * time.Millisecond,
		MaxDelay:    10 * time.Second,
		Multiplier:  2.0,
	}
}

// Execute runs fn; on retriable error it sleeps then retries.
func (p RetryPolicy) Execute(ctx context.Context, fn func() error) error {
	delay := p.BaseDelay
	for attempt := 0; attempt < p.MaxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		var ae *core.AgentError
		if !errors.As(err, &ae) || ae.Category != core.ErrCategoryRetriable {
			return err // non-retriable → propagate immediately
		}

		if attempt == p.MaxAttempts-1 {
			// Escalate to fatal after exhausting retries.
			return core.NewFatalError(ae.NodeID, ae.Cause)
		}

		// Jitter ± 20 %.
		jitter := time.Duration(float64(delay) * (0.8 + 0.4*rand.Float64()))
		if jitter > p.MaxDelay {
			jitter = p.MaxDelay
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jitter):
		}

		delay = time.Duration(float64(delay) * p.Multiplier)
		if delay > p.MaxDelay {
			delay = p.MaxDelay
		}
	}
	return nil
}

// --- CircuitBreaker ---

type cbState int

const (
	cbClosed   cbState = iota // normal operation
	cbOpen                    // failing fast
	cbHalfOpen                // probing recovery
)

// CircuitBreaker guards a callable against cascading failures using a
// sliding failure-rate window with three-state machine semantics.
type CircuitBreaker struct {
	mu          sync.Mutex
	state       cbState
	failures    int
	successes   int
	threshold   int           // failures needed to open
	openTimeout time.Duration // how long to stay Open before HalfOpen
	openedAt    time.Time

	FallbackMsg string // message returned when Open
}

func NewCircuitBreaker(threshold int, openTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold:   threshold,
		openTimeout: openTimeout,
		FallbackMsg: "service temporarily unavailable, please retry later",
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case cbClosed:
		return true
	case cbOpen:
		if time.Since(cb.openedAt) >= cb.openTimeout {
			cb.state = cbHalfOpen
			return true // probe request
		}
		return false
	case cbHalfOpen:
		return true
	}
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.successes++
	cb.state = cbClosed
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.successes = 0
	if cb.state == cbHalfOpen || cb.failures >= cb.threshold {
		cb.state = cbOpen
		cb.openedAt = time.Now()
		cb.failures = 0
	}
}

func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case cbOpen:
		return "Open"
	case cbHalfOpen:
		return "HalfOpen"
	default:
		return "Closed"
	}
}
