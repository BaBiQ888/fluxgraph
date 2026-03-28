//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/FluxGraph/fluxgraph/a2a"
	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
	"github.com/FluxGraph/fluxgraph/graph"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/FluxGraph/fluxgraph/memory"
	"github.com/FluxGraph/fluxgraph/observability"
	"github.com/FluxGraph/fluxgraph/security"
	"github.com/FluxGraph/fluxgraph/tools"
)

// SimpleNode returns a static message
type SimpleNode struct {
	id string
}

func (n *SimpleNode) ID() string { return n.id }
func (n *SimpleNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	state = state.WithMessage(core.Message{
		Role:  core.RoleAssistant,
		Parts: []core.Part{{Type: core.PartTypeText, Text: "Test server is active."}},
	})
	return &interfaces.NodeResult{State: state}, nil
}

func main() {
	fmt.Println("=== FluxGraph Phase 4 Test Server Starting ===")

	// 1. Initialize Observability
	_, err := observability.InitTracer("fluxgraph-test-server")
	if err != nil {
		log.Printf("Warning: Failed to initialize tracer: %v", err)
	}

	// 2. Setup Graph
	builder := graph.NewBuilder()
	builder.AddNode(&SimpleNode{id: "StartNode"})
	builder.SetEntry("StartNode")
	builder.SetTerminal("StartNode")
	g, err := builder.Build()
	if err != nil {
		log.Fatalf("Failed to build graph: %v", err)
	}

	// 3. Setup Storage & Tools
	mem := memory.NewInMemoryStore()
	ts := &mockTaskStore{tasks: make(map[string]*core.Task)}
	reg := tools.NewConcreteToolRegistry()

	// 4. Setup Hooks
	otelHook := observability.NewOtelTracingHook()
	promHook := observability.NewPrometheusMetricHook()
	guardHook := security.NewOutputGuardHook()
	auditHook, _ := security.NewAuditLogHook("fluxgraph_security.log")

	// 5. Setup Engine with Hooks
	eng := engine.NewEngine(g, mem, nil, 
		engine.WithHooks(otelHook, promHook, guardHook, auditHook),
		engine.WithTaskStore(ts),
	)

	// 6. Setup A2A Server
	opts := a2a.ServerOptions{
		Name:        "Phase4TestAgent",
		Description: "Agent for verifying security and observability",
		Version:     "1.0.0",
		URL:         "http://localhost:8080",
		Secret:      "test-secret-key",
	}

	server := a2a.NewServer(eng, ts, mem, reg, nil, opts)

	fmt.Println("Server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", server))
}

// Minimal mock task store for testing
type mockTaskStore struct {
	tasks map[string]*core.Task
}

func (m *mockTaskStore) Create(ctx context.Context, t *core.Task) error {
	m.tasks[t.ID] = t
	return nil
}
func (m *mockTaskStore) GetByID(ctx context.Context, id string) (*core.Task, error) {
	t, ok := m.tasks[id]
	if !ok { return nil, fmt.Errorf("not found") }
	return t, nil
}
func (m *mockTaskStore) UpdateStatus(ctx context.Context, id string, s core.TaskStatus) error {
	if t, ok := m.tasks[id]; ok { t.Status = s }
	return nil
}
func (m *mockTaskStore) AppendMessage(ctx context.Context, id string, msg core.Message) error {
	if t, ok := m.tasks[id]; ok { t.History = append(t.History, msg) }
	return nil
}
func (m *mockTaskStore) AppendArtifact(ctx context.Context, id string, art core.Artifact) error {
	if t, ok := m.tasks[id]; ok { t.Artifacts = append(t.Artifacts, art) }
	return nil
}
func (m *mockTaskStore) AddWebhook(ctx context.Context, id string, c core.WebhookConfig) error {
	return nil
}
func (m *mockTaskStore) ListByContextID(ctx context.Context, cid string) ([]*core.Task, error) {
	var out []*core.Task
	for _, t := range m.tasks {
		if t.ContextID == cid { out = append(out, t) }
	}
	return out, nil
}
func (m *mockTaskStore) ListByTenantID(ctx context.Context, tid string, limit int, cursor string) ([]*core.Task, string, error) {
	var out []*core.Task
	for _, t := range m.tasks {
		if t.TenantID == tid { out = append(out, t) }
	}
	return out, "", nil
}
