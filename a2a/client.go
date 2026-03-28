package a2a

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/FluxGraph/fluxgraph/interfaces"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// A2AClient provides methods to interact with remote A2A-compliant agents.
type A2AClient struct {
	httpClient *http.Client
	token      string // Current Bearer token
}

func NewA2AClient(token string) *A2AClient {
	return &A2AClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
	}
}

// Discover fetches the AgentCard from the remote agent.
func (c *A2AClient) Discover(ctx context.Context, agentURL string) (*AgentCard, error) {
	url := strings.TrimSuffix(agentURL, "/") + "/.well-known/agent.json"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch agent card: %d", resp.StatusCode)
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, err
	}
	return &card, nil
}

// SendMessage starts a task using the "message/send" JSON-RPC method.
func (c *A2AClient) SendMessage(ctx context.Context, agentURL string, params SendMessageParams) (*RPCResponse, error) {
	rpcReq := RPCRequest{
		JSONRPC: "2.0",
		Method:  "message/send",
		Params:  mustMarshal(params),
		ID:      time.Now().UnixNano(),
	}

	body, _ := json.Marshal(rpcReq)
	req, err := http.NewRequestWithContext(ctx, "POST", agentURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rpcResp RPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, err
	}

	return &rpcResp, nil
}

// StreamEvents connects to a task's SSE endpoint and returns a channel of events.
func (c *A2AClient) StreamEvents(ctx context.Context, agentURL, taskID string) (<-chan interfaces.Event, error) {
	url := fmt.Sprintf("%s/tasks/%s/events", strings.TrimSuffix(agentURL, "/"), taskID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "text/event-stream")

	// Inject OTel TraceContext
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to connect to stream: %d", resp.StatusCode)
	}

	out := make(chan interfaces.Event, 10)
	go func() {
		defer resp.Body.Close()
		defer close(out)

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					// Logic for logging error could go here
				}
				return
			}

			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, ":") { // Heartbeat or empty
				continue
			}

			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				var ev interfaces.Event
				if err := json.Unmarshal([]byte(data), &ev); err == nil {
					select {
					case out <- ev:
					case <-ctx.Done():
						return
					}
					if ev.Type == interfaces.EventTaskCompleted {
						return
					}
				}
			}
		}
	}()

	return out, nil
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
