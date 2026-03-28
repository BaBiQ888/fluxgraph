package providers

import (
	"context"
	"fmt"
	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
)

// FallbackChainProvider routes inferences transparently through ordered fallbacks ensuring high availability during outage intervals implicitly.
type FallbackChainProvider struct {
	Chain []interfaces.LLMProvider
}

func NewFallbackChainProvider(chain ...interfaces.LLMProvider) *FallbackChainProvider {
	return &FallbackChainProvider{Chain: chain}
}

func (f *FallbackChainProvider) ModelInfo() interfaces.ModelInfo {
	if len(f.Chain) > 0 {
		return f.Chain[0].ModelInfo()
	}
	return interfaces.ModelInfo{Name: "fallback-chain-empty"}
}

func (f *FallbackChainProvider) Generate(ctx context.Context, state *core.AgentState) (*interfaces.LLMResponse, error) {
	var lastErr error
	for _, provider := range f.Chain {
		resp, err := provider.Generate(ctx, state)
		if err == nil {
			return resp, nil
		}
		
		if ae, ok := err.(*core.AgentError); ok {
			if ae.Category == core.ErrCategoryFatal {
				return nil, err
			}
		}
		lastErr = err
	}
	return nil, fmt.Errorf("fallback chain exhausted: %w", lastErr)
}

func (f *FallbackChainProvider) GenerateStream(ctx context.Context, state *core.AgentState) (<-chan interfaces.TokenDelta, <-chan error, error) {
	var lastErr error
	for _, provider := range f.Chain {
		dCh, eCh, err := provider.GenerateStream(ctx, state)
		if err == nil {
			return dCh, eCh, nil 
		}
		
		if ae, ok := err.(*core.AgentError); ok && ae.Category == core.ErrCategoryFatal {
			return nil, nil, err
		}
		lastErr = err
	}
	return nil, nil, fmt.Errorf("fallback chain stream exhausted: %w", lastErr)
}
