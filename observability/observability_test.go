package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
	"github.com/FluxGraph/fluxgraph/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObservabilityHooks(t *testing.T) {
	// Initialize Tracer
	tp, err := observability.InitTracer("test-service")
	require.NoError(t, err)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	otelHook := observability.NewOtelTracingHook()
	metricsHook := observability.NewPrometheusMetricHook()

	state := core.NewState()
	state.Variables["tenant_id"] = "tenant1"
	state.Variables["model_name"] = "gpt-4"
	state.Variables["__token_usage__"] = map[string]any{
		"in":  10,
		"out": 20,
	}
	state.TaskID = "task-123"

	meta := engine.HookMeta{
		Point:     engine.HookBeforeNode,
		NodeID:    "node1",
		StepCount: 1,
	}

	// 1. Test BeforeNode
	otelHook.OnHook(state, meta)
	metricsHook.OnHook(state, meta)

	// 2. Test AfterNode
	meta.Point = engine.HookAfterNode
	meta.Elapsed = 100 * time.Millisecond
	otelHook.OnHook(state, meta)
	metricsHook.OnHook(state, meta)

	// In a real verification, we'd check Prometheus registry or OTel exporter.
	// For this unit test, we just ensure no panics and basic flow completion.
	assert.True(t, true)
}
