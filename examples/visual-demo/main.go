// Package main is a zero-dependency, browser-visualized demo for FluxGraph.
//
// It boots an HTTP server on :8080 that:
//   - serves a single-page UI rendering the graph topology via vis-network
//   - exposes /api/run to start a session; the engine runs in a goroutine
//   - streams LifecycleHook events over Server-Sent Events so each node
//     visually lights up in real time as it executes
//   - supports human-in-the-loop: when the "escalate" branch fires a
//     HumanNeeded error, the engine pauses and the UI shows Approve/Reject
//
// Run:  go run ./examples/visual-demo   then open http://localhost:8080
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
	"github.com/FluxGraph/fluxgraph/graph"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/FluxGraph/fluxgraph/memory"
	"github.com/FluxGraph/fluxgraph/tools"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

//go:embed static
var staticFS embed.FS

// ────────────────────────────────────────────────────────────────────────────
// Nodes
// ────────────────────────────────────────────────────────────────────────────

const userMessageKey = "__user_message__"

// intakeNode pulls the raw user message out of the bootstrap variables and
// appends it as a proper RoleUser core.Message so downstream nodes see a
// uniform Messages slice.
type intakeNode struct{}

func (n *intakeNode) ID() string { return "intake" }
func (n *intakeNode) Process(_ context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	time.Sleep(150 * time.Millisecond)
	text, _ := state.GetStringVariable(userMessageKey)
	ns := state.WithMessage(core.Message{
		Role:      core.RoleUser,
		Parts:     []core.Part{{Type: core.PartTypeText, Text: text}},
		Timestamp: time.Now(),
	})
	return &interfaces.NodeResult{State: ns}, nil
}

// classifyNode pretends to be an LLM-based intent classifier. To keep the
// demo deterministic and offline it uses keyword matching, but the shape
// mirrors what a real implementation would do (call LLM → parse → set var).
type classifyNode struct{}

func (n *classifyNode) ID() string { return "classify" }
func (n *classifyNode) Process(_ context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	time.Sleep(350 * time.Millisecond) // simulate model latency

	msg := strings.ToLower(state.LastMessage().Parts[0].Text)
	intent := "escalate"
	switch {
	case strings.Contains(msg, "退款") || strings.Contains(msg, "refund"):
		intent = "refund"
	case strings.Contains(msg, "订单") || strings.Contains(msg, "order") || strings.Contains(msg, "查"):
		intent = "query"
	}

	ns := state.WithVariable("intent", intent).WithMessage(core.Message{
		Role:      core.RoleAssistant,
		Parts:     []core.Part{{Type: core.PartTypeText, Text: fmt.Sprintf("(internal) classified intent → %s", intent)}},
		Timestamp: time.Now(),
	})
	return &interfaces.NodeResult{State: ns}, nil
}

// toolCallNode demonstrates a node that issues a ToolCall, executes it
// through the registry, and writes the ToolResult back to state.
type toolCallNode struct {
	id       string
	toolName string
	argsFn   func(state *core.AgentState) map[string]any
	registry interfaces.ToolRegistry
}

func (n *toolCallNode) ID() string { return n.id }
func (n *toolCallNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	time.Sleep(250 * time.Millisecond)
	callID := uuid.New().String()[:8]
	args := n.argsFn(state)

	callMsg := core.Message{
		Role: core.RoleAssistant,
		Parts: []core.Part{{
			Type:     core.PartTypeToolCall,
			ToolCall: &core.ToolCallPart{CallID: callID, ToolName: n.toolName, Arguments: args},
		}},
		Timestamp: time.Now(),
	}

	results := n.registry.ExecuteConcurrent(ctx, []core.ToolCallPart{
		{CallID: callID, ToolName: n.toolName, Arguments: args},
	})
	resMsg := core.Message{
		Role:      core.RoleTool,
		Parts:     []core.Part{{Type: core.PartTypeToolResult, ToolResult: &results[0]}},
		Timestamp: time.Now(),
	}

	ns := state.WithMessage(callMsg).WithMessage(resMsg)
	return &interfaces.NodeResult{State: ns}, nil
}

// respondNode crafts a final user-facing reply by reading a previous tool
// result (if present) and templating it into a friendly Assistant message.
type respondNode struct {
	id   string
	tmpl func(state *core.AgentState) string
}

func (n *respondNode) ID() string { return n.id }
func (n *respondNode) Process(_ context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	time.Sleep(200 * time.Millisecond)
	reply := n.tmpl(state)
	ns := state.WithMessage(core.Message{
		Role:      core.RoleAssistant,
		Parts:     []core.Part{{Type: core.PartTypeText, Text: reply}},
		Timestamp: time.Now(),
	})
	return &interfaces.NodeResult{State: ns}, nil
}

