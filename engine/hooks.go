package engine

import (
	"time"

	"github.com/FluxGraph/fluxgraph/core"
)

// HookPoint marks where in the node lifecycle a hook is being called.
type HookPoint string

const (
	HookBeforeNode HookPoint = "BeforeNode"
	HookAfterNode  HookPoint = "AfterNode"
	HookOnError    HookPoint = "OnError"
)

// HookMeta carries per-invocation metadata surfaced to every hook.
type HookMeta struct {
	Point     HookPoint
	NodeID    string
	StepCount int
	Elapsed   time.Duration
	Err       error
}

// LifecycleHook is the universal hook interface; implementations must never panic the main loop.
type LifecycleHook interface {
	OnHook(state *core.AgentState, meta HookMeta)
}

// --- TokenCounterHook ---

type tokenCounter struct {
	// key: tenantID+":"+modelName
	InputTokens  map[string]int
	OutputTokens map[string]int
}

// TokenCounterHook accumulates token usage per (tenant, model) dimension.
type TokenCounterHook struct {
	counters map[string]*tokenCounter
}

func NewTokenCounterHook() *TokenCounterHook {
	return &TokenCounterHook{counters: make(map[string]*tokenCounter)}
}

func (h *TokenCounterHook) OnHook(state *core.AgentState, meta HookMeta) {
	if meta.Point != HookAfterNode {
		return
	}
	if state == nil || state.Variables == nil {
		return
	}

	type usage struct{ In, Out int }
	raw, ok := state.Variables["__token_usage__"]
	if !ok {
		return
	}
	u, ok := raw.(usage)
	if !ok {
		return
	}
	tenantID, _ := state.Variables["tenant_id"].(string)
	model, _ := state.Variables["model_name"].(string)
	key := tenantID + ":" + model
	if h.counters[key] == nil {
		h.counters[key] = &tokenCounter{
			InputTokens:  make(map[string]int),
			OutputTokens: make(map[string]int),
		}
	}
	h.counters[key].InputTokens[key]  += u.In
	h.counters[key].OutputTokens[key] += u.Out
}

// GetSummary returns total token usage per key for the session.
func (h *TokenCounterHook) GetSummary() map[string][2]int {
	out := make(map[string][2]int, len(h.counters))
	for k, v := range h.counters {
		out[k] = [2]int{v.InputTokens[k], v.OutputTokens[k]}
	}
	return out
}

// --- LatencyMetricHook ---

// LatencyMetricHook tracks per-node execution time statistics.
type LatencyMetricHook struct {
	startTimestamps map[string]time.Time
	Min             map[string]time.Duration
	Max             map[string]time.Duration
	Total           map[string]time.Duration
	Count           map[string]int
}

func NewLatencyMetricHook() *LatencyMetricHook {
	return &LatencyMetricHook{
		startTimestamps: make(map[string]time.Time),
		Min:             make(map[string]time.Duration),
		Max:             make(map[string]time.Duration),
		Total:           make(map[string]time.Duration),
		Count:           make(map[string]int),
	}
}

func (h *LatencyMetricHook) OnHook(_ *core.AgentState, meta HookMeta) {
	switch meta.Point {
	case HookBeforeNode:
		h.startTimestamps[meta.NodeID] = time.Now()
	case HookAfterNode, HookOnError:
		started, ok := h.startTimestamps[meta.NodeID]
		if !ok {
			return
		}
		elapsed := time.Since(started)
		delete(h.startTimestamps, meta.NodeID)

		h.Count[meta.NodeID]++
		h.Total[meta.NodeID] += elapsed
		if cur, exists := h.Min[meta.NodeID]; !exists || elapsed < cur {
			h.Min[meta.NodeID] = elapsed
		}
		if cur, exists := h.Max[meta.NodeID]; !exists || elapsed > cur {
			h.Max[meta.NodeID] = elapsed
		}
	}
}

func (h *LatencyMetricHook) Avg(nodeID string) time.Duration {
	if h.Count[nodeID] == 0 {
		return 0
	}
	return h.Total[nodeID] / time.Duration(h.Count[nodeID])
}

// --- AuditLogHook ---

// AuditEvent is a structured audit record emitted at critical lifecycle boundaries.
type AuditEvent struct {
	Timestamp time.Time
	TenantID  string
	SessionID string
	NodeID    string
	Point     HookPoint
	StepCount int
	Err       string
}

// AuditLogHook emits structured audit events to a configurable sink.
type AuditLogHook struct {
	Sink func(AuditEvent) // inject os.Stdout writer, log writer, or HTTP exporter
}

func NewAuditLogHook(sink func(AuditEvent)) *AuditLogHook {
	return &AuditLogHook{Sink: sink}
}

func (h *AuditLogHook) OnHook(state *core.AgentState, meta HookMeta) {
	ev := AuditEvent{
		Timestamp: time.Now(),
		NodeID:    meta.NodeID,
		Point:     meta.Point,
		StepCount: meta.StepCount,
	}
	if state != nil && state.Variables != nil {
		ev.TenantID, _ = state.Variables["tenant_id"].(string)
		ev.SessionID, _ = state.Variables["session_id"].(string)
	}
	if meta.Err != nil {
		ev.Err = meta.Err.Error()
	}
	if h.Sink != nil {
		h.Sink(ev)
	}
}

// --- ContextWindowGuardHook ---

// ContextWindowGuardHook triggers message summarization if the estimated token
// count exceeds a configurable fraction of the model's max context.
type ContextWindowGuardHook struct {
	MaxTokens    int     // model max context tokens
	ThresholdPct float64 // e.g. 0.8 → trigger at 80 %
}

func NewContextWindowGuardHook(maxTokens int, thresholdPct float64) *ContextWindowGuardHook {
	return &ContextWindowGuardHook{MaxTokens: maxTokens, ThresholdPct: thresholdPct}
}

func (h *ContextWindowGuardHook) OnHook(state *core.AgentState, meta HookMeta) {
	if meta.Point != HookBeforeNode || state == nil {
		return
	}
	estimated := h.estimateTokens(state)
	threshold := int(float64(h.MaxTokens) * h.ThresholdPct)
	if estimated >= threshold {
		// Flag for downstream aware nodes to trigger summarization.
		// Full compaction pipeline belongs to Phase 5; here we set the signal.
		if state.Variables == nil {
			return
		}
		state.Variables["__context_window_pressure__"] = true
	}
}

// estimateTokens uses the chars÷4 approximation for fast pre-request checks.
func (h *ContextWindowGuardHook) estimateTokens(state *core.AgentState) int {
	total := 0
	for _, msg := range state.Messages {
		for _, part := range msg.Parts {
			total += len(part.Text) / 4
		}
	}
	return total
}
