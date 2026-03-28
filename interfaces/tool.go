package interfaces

import (
	"context"

	"github.com/FluxGraph/fluxgraph/core"
)

// ToolInputSchema corresponds to a JSON Schema equivalent definition.
type ToolInputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]map[string]any `json:"properties"`
	Required   []string                  `json:"required"`
}

// Tool declares an actionable remote or local function.
type Tool interface {
	Name() string
	Description() string
	InputSchema() ToolInputSchema
	RequiredPermissions() []string
	Execute(ctx context.Context, args map[string]any) (string, error)
}

// ToolRegistry manages lookup, authorization, and execution routing for tools.
type ToolRegistry interface {
	Register(tool Tool, permissions ...string) error
	GetTool(name string) (Tool, bool)
	ListTools() []Tool
	ExecuteConcurrent(ctx context.Context, calls []core.ToolCallPart) []core.ToolResultPart
	AuthorizeCall(ctx context.Context, toolName string) error
}
