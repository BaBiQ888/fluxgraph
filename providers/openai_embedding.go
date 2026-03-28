package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sony/gobreaker"
)

type OpenAIEmbeddingOptions struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

type OpenAIEmbeddingProvider struct {
	opts   OpenAIEmbeddingOptions
	cb     *gobreaker.CircuitBreaker
	logger zerolog.Logger
}

func NewOpenAIEmbeddingProvider(opts OpenAIEmbeddingOptions) *OpenAIEmbeddingProvider {
	if opts.BaseURL == "" {
		opts.BaseURL = "https://api.openai.com/v1"
	}
	if opts.Model == "" {
		opts.Model = "text-embedding-3-small"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name: "openai-embedding",
		OnStateChange: func(name string, from, to gobreaker.State) {
			log.Warn().Str("provider", name).Str("from", from.String()).Str("to", to.String()).Msg("circuit breaker state changed")
		},
	})

	return &OpenAIEmbeddingProvider{
		opts:   opts,
		cb:     cb,
		logger: log.With().Str("component", "openai-embedding").Logger(),
	}
}

type embeddingReq struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type embeddingResp struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (p *OpenAIEmbeddingProvider) EmbedText(ctx context.Context, text string) ([]float32, error) {
	reqBody := embeddingReq{
		Input: text,
		Model: p.opts.Model,
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, core.NewFatalError("OpenAIEmbeddingProvider", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/embeddings", bytes.NewReader(b))
	if err != nil {
		return nil, core.NewFatalError("OpenAIEmbeddingProvider", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.opts.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.opts.APIKey)
	}

	res, err := p.cb.Execute(func() (interface{}, error) {
		resp, callErr := p.opts.HTTPClient.Do(req)
		if callErr != nil {
			return nil, callErr
		}
		return resp, nil
	})

	if err != nil {
		return nil, core.NewRetriableError("OpenAIEmbeddingProvider", err, 2*time.Second)
	}
	
	resp := res.(*http.Response)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, core.NewFatalError("OpenAIEmbeddingProvider", fmt.Errorf("http %d: %s", resp.StatusCode, string(body)))
	}

	var eResp embeddingResp
	if err := json.NewDecoder(resp.Body).Decode(&eResp); err != nil {
		return nil, core.NewFatalError("OpenAIEmbeddingProvider", fmt.Errorf("json unmarshal error: %w", err))
	}

	if len(eResp.Data) == 0 {
		return nil, core.NewFatalError("OpenAIEmbeddingProvider", fmt.Errorf("empty embedding data returned"))
	}

	return eResp.Data[0].Embedding, nil
}
