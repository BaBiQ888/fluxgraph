package integration_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
	"github.com/FluxGraph/fluxgraph/graph"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/FluxGraph/fluxgraph/memory"
	"github.com/FluxGraph/fluxgraph/mock"
	"github.com/FluxGraph/fluxgraph/providers"
	"github.com/FluxGraph/fluxgraph/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// 1. Concurrent tool execution: total wall-clock ≈ slowest tool, not sum.
// ─────────────────────────────────────────────────────────────────────────────

func TestConcurrentTools_WallClockNearSlowest(t *testing.T) {
	reg := tools.NewConcreteToolRegistry()
	require.NoError(t, reg.Register(&tools.SleepTool{}))
	require.NoError(t, reg.Register(&tools.EchoTool{}))

	calls := []core.ToolCallPart{
		{CallID: "c1", ToolName: "sleep", Arguments: map[string]any{"ms": float64(100)}},
		{CallID: "c2", ToolName: "sleep", Arguments: map[string]any{"ms": float64(80)}},
		{CallID: "c3", ToolName: "echo",  Arguments: map[string]any{"message": "fast"}},
		{CallID: "c4", ToolName: "sleep", Arguments: map[string]any{"ms": float64(60)}},
	}

	start := time.Now()
	results := reg.ExecuteConcurrent(context.Background(), calls)
	elapsed := time.Since(start)

	// All results returned.
	require.Len(t, results, 4)
	for _, r := range results {
		assert.False(t, r.IsError, "unexpected error: %s", r.Result)
	}

	// Wall-clock should be ≈ 100 ms (slowest), not 240+ ms (sequential sum).
	// Allow generous 2× headroom for CI scheduler overhead.
	assert.Less(t, elapsed, 200*time.Millisecond,
		"concurrent execution took %v; expected near 100 ms", elapsed)

	// Result ordering preserved.
	assert.Equal(t, "c1", results[0].CallID)
	assert.Equal(t, "c2", results[1].CallID)
	assert.Equal(t, "c3", results[2].CallID)
	assert.Equal(t, "c4", results[3].CallID)
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. RetryPolicy: exhausts retries and escalates to Fatal.
// ─────────────────────────────────────────────────────────────────────────────

func TestRetryPolicy_ExhaustsAndEscalates(t *testing.T) {
	rp := engine.RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Millisecond, // fast for test
		MaxDelay:    10 * time.Millisecond,
		Multiplier:  2.0,
	}

	attempts := 0
	retriable := core.NewRetriableError("test", errors.New("flaky"), 0)
	err := rp.Execute(context.Background(), func() error {
		attempts++
		return retriable
	})

	assert.Equal(t, 3, attempts, "should attempt exactly MaxAttempts times")
	require.Error(t, err)
	var ae *core.AgentError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, core.ErrCategoryFatal, ae.Category, "should escalate to Fatal")
}

func TestRetryPolicy_SucceedsOnSecondAttempt(t *testing.T) {
	rp := engine.RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    10 * time.Millisecond,
		Multiplier:  2.0,
	}

	call := 0
	retriable := core.NewRetriableError("test", errors.New("once"), 0)
	err := rp.Execute(context.Background(), func() error {
		call++
		if call < 2 {
			return retriable
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 2, call)
}

func TestRetryPolicy_FatalNotRetried(t *testing.T) {
	rp := engine.RetryPolicy{MaxAttempts: 5, BaseDelay: 1 * time.Millisecond, Multiplier: 2.0}

	attempts := 0
	fatal := core.NewFatalError("test", errors.New("bad request"))
	err := rp.Execute(context.Background(), func() error {
		attempts++
		return fatal
	})

	assert.Equal(t, 1, attempts, "fatal error must not be retried")
	require.Error(t, err)
	var ae *core.AgentError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, core.ErrCategoryFatal, ae.Category)
}

