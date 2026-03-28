package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
)

// DelegateNode represents a graph node that delegates its task to a remote A2A agent.
type DelegateNode struct {
	id             string
	remoteAgentURL string
	client         *A2AClient
	preferStream   bool
}

func NewDelegateNode(id, url string, client *A2AClient) *DelegateNode {
	return &DelegateNode{
		id:             id,
		remoteAgentURL: url,
		client:         client,
		preferStream:   true, // Default to streaming for better responsiveness
	}
}

func (n *DelegateNode) ID() string { return n.id }

func (n *DelegateNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	// 1. Prepare Message from current state
	// For now, we send the entire message history
	lastMsg := state.LastMessage()
	
	params := SendMessageParams{
		Message:           lastMsg,
		ContextID:         state.ContextID,
		ReturnImmediately: true, // We want the TaskID to start streaming
	}

	// 2. Discover far end (optional optimization: cache this)
	card, err := n.client.Discover(ctx, n.remoteAgentURL)
	if err != nil {
		return nil, fmt.Errorf("delegate discovery failed: %w", err)
	}

	// 3. Initiate delegation
	rpcResp, err := n.client.SendMessage(ctx, n.remoteAgentURL, params)
	if err != nil {
		return nil, fmt.Errorf("delegate initiation failed: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("remote agent error: %s", rpcResp.Error.Message)
	}

	// Extract Task from result
	taskData, _ := json.Marshal(rpcResp.Result)
	var remoteTask core.Task
	if err := json.Unmarshal(taskData, &remoteTask); err != nil {
		return nil, fmt.Errorf("failed to decode remote task: %w", err)
	}

	// 4. Stream or Poll for results
	if n.preferStream && card.Capabilities.Streaming {
		events, err := n.client.StreamEvents(ctx, n.remoteAgentURL, remoteTask.ID)
		if err != nil {
			log.Printf("Streaming failed, falling back to polling: %v", err)
		} else {
			for ev := range events {
				// Handle artifacts from remote
				if ev.Type == interfaces.EventNodeCompleted {
					if art, ok := ev.Payload["artifact"].(map[string]any); ok {
						state.Artifacts = append(state.Artifacts, core.Artifact{
							ID:   art["id"].(string),
							Name: "RemoteArtifact",
							Metadata: map[string]any{
								"type": art["type"],
								"data": art["data"],
							},
						})
					}
				}
				if ev.Type == interfaces.EventTaskCompleted {
					break
				}
			}
		}
	}

	// 5. Final sync of the task state
	// Note: In a production version, we would fetch the final task artifacts here if not using streaming.
	
	return &interfaces.NodeResult{
		State: state,
	}, nil
}
