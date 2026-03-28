package observability

import (
	"context"
	"fmt"
	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

// InitTracer initializes an OTel TracerProvider.
func InitTracer(serviceName string) (*sdktrace.TracerProvider, error) {
	// For local development, we use a simple console exporter if no OTLP endpoint is provided.
	// In production, we would use an OTLP exporter here.
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			attribute.String("environment", "production"),
		),
	)
	if err != nil {
		log.Error().Err(err).Msg("failed to create otel resource")
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(nil), // In real use, inject a real exporter here
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return tp, nil
}

// OtelTracingHook implements engine.LifecycleHook to provide full execution spans.
type OtelTracingHook struct {
	tracer trace.Tracer
	spans  map[string]trace.Span
}

func NewOtelTracingHook() *OtelTracingHook {
	return &OtelTracingHook{
		tracer: otel.Tracer("fluxgraph-engine"),
		spans:  make(map[string]trace.Span),
	}
}

func (h *OtelTracingHook) OnHook(state *core.AgentState, meta engine.HookMeta) {
	ctx := context.Background() // Hook interface doesn't pass context yet, we should ideally use trace from state
	
	switch meta.Point {
	case engine.HookBeforeNode:
		// Get tenantID from variables
		tenantID, _ := state.Variables["tenant_id"].(string)
		if tenantID == "" { tenantID = "default" }

		// Start a new span for the node
		_, span := h.tracer.Start(ctx, fmt.Sprintf("node.%s", meta.NodeID),
			trace.WithAttributes(
				attribute.String("fluxgraph.node_id", meta.NodeID),
				attribute.Int("fluxgraph.step_count", meta.StepCount),
				attribute.String("fluxgraph.tenant_id", tenantID),
				attribute.String("fluxgraph.task_id", state.TaskID),
				attribute.String("fluxgraph.context_id", state.ContextID),
			))
		h.spans[meta.NodeID] = span

	case engine.HookAfterNode:
		if span, ok := h.spans[meta.NodeID]; ok {
			span.End()
			delete(h.spans, meta.NodeID)
		}

	case engine.HookOnError:
		if span, ok := h.spans[meta.NodeID]; ok {
			span.RecordError(meta.Err)
			span.SetAttributes(attribute.Bool("error", true))
			span.End()
			delete(h.spans, meta.NodeID)
		}
	}
}

// GetTextMapPropagator returns the global propagator for A2A cross-service tracing.
func GetTextMapPropagator() propagation.TextMapPropagator {
	return otel.GetTextMapPropagator()
}