func TestRetryPolicy_ContextCancellation(t *testing.T) {
	rp := engine.RetryPolicy{
		MaxAttempts: 10,
		BaseDelay:   50 * time.Millisecond,
		MaxDelay:    200 * time.Millisecond,
		Multiplier:  2.0,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	retriable := core.NewRetriableError("test", errors.New("slow"), 0)
	err := rp.Execute(ctx, func() error { return retriable })

	// Must exit early due to context timeout, not exhaust all 10 attempts.
	assert.Error(t, err)
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. CircuitBreaker: Closed → Open → HalfOpen → Closed.
// ─────────────────────────────────────────────────────────────────────────────

func TestCircuitBreaker_ThreeStateTransitions(t *testing.T) {
	cb := engine.NewCircuitBreaker(3, 50*time.Millisecond)

	assert.Equal(t, "Closed", cb.State())

	// Trip the breaker.
	for i := 0; i < 3; i++ {
		assert.True(t, cb.Allow())
		cb.RecordFailure()
	}
	assert.Equal(t, "Open", cb.State())

	// Requests during Open must be rejected.
	assert.False(t, cb.Allow())

	// Wait for openTimeout to elapse → HalfOpen.
	time.Sleep(60 * time.Millisecond)
	assert.True(t, cb.Allow())
	assert.Equal(t, "HalfOpen", cb.State())

	// Successful probe → back to Closed.
	cb.RecordSuccess()
	assert.Equal(t, "Closed", cb.State())
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb := engine.NewCircuitBreaker(2, 30*time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, "Open", cb.State())

	time.Sleep(40 * time.Millisecond)
	assert.True(t, cb.Allow()) // probe

	cb.RecordFailure() // probe failed
	assert.Equal(t, "Open", cb.State())
}

func TestCircuitBreaker_ConcurrentSafety(t *testing.T) {
	cb := engine.NewCircuitBreaker(100, 1*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cb.Allow() {
				cb.RecordFailure()
			}
		}()
	}
	wg.Wait()
	// No race / panic — just verify state is coherent.
	state := cb.State()
	assert.Contains(t, []string{"Closed", "Open", "HalfOpen"}, state)
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. FallbackChainProvider: skips retriable, stops on fatal.
// ─────────────────────────────────────────────────────────────────────────────

type alwaysFailProvider struct {
	category core.ErrorCategory
	calls    atomic.Int32
}

func (p *alwaysFailProvider) ModelInfo() interfaces.ModelInfo { return interfaces.ModelInfo{Name: "fail"} }
func (p *alwaysFailProvider) Generate(_ context.Context, _ *core.AgentState) (*interfaces.LLMResponse, error) {
	p.calls.Add(1)
	return nil, &core.AgentError{Category: p.category, Cause: errors.New("forced")}
}
func (p *alwaysFailProvider) GenerateStream(_ context.Context, _ *core.AgentState) (<-chan interfaces.TokenDelta, <-chan error, error) {
	p.calls.Add(1)
	return nil, nil, &core.AgentError{Category: p.category, Cause: errors.New("forced")}
}

func TestFallbackChain_SkipsRetriableUsesNext(t *testing.T) {
	primary := &alwaysFailProvider{category: core.ErrCategoryRetriable}
	good := mock.NewMockLLMProvider([]interfaces.LLMResponse{
		{Message: core.Message{Role: core.RoleAssistant, Parts: []core.Part{{Type: core.PartTypeText, Text: "ok"}}}},
	}, nil)

	chain := providers.NewFallbackChainProvider(primary, good)
	resp, err := chain.Generate(context.Background(), core.NewState())

	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Message.Parts[0].Text)
	assert.Equal(t, int32(1), primary.calls.Load())
}

func TestFallbackChain_StopsOnFatal(t *testing.T) {
	primary := &alwaysFailProvider{category: core.ErrCategoryFatal}
	secondary := &alwaysFailProvider{category: core.ErrCategoryRetriable}

	chain := providers.NewFallbackChainProvider(primary, secondary)
	_, err := chain.Generate(context.Background(), core.NewState())

	require.Error(t, err)
	// Secondary must never be called — fatal propagates immediately.
	assert.Equal(t, int32(0), secondary.calls.Load())
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Hook integration: LatencyMetricHook records per-node timings via Engine.
// ─────────────────────────────────────────────────────────────────────────────

type slowNode struct{ delay time.Duration }

func (n *slowNode) ID() string { return "SlowNode" }
func (n *slowNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	select {
	case <-time.After(n.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &interfaces.NodeResult{State: state}, nil
}

func TestLatencyHook_RecordsNodeTiming(t *testing.T) {
	latencyHook := engine.NewLatencyMetricHook()

	b := graph.NewBuilder()
	node := &slowNode{delay: 30 * time.Millisecond}
	require.NoError(t, b.AddNode(node))
	b.SetEntry("SlowNode")
	b.SetTerminal("SlowNode")
	g, err := b.Build()
	require.NoError(t, err)

	store := memory.NewInMemoryStore()
	eng := engine.NewEngine(g, store, nil, engine.WithHooks(latencyHook))

	_, err = eng.Start(context.Background(), "sess-latency", core.NewState())
	require.NoError(t, err)

	avg := latencyHook.Avg("SlowNode")
	assert.GreaterOrEqual(t, avg, 30*time.Millisecond,
		"LatencyMetricHook avg should be ≥ node delay, got %v", avg)
	assert.Equal(t, 1, latencyHook.Count["SlowNode"])
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. Hook panic isolation: panicking hook must not crash the engine.
// ─────────────────────────────────────────────────────────────────────────────

type panicHook struct{}

func (h *panicHook) OnHook(_ *core.AgentState, _ engine.HookMeta) {
	panic("intentional hook panic")
}

func TestHookPanic_DoesNotCrashEngine(t *testing.T) {
	b := graph.NewBuilder()
	require.NoError(t, b.AddNode(&slowNode{delay: 0}))
	b.SetEntry("SlowNode")
	b.SetTerminal("SlowNode")
	g, _ := b.Build()

	store := memory.NewInMemoryStore()
	eng := engine.NewEngine(g, store, nil, engine.WithHooks(&panicHook{}))

	state, err := eng.Start(context.Background(), "sess-panic", core.NewState())
	// Engine should still complete successfully despite hook panic.
	require.NoError(t, err)
	assert.Equal(t, core.StatusCompleted, state.Status)
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. HumanNeeded error auto-converts to Interrupt + Resume.
// ─────────────────────────────────────────────────────────────────────────────

type humanNeededNode struct{ called int }

func (n *humanNeededNode) ID() string { return "HNNode" }
func (n *humanNeededNode) Process(_ context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	n.called++
	if approved, _ := state.Variables["approved"].(bool); approved {
		return &interfaces.NodeResult{State: state}, nil
	}
	return nil, &core.AgentError{
		Category: core.ErrCategoryHumanNeeded,
		NodeID:   "HNNode",
		Cause:    errors.New("needs human approval"),
	}
}

func TestEngine_HumanNeededError_ConvertsToInterrupt(t *testing.T) {
	node := &humanNeededNode{}
	b := graph.NewBuilder()
	require.NoError(t, b.AddNode(node))
	b.SetEntry("HNNode")
	b.SetTerminal("HNNode")
	g, _ := b.Build()

	store := memory.NewInMemoryStore()
	eng := engine.NewEngine(g, store, nil)

	// First run: HumanNeeded → Paused.
	state, err := eng.Start(context.Background(), "sess-hn", core.NewState())
	require.NoError(t, err)
	assert.Equal(t, core.StatusPaused, state.Status)

	// Resume with approval.
	resumed, err := eng.Resume(context.Background(), "sess-hn", map[string]any{"approved": true})
	require.NoError(t, err)
	assert.Equal(t, core.StatusCompleted, resumed.Status)
}

// ─────────────────────────────────────────────────────────────────────────────
// 8. AuditLogHook: captures structured events for every node lifecycle point.
// ─────────────────────────────────────────────────────────────────────────────

func TestAuditLogHook_CapturesEvents(t *testing.T) {
	var mu sync.Mutex
	var events []engine.AuditEvent

	auditHook := engine.NewAuditLogHook(func(ev engine.AuditEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	b := graph.NewBuilder()
	require.NoError(t, b.AddNode(&slowNode{delay: 0}))
	b.SetEntry("SlowNode")
	b.SetTerminal("SlowNode")
	g, _ := b.Build()

	store := memory.NewInMemoryStore()
	eng := engine.NewEngine(g, store, nil, engine.WithHooks(auditHook))
	_, err := eng.Start(context.Background(), "sess-audit", core.NewState())
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	// Expect at least BeforeNode + AfterNode events.
	require.GreaterOrEqual(t, len(events), 2)

	points := map[engine.HookPoint]bool{}
	for _, ev := range events {
		points[ev.Point] = true
		assert.Equal(t, "SlowNode", ev.NodeID)
	}
	assert.True(t, points[engine.HookBeforeNode], "missing BeforeNode audit event")
	assert.True(t, points[engine.HookAfterNode],  "missing AfterNode audit event")
}
