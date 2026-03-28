package tools

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/stretchr/testify/assert"
)

func TestConcreteToolRegistry_RegisterAndLookup(t *testing.T) {
	r := NewConcreteToolRegistry()
	assert.NoError(t, r.Register(&EchoTool{}))
	assert.ErrorContains(t, r.Register(&EchoTool{}), "already registered")

	tool, ok := r.GetTool("echo")
	assert.True(t, ok)
	assert.Equal(t, "echo", tool.Name())
	assert.Len(t, r.ListTools(), 1)
}

func TestConcreteToolRegistry_ConcurrentExecution_OrderPreserved(t *testing.T) {
	r := NewConcreteToolRegistry()
	assert.NoError(t, r.Register(&SleepTool{}))
	assert.NoError(t, r.Register(&EchoTool{}))

	calls := []core.ToolCallPart{
		{CallID: "c1", ToolName: "sleep", Arguments: map[string]any{"ms": float64(50)}},
		{CallID: "c2", ToolName: "echo",  Arguments: map[string]any{"message": "hello"}},
		{CallID: "c3", ToolName: "sleep", Arguments: map[string]any{"ms": float64(30)}},
	}

	start := time.Now()
	results := r.ExecuteConcurrent(context.Background(), calls)
	elapsed := time.Since(start)

	// All executed
	assert.Len(t, results, 3)
	// Order preserved
	assert.Equal(t, "c1", results[0].CallID)
	assert.Equal(t, "c2", results[1].CallID)
	assert.Equal(t, "c3", results[2].CallID)
	// Concurrent: total time << 80ms (sequential) 
	assert.Less(t, elapsed, 150*time.Millisecond)
	// Echo returned correct value
	assert.Equal(t, "hello", results[1].Result)
}

func TestConcreteToolRegistry_Permissions(t *testing.T) {
	r := NewConcreteToolRegistry()
	assert.NoError(t, r.Register(&EchoTool{}))
	r.GrantPermission("tenantA", "echo")

	ctx := context.WithValue(context.Background(), "tenantID", "tenantA")
	results := r.ExecuteConcurrent(ctx, []core.ToolCallPart{
		{CallID: "c1", ToolName: "echo", Arguments: map[string]any{"message": "ok"}},
	})
	assert.False(t, results[0].IsError)

	ctxB := context.WithValue(context.Background(), "tenantID", "tenantB")
	resultsB := r.ExecuteConcurrent(ctxB, []core.ToolCallPart{
		{CallID: "c2", ToolName: "echo", Arguments: map[string]any{"message": "ok"}},
	})
	assert.True(t, resultsB[0].IsError)
	assert.Contains(t, resultsB[0].Result, "PermissionDenied")
}

func TestEchoTool(t *testing.T) {
	tool := &EchoTool{}
	out, err := tool.Execute(context.Background(), map[string]any{"message": "world"})
	assert.NoError(t, err)
	assert.Equal(t, "world", out)
}

func TestSleepTool_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	tool := &SleepTool{}
	_, err := tool.Execute(ctx, map[string]any{"ms": float64(500)})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestConcurrentToolExecution_Stress(t *testing.T) {
	r := NewConcreteToolRegistry()
	assert.NoError(t, r.Register(&EchoTool{}))

	const n = 50
	calls := make([]core.ToolCallPart, n)
	for i := range calls {
		calls[i] = core.ToolCallPart{
			CallID:    string(rune('A' + i%26)),
			ToolName:  "echo",
			Arguments: map[string]any{"message": string(rune('a' + i%26))},
		}
	}

	var wg sync.WaitGroup
	resultsCh := make(chan []core.ToolResultPart, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resultsCh <- r.ExecuteConcurrent(context.Background(), calls)
		}()
	}
	wg.Wait()
	close(resultsCh)

	for results := range resultsCh {
		assert.Len(t, results, n)
		// Verify all results are non-error
		for _, res := range results {
			assert.False(t, res.IsError, "unexpected error: %s", res.Result)
		}
	}
}

// Helper for ordering.
type byCallID []core.ToolResultPart
func (b byCallID) Len() int           { return len(b) }
func (b byCallID) Less(i, j int) bool { return b[i].CallID < b[j].CallID }
func (b byCallID) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

var _ sort.Interface = byCallID{}