// humanReviewNode demonstrates the Human-in-the-Loop pattern: it returns
// ErrCategoryHumanNeeded the first time, which the engine converts to a
// StatusPaused. On Resume(approved=true) it lets execution flow downstream.
type humanReviewNode struct{}

func (n *humanReviewNode) ID() string { return "human_review" }
func (n *humanReviewNode) Process(_ context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	time.Sleep(200 * time.Millisecond)
	if approved, _ := state.Variables["approved"].(bool); approved {
		ns := state.WithMessage(core.Message{
			Role:      core.RoleSystem,
			Parts:     []core.Part{{Type: core.PartTypeText, Text: "human approved → unblocking escalation"}},
			Timestamp: time.Now(),
		})
		return &interfaces.NodeResult{State: ns}, nil
	}
	return nil, &core.AgentError{
		Category: core.ErrCategoryHumanNeeded,
		NodeID:   "human_review",
		Cause:    errors.New("requires supervisor approval before escalating"),
	}
}

// ────────────────────────────────────────────────────────────────────────────
// SSE hub + visualization hook
// ────────────────────────────────────────────────────────────────────────────

type sseEvent struct {
	Type     string           `json:"type"`
	NodeID   string           `json:"nodeID,omitempty"`
	Step     int              `json:"step"`
	Elapsed  string           `json:"elapsed,omitempty"`
	Snapshot *snapshotPayload `json:"snapshot,omitempty"`
	Error    string           `json:"error,omitempty"`
}

type snapshotPayload struct {
	Messages  []messagePayload `json:"messages"`
	Variables map[string]any   `json:"variables"`
	Status    string           `json:"status"`
}

type messagePayload struct {
	Role string `json:"role"`
	Kind string `json:"kind"` // "text" | "tool_call" | "tool_result" | "system"
	Text string `json:"text"`
}

func snapshotOf(state *core.AgentState) *snapshotPayload {
	if state == nil {
		return nil
	}
	msgs := make([]messagePayload, 0, len(state.Messages))
	for _, m := range state.Messages {
		for _, p := range m.Parts {
			mp := messagePayload{Role: string(m.Role)}
			switch p.Type {
			case core.PartTypeText:
				mp.Kind = "text"
				mp.Text = p.Text
			case core.PartTypeToolCall:
				mp.Kind = "tool_call"
				if p.ToolCall != nil {
					b, _ := json.Marshal(p.ToolCall.Arguments)
					mp.Text = fmt.Sprintf("→ %s(%s)", p.ToolCall.ToolName, string(b))
				}
			case core.PartTypeToolResult:
				mp.Kind = "tool_result"
				if p.ToolResult != nil {
					mp.Text = "← " + p.ToolResult.Result
				}
			default:
				continue
			}
			msgs = append(msgs, mp)
		}
	}
	vars := make(map[string]any, len(state.Variables))
	for k, v := range state.Variables {
		if strings.HasPrefix(k, "__") {
			continue
		}
		vars[k] = v
	}
	return &snapshotPayload{Messages: msgs, Variables: vars, Status: string(state.Status)}
}

type sseHub struct {
	mu  sync.RWMutex
	chs map[string]chan sseEvent
}

func newSSEHub() *sseHub { return &sseHub{chs: make(map[string]chan sseEvent)} }

func (h *sseHub) subscribe(sessionID string) chan sseEvent {
	ch := make(chan sseEvent, 32)
	h.mu.Lock()
	h.chs[sessionID] = ch
	h.mu.Unlock()
	return ch
}

func (h *sseHub) channel(sessionID string) (chan sseEvent, bool) {
	h.mu.RLock()
	ch, ok := h.chs[sessionID]
	h.mu.RUnlock()
	return ch, ok
}

func (h *sseHub) publish(sessionID string, ev sseEvent) {
	if ch, ok := h.channel(sessionID); ok {
		select {
		case ch <- ev:
		default: // drop if backed up — visualization is best-effort
		}
	}
}

func (h *sseHub) close(sessionID string) {
	h.mu.Lock()
	if ch, ok := h.chs[sessionID]; ok {
		close(ch)
		delete(h.chs, sessionID)
	}
	h.mu.Unlock()
}

// vizHook implements engine.LifecycleHook and pipes every lifecycle point
// for one session into the SSE hub.
type vizHook struct {
	sessionID string
	hub       *sseHub
}

