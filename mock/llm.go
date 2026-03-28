package mock

import (
	"context"
	"errors"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
)

var ErrNoMoreMockResponses = errors.New("no more mock responses available")

// MockLLMProvider deterministically replays requested pre-recorded model outputs easing tests and CI workflows.
type MockLLMProvider struct {
	Responses    []interfaces.LLMResponse
	Fallback     *interfaces.LLMResponse
	
	currentIndex int
	CallsHistory []*core.AgentState
}

func NewMockLLMProvider(responses []interfaces.LLMResponse, fallback *interfaces.LLMResponse) *MockLLMProvider {
	return &MockLLMProvider{
		Responses: responses,
		Fallback:  fallback,
	}
}

func (m *MockLLMProvider) ModelInfo() interfaces.ModelInfo {
	return interfaces.ModelInfo{
		Name:            "mock-llm",
		MaxTokens:       4096,
		MaxOutputTokens: 1024,
		SupportsTools:   true,
	}
}

func (m *MockLLMProvider) Generate(ctx context.Context, state *core.AgentState) (*interfaces.LLMResponse, error) {
	m.CallsHistory = append(m.CallsHistory, state)

	if m.currentIndex < len(m.Responses) {
		resp := append([]interfaces.LLMResponse{}, m.Responses[m.currentIndex])[0]
		m.currentIndex++
		return &resp, nil
	}

	if m.Fallback != nil {
		return m.Fallback, nil
	}

	return nil, ErrNoMoreMockResponses
}

func (m *MockLLMProvider) GenerateStream(ctx context.Context, state *core.AgentState) (<-chan interfaces.TokenDelta, <-chan error, error) {
	m.CallsHistory = append(m.CallsHistory, state)

	deltaCh := make(chan interfaces.TokenDelta)
	errCh := make(chan error, 1)

	var nextResp *interfaces.LLMResponse
	if m.currentIndex < len(m.Responses) {
		nextResp = &m.Responses[m.currentIndex]
		m.currentIndex++
	} else if m.Fallback != nil {
		nextResp = m.Fallback
	} else {
		errCh <- ErrNoMoreMockResponses
		close(errCh)
		close(deltaCh)
		return deltaCh, errCh, nil
	}

	go func() {
		defer close(deltaCh)
		defer close(errCh)

		for _, part := range nextResp.Message.Parts {
			if part.Type == core.PartTypeText {
				for _, ch := range part.Text {
					select {
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					case deltaCh <- interfaces.TokenDelta{Type: interfaces.DeltaTypeText, Content: string(ch)}:
					}
				}
			} else if part.Type == core.PartTypeToolCall {
				select {
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				case deltaCh <- interfaces.TokenDelta{
					Type: interfaces.DeltaTypeToolCall,
					Content: part.ToolCall.ToolName,
				}:
				}
			}
		}

		deltaCh <- interfaces.TokenDelta{Type: interfaces.DeltaTypeDone}
	}()

	return deltaCh, errCh, nil
}
