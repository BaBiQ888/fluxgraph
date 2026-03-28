package a2a_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FluxGraph/fluxgraph/a2a"
	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestA2AClient_Discover(t *testing.T) {
	card := a2a.AgentCard{Name: "RemoteAgent", Version: "1.0.0"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/.well-known/agent.json", r.URL.Path)
		_ = json.NewEncoder(w).Encode(card)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := a2a.NewA2AClient("test-token")
	res, err := client.Discover(context.Background(), ts.URL)
	require.NoError(t, err)
	assert.Equal(t, "RemoteAgent", res.Name)
}

func TestA2AClient_StreamEvents(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Send initial event
		ev1 := interfaces.Event{Type: interfaces.EventNodeCompleted, TaskID: "t1"}
		data1, _ := json.Marshal(ev1)
		_, _ = w.Write([]byte("data: " + string(data1) + "\n\n"))
		
		// Send completion event
		ev2 := interfaces.Event{Type: interfaces.EventTaskCompleted, TaskID: "t1"}
		data2, _ := json.Marshal(ev2)
		_, _ = w.Write([]byte("data: " + string(data2) + "\n\n"))
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := a2a.NewA2AClient("test-token")
	events, err := client.StreamEvents(context.Background(), ts.URL, "t1")
	require.NoError(t, err)

	var count int
	for ev := range events {
		count++
		if ev.Type == interfaces.EventTaskCompleted {
			break
		}
	}
	assert.Equal(t, 2, count)
}

func TestDelegateNode_Process(t *testing.T) {
	// 1. Mock remote A2A Server
	srvHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent.json" {
			_ = json.NewEncoder(w).Encode(a2a.AgentCard{
				Capabilities: a2a.AgentCapabilities{Streaming: true},
			})
			return
		}
		if r.Method == "POST" {
			// Mock successful SendMessage
			resp := a2a.RPCResponse{
				JSONRPC: "2.0",
				Result: core.Task{
					ID: "remote-task-1",
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.URL.Path == "/tasks/remote-task-1/events" {
			w.Header().Set("Content-Type", "text/event-stream")
			ev := interfaces.Event{
				Type:   interfaces.EventTaskCompleted,
				TaskID: "remote-task-1",
			}
			data, _ := json.Marshal(ev)
			_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
			return
		}
	})
	ts := httptest.NewServer(srvHandler)
	defer ts.Close()

	// 2. Setup DelegateNode
	client := a2a.NewA2AClient("token")
	node := a2a.NewDelegateNode("node1", ts.URL, client)
	
	state := core.NewState()
	state.Messages = append(state.Messages, core.Message{Role: core.RoleUser, Parts: []core.Part{{Type: core.PartTypeText, Text: "hi"}}})

	res, err := node.Process(context.Background(), state)
	require.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, state.TaskID, res.State.TaskID)
}