func (h *vizHook) OnHook(state *core.AgentState, meta engine.HookMeta) {
	ev := sseEvent{
		NodeID:   meta.NodeID,
		Step:     meta.StepCount,
		Snapshot: snapshotOf(state),
	}
	switch meta.Point {
	case engine.HookBeforeNode:
		ev.Type = "node_enter"
	case engine.HookAfterNode:
		ev.Type = "node_exit"
		ev.Elapsed = meta.Elapsed.String()
	case engine.HookOnError:
		ev.Type = "node_error"
		if meta.Err != nil {
			ev.Error = meta.Err.Error()
		}
	}
	h.hub.publish(h.sessionID, ev)
}

// ────────────────────────────────────────────────────────────────────────────
// Graph construction
// ────────────────────────────────────────────────────────────────────────────

func buildGraph(reg interfaces.ToolRegistry) *graph.Graph {
	b := graph.NewBuilder()

	must(b.AddNode(&intakeNode{}))
	must(b.AddNode(&classifyNode{}))
	must(b.AddNode(&toolCallNode{
		id: "refund_tool", toolName: "echo", registry: reg,
		argsFn: func(s *core.AgentState) map[string]any {
			return map[string]any{"message": "REFUND_OK | amount=99.00 | order=" + extractOrderID(s)}
		},
	}))
	must(b.AddNode(&toolCallNode{
		id: "query_tool", toolName: "echo", registry: reg,
		argsFn: func(s *core.AgentState) map[string]any {
			return map[string]any{"message": "ORDER_STATUS | order=" + extractOrderID(s) + " | status=SHIPPED | tracking=SF1234567"}
		},
	}))
	must(b.AddNode(&humanReviewNode{}))

	must(b.AddNode(&respondNode{
		id: "respond_refund",
		tmpl: func(s *core.AgentState) string {
			return "✅ 已为您发起退款，款项将在 3–5 个工作日内原路返回。\n详情: " + lastToolResult(s)
		},
	}))
	must(b.AddNode(&respondNode{
		id: "respond_query",
		tmpl: func(s *core.AgentState) string {
			return "📦 您的订单状态如下：\n" + lastToolResult(s)
		},
	}))
	must(b.AddNode(&respondNode{
		id: "respond_escalated",
		tmpl: func(_ *core.AgentState) string {
			return "👨‍💼 已为您转接到人工客服，工号 #A042 将在 5 分钟内联系您。"
		},
	}))

	b.SetEntry("intake")
	b.AddEdge("intake", "classify")
	b.AddConditionalEdge("classify", func(_ context.Context, s *core.AgentState) (string, error) {
		switch s.Variables["intent"] {
		case "refund":
			return "refund_tool", nil
		case "query":
			return "query_tool", nil
		default:
			return "human_review", nil
		}
	})
	b.AddEdge("refund_tool", "respond_refund")
	b.AddEdge("query_tool", "respond_query")
	b.AddEdge("human_review", "respond_escalated")
	b.SetTerminal("respond_refund", "respond_query", "respond_escalated")

	g, err := b.Build()
	if err != nil {
		log.Fatalf("graph build failed: %v", err)
	}
	return g
}

func extractOrderID(s *core.AgentState) string {
	text, _ := s.GetStringVariable(userMessageKey)
	// Tolerate Chinese commas / full-width punctuation around the ID.
	text = strings.NewReplacer("，", " ", ",", " ", "。", " ").Replace(text)
	for _, w := range strings.Fields(text) {
		if strings.HasPrefix(w, "#") {
			return strings.TrimPrefix(w, "#")
		}
	}
	return "UNKNOWN"
}

