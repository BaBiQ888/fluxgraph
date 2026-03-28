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

// OpenAIOptions configures standard initialization contexts defining HTTP proxy limits optionally.
type OpenAIOptions struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

// OpenAIProvider encompasses the complete bindings against vanilla OpenAI endpoints producing seamless Agent tooling integration.
type OpenAIProvider struct {
	opts   OpenAIOptions
	cb     *gobreaker.CircuitBreaker
	logger zerolog.Logger
}

func NewOpenAIProvider(opts OpenAIOptions) *OpenAIProvider {
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name: "openai-provider",
		OnStateChange: func(name string, from, to gobreaker.State) {
			log.Warn().Str("provider", name).Str("from", from.String()).Str("to", to.String()).Msg("circuit breaker state changed")
		},
	})

	return &OpenAIProvider{
		opts:   opts,
		cb:     cb,
		logger: log.With().Str("component", "openai-provider").Logger(),
	}
}

func (p *OpenAIProvider) ModelInfo() interfaces.ModelInfo {
	return interfaces.ModelInfo{
		Name:            p.opts.Model,
		MaxTokens:       128000,
		MaxOutputTokens: 4096,
		SupportsTools:   true,
	}
}

type openAIReq struct {
	Model    string       `json:"model"`
	Messages []openAIMsg  `json:"messages"`
	Tools    []openAITool `json:"tools,omitempty"`
	Stream   bool         `json:"stream,omitempty"`
}

type openAIMsg struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	Name       string           `json:"name,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string           `json:"type"`
	Function openAIFuncSchema `json:"function"`
}

type openAIFuncSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openAIFuncCall `json:"function"`
}

type openAIFuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func mapRole(r core.Role) string {
	switch r {
	case core.RoleUser:
		return "user"
	case core.RoleAssistant:
		return "assistant"
	case core.RoleSystem:
		return "system"
	case core.RoleTool:
		return "tool"
	default:
		return "unknown"
	}
}

func stringifyArgs(args map[string]any) string {
	b, _ := json.Marshal(args)
	return string(b)
}

func mapMessages(msgs []core.Message, sysPrompt string) []openAIMsg {
	var out []openAIMsg
	if sysPrompt != "" {
		out = append(out, openAIMsg{Role: "system", Content: sysPrompt})
	}

	for _, m := range msgs {
		role := mapRole(m.Role)
		if role == "unknown" {
			continue
		}
		
		msg := openAIMsg{Role: role}
		var contentBuilder strings.Builder
		
		for _, p := range m.Parts {
			switch p.Type {
			case core.PartTypeText:
				contentBuilder.WriteString(p.Text)
			case core.PartTypeToolCall:
				msg.ToolCalls = append(msg.ToolCalls, openAIToolCall{
					ID:   p.ToolCall.CallID,
					Type: "function",
					Function: openAIFuncCall{
						Name:      p.ToolCall.ToolName,
						Arguments: stringifyArgs(p.ToolCall.Arguments),
					},
				})
			case core.PartTypeToolResult:
				msg.ToolCallID = p.ToolResult.CallID
				msg.Content = p.ToolResult.Result
			}
		}

		if contentBuilder.Len() > 0 && msg.Content == "" {
			msg.Content = contentBuilder.String()
		}
		out = append(out, msg)
	}
	return out
}

func (p *OpenAIProvider) buildRequestInfo(state *core.AgentState, stream bool) openAIReq {
	var sysPrompt string
	if state.Variables != nil {
		if sp, ok := state.Variables["system_prompt"].(string); ok {
			sysPrompt = sp
		}
	}

	req := openAIReq{
		Model:    p.opts.Model,
		Messages: mapMessages(state.Messages, sysPrompt),
		Stream:   stream,
	}

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

				req.Tools = append(req.Tools, openAITool{
					Type: "function",
					Function: openAIFuncSchema{
						Name:        t.Name(),
						Description: t.Description(),
						Parameters:  parameters,
					},
				})
			}
		}
	}

	return req
}

func (p *OpenAIProvider) handleHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	errBody := fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	
	switch resp.StatusCode {
	case 429:
		retryAfterStr := resp.Header.Get("Retry-After")
		dur := 5 * time.Second
		if retryAfterStr != "" {
			// Phase2 specific backoff mapping.
		}
		return core.NewRetriableError("OpenAIProvider", errBody, dur)
	case 400, 401, 403, 404:
		return core.NewFatalError("OpenAIProvider", errBody)
	case 500, 502, 503, 504:
		return core.NewRetriableError("OpenAIProvider", errBody, 5*time.Second)
	default:
		return core.NewFatalError("OpenAIProvider", errBody)
	}
}

type openAIResp struct {
	Choices []struct {
		Message      openAIMsg `json:"message"`
		FinishReason string    `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (p *OpenAIProvider) Generate(ctx context.Context, state *core.AgentState) (*interfaces.LLMResponse, error) {
	reqBody := p.buildRequestInfo(state, false)
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, core.NewFatalError("OpenAIProvider", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, core.NewFatalError("OpenAIProvider", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.opts.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.opts.APIKey)
	}

	res, err := p.cb.Execute(func() (interface{}, error) {
		resp, err := p.opts.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		return resp, nil
	})

	if err != nil {
		return nil, core.NewRetriableError("OpenAIProvider", err, 2*time.Second)
	}
	resp := res.(*http.Response)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, p.handleHTTPError(resp)
	}

	var oResp openAIResp
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return nil, core.NewFatalError("OpenAIProvider", fmt.Errorf("json unmarshal error: %w", err))
	}

	if len(oResp.Choices) == 0 {
		return nil, core.NewFatalError("OpenAIProvider", errors.New("empty choices returned"))
	}

	return p.mapResponse(oResp), nil
}

