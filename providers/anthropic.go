package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sony/gobreaker"
)

type AnthropicOptions struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

type AnthropicProvider struct {
	opts   AnthropicOptions
	cb     *gobreaker.CircuitBreaker
	logger zerolog.Logger
}

func NewAnthropicProvider(opts AnthropicOptions) *AnthropicProvider {
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name: "anthropic-provider",
		OnStateChange: func(name string, from, to gobreaker.State) {
			log.Warn().Str("provider", name).Str("from", from.String()).Str("to", to.String()).Msg("circuit breaker state changed")
		},
	})

	return &AnthropicProvider{
		opts:   opts,
		cb:     cb,
		logger: log.With().Str("component", "anthropic-provider").Logger(),
	}
}

func (p *AnthropicProvider) ModelInfo() interfaces.ModelInfo {
	return interfaces.ModelInfo{
		Name:            p.opts.Model,
		MaxTokens:       200000,
		MaxOutputTokens: 8192,
		SupportsTools:   true,
	}
}

type anthropicReq struct {
	Model       string           `json:"model"`
	System      string           `json:"system,omitempty"`
	Messages    []anthropicMsg   `json:"messages"`
	MaxTokens   int              `json:"max_tokens"`
	Tools       []anthropicTool  `json:"tools,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
}

type anthropicMsg struct {
	Role    string           `json:"role"`
	Content []anthropicPart  `json:"content"`
}

type anthropicPart struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	
	// Tool use
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
	
	// Tool result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

func (p *AnthropicProvider) buildRequestInfo(state *core.AgentState, stream bool) anthropicReq {
	var sysPrompt string
	if state.Variables != nil {
		if sp, ok := state.Variables["system_prompt"].(string); ok {
			sysPrompt = sp
		}
	}

	req := anthropicReq{
		Model:     p.opts.Model,
		System:    sysPrompt,
		MaxTokens: 8192,
		Stream:    stream,
	}

	var msgs []anthropicMsg

	for _, m := range state.Messages {
		if m.Role == core.RoleSystem {
			continue // System handled via top-level field
		}
		
		role := mapRole(m.Role)
		if role == "tool" {
			role = "user" // Anthropic merges tool outputs as "user" roles encapsulating tool_result
		}
		if role == "unknown" {
			continue
		}
		
		msg := anthropicMsg{Role: role}
		for _, part := range m.Parts {
			switch part.Type {
			case core.PartTypeText:
				msg.Content = append(msg.Content, anthropicPart{Type: "text", Text: part.Text})
			case core.PartTypeToolCall:
				msg.Content = append(msg.Content, anthropicPart{
					Type:  "tool_use",
					ID:    part.ToolCall.CallID,
					Name:  part.ToolCall.ToolName,
					Input: json.RawMessage(stringifyArgs(part.ToolCall.Arguments)),
				})
			case core.PartTypeToolResult:
				msg.Content = append(msg.Content, anthropicPart{
					Type:      "tool_result",
					ToolUseID: part.ToolResult.CallID,
					Content:   part.ToolResult.Result,
				})
			}
		}

		if len(msg.Content) > 0 {
			msgs = append(msgs, msg)
		}
	}
	req.Messages = msgs

	if toolsRaw, ok := state.Variables["tools"]; ok {
		if tools, ok := toolsRaw.([]interfaces.Tool); ok {
			for _, t := range tools {
				schema := t.InputSchema()
				parameters := map[string]any{
					"type":       schema.Type,
					"properties": schema.Properties,
				}
				if len(schema.Required) > 0 {
					parameters["required"] = schema.Required
				}

				req.Tools = append(req.Tools, anthropicTool{
					Name:        t.Name(),
					Description: t.Description(),
					InputSchema: parameters,
				})
			}
		}
	}

	return req
}

func (p *AnthropicProvider) handleHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	errBody := fmt.Errorf("anthropic http %d: %s", resp.StatusCode, string(body))
	
	switch resp.StatusCode {
	case 429:
		return core.NewRetriableError("AnthropicProvider", errBody, 5*time.Second)
	case 400, 401, 403, 404:
		return core.NewFatalError("AnthropicProvider", errBody)
	case 500, 502, 503, 504:
		return core.NewRetriableError("AnthropicProvider", errBody, 5*time.Second)
	default:
		return core.NewFatalError("AnthropicProvider", errBody)
	}
}

type anthropicResp struct {
	Content []anthropicPart `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (p *AnthropicProvider) Generate(ctx context.Context, state *core.AgentState) (*interfaces.LLMResponse, error) {
	reqBody := p.buildRequestInfo(state, false)
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, core.NewFatalError("AnthropicProvider", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/messages", bytes.NewReader(b))
	if err != nil {
		return nil, core.NewFatalError("AnthropicProvider", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.opts.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	res, err := p.cb.Execute(func() (interface{}, error) {
		resp, err := p.opts.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		return resp, nil
	})

	if err != nil {
		return nil, core.NewRetriableError("AnthropicProvider", err, 2*time.Second)
	}
	resp := res.(*http.Response)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, p.handleHTTPError(resp)
	}

	var aResp anthropicResp
	if err := json.NewDecoder(resp.Body).Decode(&aResp); err != nil {
		return nil, core.NewFatalError("AnthropicProvider", fmt.Errorf("json decoding error: %w", err))
	}

	if len(aResp.Content) == 0 {
		return nil, core.NewFatalError("AnthropicProvider", errors.New("empty choices returned"))
	}

	msg := core.Message{Role: core.RoleAssistant}
	for _, c := range aResp.Content {
		if c.Type == "text" {
			msg.Parts = append(msg.Parts, core.Part{
				Type: core.PartTypeText,
				Text: c.Text,
			})
		} else if c.Type == "tool_use" {
			var args map[string]any
			b, _ := json.Marshal(c.Input)
			_ = json.Unmarshal(b, &args)
			msg.Parts = append(msg.Parts, core.Part{
				Type: core.PartTypeToolCall,
				ToolCall: &core.ToolCallPart{
					CallID:    c.ID,
					ToolName:  c.Name,
					Arguments: args,
				},
			})
		}
	}

	return &interfaces.LLMResponse{
		Message: msg,
		TokenUsage: interfaces.TokenUsage{
			InputTokens:  aResp.Usage.InputTokens,
			OutputTokens: aResp.Usage.OutputTokens,
			TotalTokens:  aResp.Usage.InputTokens + aResp.Usage.OutputTokens,
		},
	}, nil
}

func (p *AnthropicProvider) GenerateStream(ctx context.Context, state *core.AgentState) (<-chan interfaces.TokenDelta, <-chan error, error) {
	deltaCh := make(chan interfaces.TokenDelta)
	errCh := make(chan error, 1)

	reqBody := p.buildRequestInfo(state, true)
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, core.NewFatalError("AnthropicProvider", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/messages", bytes.NewReader(b))
	if err != nil {
		return nil, nil, core.NewFatalError("AnthropicProvider", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.opts.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "text/event-stream")

	res, err := p.cb.Execute(func() (interface{}, error) {
		resp, err := p.opts.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		return resp, nil
	})

	if err != nil {
		return nil, nil, core.NewRetriableError("AnthropicProvider", err, 2*time.Second)
	}
	resp := res.(*http.Response)

	if resp.StatusCode != 200 {
		err := p.handleHTTPError(resp)
		resp.Body.Close()
		return nil, nil, err
	}

	go func() {
		defer resp.Body.Close()
		defer close(deltaCh)
		defer close(errCh)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				errCh <- core.NewFatalError("AnthropicStream", ctx.Err())
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			var event struct {
				Type  string `json:"type"`
				Delta struct{
					Type string `json:"type"`
					Text string `json:"text"`
					PartialJson string `json:"partial_json"`
				} `json:"delta"`
				ContentBlock struct {
					Name string `json:"name"`
					ID   string `json:"id"`
				} `json:"content_block"`
			}
			
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			if event.Type == "content_block_delta" {
				if event.Delta.Type == "text_delta" {
					deltaCh <- interfaces.TokenDelta{
						Type:    interfaces.DeltaTypeText,
						Content: event.Delta.Text,
					}
				} else if event.Delta.Type == "input_json_delta" {
					deltaCh <- interfaces.TokenDelta{
						Type:    interfaces.DeltaTypeToolCall,
						Content: event.Delta.PartialJson, // Streaming raw json chunks
					}
				}
			} else if event.Type == "content_block_start" {
				if event.ContentBlock.Name != "" {
					deltaCh <- interfaces.TokenDelta{
						Type:    interfaces.DeltaTypeToolCall,
						Content: fmt.Sprintf("ToolCall:%s|%s", event.ContentBlock.ID, event.ContentBlock.Name),
					}
				}
			} else if event.Type == "message_stop" {
				deltaCh <- interfaces.TokenDelta{Type: interfaces.DeltaTypeDone}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- core.NewRetriableError("AnthropicStream", err, 2*time.Second)
		}
	}()

	return deltaCh, errCh, nil
}