func lastToolResult(s *core.AgentState) string {
	for i := len(s.Messages) - 1; i >= 0; i-- {
		for _, p := range s.Messages[i].Parts {
			if p.Type == core.PartTypeToolResult && p.ToolResult != nil {
				return p.ToolResult.Result
			}
		}
	}
	return ""
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// HTTP handlers
// ────────────────────────────────────────────────────────────────────────────

type server struct {
	g     *graph.Graph
	store interfaces.MemoryStore
	hub   *sseHub
}

func (s *server) handleGraph(w http.ResponseWriter, _ *http.Request) {
	type nodeAPI struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Kind  string `json:"kind"`
	}
	type edgeAPI struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Label string `json:"label,omitempty"`
	}

	kinds := map[string]string{
		"intake": "entry", "classify": "llm",
		"refund_tool": "tool", "query_tool": "tool",
		"human_review":      "human",
		"respond_refund":    "terminal",
		"respond_query":     "terminal",
		"respond_escalated": "terminal",
	}

	nodes := make([]nodeAPI, 0, len(s.g.Nodes))
	for id := range s.g.Nodes {
		nodes = append(nodes, nodeAPI{ID: id, Label: id, Kind: kinds[id]})
	}

	// Static edges from the graph, plus the three conditional branches that
	// RouterFunc resolves at runtime (not introspectable, so listed explicitly).
	edges := []edgeAPI{}
	for _, e := range s.g.Edges {
		if !e.IsCond {
			edges = append(edges, edgeAPI{From: e.FromID, To: e.ToID})
		}
	}
	edges = append(edges,
		edgeAPI{From: "classify", To: "refund_tool", Label: "intent=refund"},
		edgeAPI{From: "classify", To: "query_tool", Label: "intent=query"},
		edgeAPI{From: "classify", To: "human_review", Label: "intent=escalate"},
	)

	writeJSON(w, map[string]any{"nodes": nodes, "edges": edges})
}

type runRequest struct {
	Message string `json:"message"`
}

func (s *server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message required", http.StatusBadRequest)
		return
	}
	sessionID := uuid.New().String()
	s.hub.subscribe(sessionID)

	initial := core.NewState().WithVariable(userMessageKey, req.Message)
	eng := engine.NewEngine(s.g, s.store, nil,
		engine.WithHooks(&vizHook{sessionID: sessionID, hub: s.hub}),
	)

	go s.runEngine(sessionID, eng, func(ctx context.Context) (*core.AgentState, error) {
		return eng.Start(ctx, sessionID, initial)
	})

	writeJSON(w, map[string]string{"sessionID": sessionID})
}

type resumeRequest struct {
	SessionID string `json:"sessionID"`
	Approved  bool   `json:"approved"`
}

func (s *server) handleResume(w http.ResponseWriter, r *http.Request) {
	var req resumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" {
		http.Error(w, "sessionID required", http.StatusBadRequest)
		return
	}
	if _, ok := s.hub.channel(req.SessionID); !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	eng := engine.NewEngine(s.g, s.store, nil,
		engine.WithHooks(&vizHook{sessionID: req.SessionID, hub: s.hub}),
	)
	go s.runEngine(req.SessionID, eng, func(ctx context.Context) (*core.AgentState, error) {
		return eng.Resume(ctx, req.SessionID, map[string]any{"approved": req.Approved})
	})

	w.WriteHeader(http.StatusAccepted)
}

func (s *server) runEngine(sessionID string, _ *engine.Engine, fn func(context.Context) (*core.AgentState, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	final, err := fn(ctx)
	if err != nil {
		s.hub.publish(sessionID, sseEvent{
			Type:     "failed",
			Error:    err.Error(),
			Snapshot: snapshotOf(final),
		})
		time.Sleep(100 * time.Millisecond)
		s.hub.close(sessionID)
		return
	}

	switch final.Status {
	case core.StatusPaused:
		s.hub.publish(sessionID, sseEvent{
			Type: "paused", NodeID: final.LastNodeID, Snapshot: snapshotOf(final),
		})
	case core.StatusCompleted:
		s.hub.publish(sessionID, sseEvent{
			Type: "completed", NodeID: final.LastNodeID, Snapshot: snapshotOf(final),
		})
		time.Sleep(200 * time.Millisecond)
		s.hub.close(sessionID)
	default:
		s.hub.publish(sessionID, sseEvent{
			Type: "failed", Snapshot: snapshotOf(final), Error: "unexpected status: " + string(final.Status),
		})
		s.hub.close(sessionID)
	}
}

func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	ch, ok := s.hub.channel(sessionID)
	if !ok {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// ────────────────────────────────────────────────────────────────────────────
// main
// ────────────────────────────────────────────────────────────────────────────

func main() {
	reg := tools.NewConcreteToolRegistry()
	must(reg.Register(&tools.EchoTool{}))

	srv := &server{
		g:     buildGraph(reg),
		store: memory.NewInMemoryStore(),
		hub:   newSSEHub(),
	}

	r := chi.NewRouter()

	staticContent, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}
	r.Handle("/*", http.FileServer(http.FS(staticContent)))

	r.Get("/api/graph", srv.handleGraph)
	r.Post("/api/run", srv.handleRun)
	r.Post("/api/resume", srv.handleResume)
	r.Get("/api/events", srv.handleEvents)

	addr := ":8080"
	log.Printf("FluxGraph visual demo → http://localhost%s", addr)
	httpSrv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
