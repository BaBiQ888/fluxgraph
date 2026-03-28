package interfaces

import (
	"context"

	"github.com/FluxGraph/fluxgraph/core"
)

type FinishReason string

const (
	FinishReasonStop          FinishReason = "Stop"
	FinishReasonLength        FinishReason = "Length"
	FinishReasonToolCalls     FinishReason = "ToolCalls"
	FinishReasonContentFilter FinishReason = "ContentFilter"
)

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type ModelInfo struct {
	Name            string
	MaxTokens       int
	MaxOutputTokens int
	SupportsTools   bool
}

type LLMResponse struct {
	Message      core.Message
	TokenUsage   TokenUsage
	FinishReason FinishReason
}

type DeltaType string

const (
	DeltaTypeText     DeltaType = "Text"
	DeltaTypeToolCall DeltaType = "ToolCall"
	DeltaTypeDone     DeltaType = "Done"
)

type TokenDelta struct {
	Type    DeltaType
	Content string // Text chunk or raw JSON chunk for tool call arguments
}

// LLMProvider acts as a unified abstraction over various underlying LLMs (OpenAI, Anthropic, Ollama).
type LLMProvider interface {
	ModelInfo() ModelInfo
	Generate(ctx context.Context, state *core.AgentState) (*LLMResponse, error)
	GenerateStream(ctx context.Context, state *core.AgentState) (<-chan TokenDelta, <-chan error, error)
}

// EmbeddingProvider defines the contract for generating vector representations from text.
type EmbeddingProvider interface {
	// EmbedText returns a vector corresponding to the input text. Error covers API failures.
	EmbedText(ctx context.Context, text string) ([]float32, error)
}
