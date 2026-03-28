package a2a_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/FluxGraph/fluxgraph/a2a"
	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
	"github.com/FluxGraph/fluxgraph/graph"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/FluxGraph/fluxgraph/storage"
	"github.com/FluxGraph/fluxgraph/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- Test Helpers ----

type testNode struct {
	id string
}
func (n testNode) ID() string { return n.id }
func (n testNode) Process(_ context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	return &interfaces.NodeResult{State: state}, nil
}

type stubRedis struct {
	mu      sync.Mutex
	strings map[string]string
	lists   map[string][]string
	zsets   map[string][]struct{score float64; member string}
}
func (r *stubRedis) Get(_ context.Context, k string) (string, error) {
	r.mu.Lock(); defer r.mu.Unlock()
	v, ok := r.strings[k]
	if !ok { return "", storage.ErrRedisKeyNotFound }
	return v, nil
}
func (r *stubRedis) Set(_ context.Context, k, v string, _ time.Duration) error {
	r.mu.Lock(); defer r.mu.Unlock(); r.strings[k] = v; return nil
}
func (r *stubRedis) RPush(_ context.Context, k string, vs ...string) error {
	r.mu.Lock(); defer r.mu.Unlock(); r.lists[k] = append(r.lists[k], vs...); return nil
}
func (r *stubRedis) LRange(_ context.Context, k string, _, _ int64) ([]string, error) {
	r.mu.Lock(); defer r.mu.Unlock(); return r.lists[k], nil
}
func (r *stubRedis) LTrim(_ context.Context, k string, _, _ int64) error { return nil }
func (r *stubRedis) ZAdd(_ context.Context, k string, s float64, m string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	r.zsets[k] = append(r.zsets[k], struct{score float64; member string}{s, m})
	return nil
}
func (r *stubRedis) ZRange(_ context.Context, k string, _, _ int64) ([]string, error) {
	r.mu.Lock(); defer r.mu.Unlock()
	var out []string
	for _, m := range r.zsets[k] { out = append(out, m.member) }
	return out, nil
}
func (r *stubRedis) ZRemRangeByRank(_ context.Context, _ string, _, _ int64) error { return nil }
func (r *stubRedis) TxExec(_ context.Context, fn func(storage.RedisClient) error) error { return fn(r) }

func setupTestServer() (*a2a.Server, *a2a.Authenticator, interfaces.TaskStore) {
	b := graph.NewBuilder()
	_ = b.AddNode(testNode{id: "start"})
	b.SetEntry("start")
	g, _ := b.Build()
	
	stub := &stubRedis{
		strings: make(map[string]string),
		lists:   make(map[string][]string),
		zsets:   make(map[string][]struct{score float64; member string}),
	}
	mem := storage.NewRedisMemoryStore(stub, storage.RedisMemoryStoreOptions{})
	ts := storage.NewRedisTaskStore(stub, "test", 0)
	reg := tools.NewConcreteToolRegistry()
	eng := engine.NewEngine(g, mem, nil)
	
	opts := a2a.ServerOptions{
		Name:    "TestAgent",
		Secret:  "super-secret",
		URL:     "http://localhost",
		Version: "1.0.0",
	}
	return a2a.NewServer(eng, ts, mem, reg, nil, opts), a2a.NewAuthenticator("super-secret"), ts
}

// ---- Tests ----

func TestAgentCardEndpoint(t *testing.T) {
	srv, _, _ := setupTestServer()
	req := httptest.NewRequest("GET", "/.well-known/agent.json", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var card a2a.AgentCard
	err := json.Unmarshal(w.Body.Bytes(), &card)
	require.NoError(t, err)
	assert.Equal(t, "TestAgent", card.Name)
}

func TestJSONRPC_SendMessage(t *testing.T) {
	srv, auth, _ := setupTestServer()
	token, _ := auth.GenerateToken("tenant1", []string{"agent:write"}, time.Hour)

	params := a2a.SendMessageParams{
		Message: core.Message{
			Role:  core.RoleUser,
			Parts: []core.Part{{Type: core.PartTypeText, Text: "Hello"}},
		},
	}
	paramsRaw, _ := json.Marshal(params)
	
	rpcReq := a2a.RPCRequest{
		JSONRPC: "2.0",
		Method:  "message/send",
		Params:  paramsRaw,
		ID:      1,
	}
	body, _ := json.Marshal(rpcReq)

	req := httptest.NewRequest("POST", "/", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var rpcResp a2a.RPCResponse
	err := json.Unmarshal(w.Body.Bytes(), &rpcResp)
	require.NoError(t, err)
	assert.Nil(t, rpcResp.Error)
}

func TestJSONRPC_CreateWebhook(t *testing.T) {
	srv, auth, ts := setupTestServer()
	token, _ := auth.GenerateToken("tenant1", []string{"agent:write"}, time.Hour)

	// Pre-create a task
	task := &core.Task{ID: "task1", TenantID: "tenant1"}
	_ = ts.Create(context.WithValue(context.Background(), "tenantID", "tenant1"), task)

	params := a2a.CreatePushConfigParams{
		TaskID: "task1",
		URL:    "http://webhook.site/abc",
		Secret: "shhh",
	}
	paramsRaw, _ := json.Marshal(params)
	
	rpcReq := a2a.RPCRequest{
		JSONRPC: "2.0",
		Method:  "tasks/pushNotificationConfig/create",
		Params:  paramsRaw,
		ID:      1,
	}
	body, _ := json.Marshal(rpcReq)

	req := httptest.NewRequest("POST", "/", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var rpcResp a2a.RPCResponse
	_ = json.Unmarshal(w.Body.Bytes(), &rpcResp)
	assert.Nil(t, rpcResp.Error)
}
