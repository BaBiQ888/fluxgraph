package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
)

// SearchMemoryTool connects the LLM Agent to the Postgres RAG vector layer.
type SearchMemoryTool struct {
	store interfaces.MemoryStore
}

func NewSearchMemoryTool(store interfaces.MemoryStore) *SearchMemoryTool {
	return &SearchMemoryTool{
		store: store,
	}
}

func (s *SearchMemoryTool) Name() string { return "search_memory" }

func (s *SearchMemoryTool) Description() string {
	return "Searches the historical conversation context for semantically matching messages using vector embeddings. Use this when the user asks about something discussed previously that is no longer in the immediate context window. Query should be a full sentence describing the expected information."
}

func (s *SearchMemoryTool) InputSchema() interfaces.ToolInputSchema {
	return interfaces.ToolInputSchema{
		Type: "object",
		Properties: map[string]map[string]any{
			"query": {
				"type":        "string",
				"description": "The search phrase to vector embed and lookup. Please be descriptive and semantic.",
			},
		},
		Required: []string{"query"},
	}
}

func (s *SearchMemoryTool) RequiredPermissions() []string { return nil }

func (s *SearchMemoryTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("missing or empty 'query' argument")
	}

	// 1. Extract sessionID injected by the Engine's Execute() context
	sessionValue := ctx.Value(core.SessionContextKey)
	sessionID, ok := sessionValue.(string)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("session_id not found in execution context; tool must be run within an engine session")
	}

	// 2. Perform the semantic search
	messages, err := s.store.Search(ctx, sessionID, query, 5) // Hardcode TopK=5 to prevent context overflow
	if err != nil {
		return "", fmt.Errorf("failed to perform vector search: %w", err)
	}

	if len(messages) == 0 {
		return "No highly relevant historical records found for your query.", nil
	}

	// 3. Format matched structural outcomes back to the prompt
	var sb strings.Builder
	sb.WriteString("Found the following historical context:\n\n")
	for i, msg := range messages {
		sb.WriteString(fmt.Sprintf("--- Match %d (Role: %s) ---\n", i+1, msg.Role))
		
		for _, part := range msg.Parts {
			if part.Type == core.PartTypeText {
				sb.WriteString(part.Text)
				sb.WriteString("\n")
			} else if part.Type == core.PartTypeToolCall {
				b, _ := json.Marshal(part.ToolCall.Arguments)
				sb.WriteString(fmt.Sprintf("[ToolCall %s]: %s\n", part.ToolCall.ToolName, string(b)))
			} else if part.Type == core.PartTypeToolResult {
				sb.WriteString(fmt.Sprintf("[ToolResult]: %s\n", part.ToolResult.Result))
			}
		}
	}

	return sb.String(), nil
}
