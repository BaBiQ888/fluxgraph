package observability

import (
	"net/http"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Token consumption metrics
	llmInputTokens = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fluxgraph_llm_input_tokens_total",
			Help: "Total number of input tokens consumed by LLM nodes.",
		},
		[]string{"tenant_id", "model"},
	)
	llmOutputTokens = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fluxgraph_llm_output_tokens_total",
			Help: "Total number of output tokens consumed by LLM nodes.",
		},
		[]string{"tenant_id", "model"},
	)

	// Latency metrics
	nodeDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "fluxgraph_node_duration_seconds",
			Help:    "Histogram of node execution duration in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{"tenant_id", "node_id"},
	)

	// Tool execution metrics
	toolCallsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fluxgraph_tool_calls_total",
			Help: "Total number of tool calls processed.",
		},
		[]string{"tool_name", "status"},
	)

	// System metrics
	activeSessions = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "fluxgraph_active_sessions",
			Help: "Number of currently active agent sessions.",
		},
		[]string{"tenant_id"},
	)
)

func init() {
	prometheus.MustRegister(llmInputTokens)
	prometheus.MustRegister(llmOutputTokens)
	prometheus.MustRegister(nodeDuration)
	prometheus.MustRegister(toolCallsTotal)
	prometheus.MustRegister(activeSessions)
}

// PrometheusMetricHook implements engine.LifecycleHook to export Prometheus metrics.
type PrometheusMetricHook struct {
	// Add state if needed
}

func NewPrometheusMetricHook() *PrometheusMetricHook {
	return &PrometheusMetricHook{}
}

func (h *PrometheusMetricHook) OnHook(state *core.AgentState, meta engine.HookMeta) {
	tenantID := "default"
	if state != nil && state.Variables != nil {
		if tid, ok := state.Variables["tenant_id"].(string); ok && tid != "" {
			tenantID = tid
		}
	}

	switch meta.Point {
	case engine.HookAfterNode:
		nodeDuration.WithLabelValues(tenantID, meta.NodeID).Observe(meta.Elapsed.Seconds())
		
		// Record token usage if stashed in variables
		if state != nil && state.Variables != nil {
			if usage, ok := state.Variables["__token_usage__"].(map[string]any); ok {
				model, _ := state.Variables["model_name"].(string)
				if model == "" { model = "unknown" }
				
				if in, ok := usage["in"].(int); ok {
					llmInputTokens.WithLabelValues(tenantID, model).Add(float64(in))
				}
				if out, ok := usage["out"].(int); ok {
					llmOutputTokens.WithLabelValues(tenantID, model).Add(float64(out))
				}
			}
		}

	case engine.HookOnError:
		// Logic for recording errors could go here
	}
}

// MetricsHandler returns an HTTP handler for Prometheus scraping.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