func (p *OpenAIProvider) mapResponse(oResp openAIResp) *interfaces.LLMResponse {
	choice := oResp.Choices[0]
	msg := core.Message{Role: core.RoleAssistant}

	if choice.Message.Content != "" {
		msg.Parts = append(msg.Parts, core.Part{
			Type: core.PartTypeText,
			Text: choice.Message.Content,
		})
	}

	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args) 
		msg.Parts = append(msg.Parts, core.Part{
			Type: core.PartTypeToolCall,
			ToolCall: &core.ToolCallPart{
				CallID:    tc.ID,
				ToolName:  tc.Function.Name,
				Arguments: args,
			},
		})
	}

	return &interfaces.LLMResponse{
		Message: msg,
		TokenUsage: interfaces.TokenUsage{
			InputTokens:  oResp.Usage.PromptTokens,
			OutputTokens: oResp.Usage.CompletionTokens,
			TotalTokens:  oResp.Usage.TotalTokens,
		},
	}
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

func (p *OpenAIProvider) GenerateStream(ctx context.Context, state *core.AgentState) (<-chan interfaces.TokenDelta, <-chan error, error) {
	deltaCh := make(chan interfaces.TokenDelta)
	errCh := make(chan error, 1)

	reqBody := p.buildRequestInfo(state, true)
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, core.NewFatalError("OpenAIProvider", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, nil, core.NewFatalError("OpenAIProvider", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.opts.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.opts.APIKey)
	}
	req.Header.Set("Accept", "text/event-stream")

	res, err := p.cb.Execute(func() (interface{}, error) {
		resp, err := p.opts.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		return resp, nil
	})

	if err != nil {
		return nil, nil, core.NewRetriableError("OpenAIProvider", err, 2*time.Second)
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
				errCh <- core.NewFatalError("OpenAIStream", ctx.Err())
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				deltaCh <- interfaces.TokenDelta{Type: interfaces.DeltaTypeDone}
				return
			}

			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue 
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				deltaCh <- interfaces.TokenDelta{
					Type:    interfaces.DeltaTypeText,
					Content: delta.Content,
				}
			}

			if len(delta.ToolCalls) > 0 {
				tc := delta.ToolCalls[0] 
				contentStr := tc.Function.Arguments
				if tc.Function.Name != "" {
					contentStr = fmt.Sprintf("ToolCall:%s|%s", tc.ID, tc.Function.Name) 
				}
				deltaCh <- interfaces.TokenDelta{
					Type:    interfaces.DeltaTypeToolCall,
					Content: contentStr,
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- core.NewRetriableError("OpenAIStream", err, 2*time.Second)
		}
	}()

	return deltaCh, errCh, nil
}
