package mock

import (
	"context"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
)

// MockTool offers injection point defining predetermined behavior outputs verifying execution branching traces safely.
type MockTool struct {
	ToolName        string
	Desc            string
	BehaviorFunc    func(args map[string]any) (string, error)
	FixedResult     string
	ReqPermissions  []string
	ExecuteHistory  []map[string]any
}

func (t *MockTool) Name() string { return t.ToolName }
func (t *MockTool) Description() string { return t.Desc }
func (t *MockTool) InputSchema() interfaces.ToolInputSchema {
	return interfaces.ToolInputSchema{}
}
func (t *MockTool) RequiredPermissions() []string { return t.ReqPermissions }
func (t *MockTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	t.ExecuteHistory = append(t.ExecuteHistory, args)
	if t.BehaviorFunc != nil {
		return t.BehaviorFunc(args)
	}
	return t.FixedResult, nil
}

type MockToolRegistry struct {
	tools map[string]interfaces.Tool
}

func NewMockToolRegistry() *MockToolRegistry {
	return &MockToolRegistry{
		tools: make(map[string]interfaces.Tool),
	}
}

func (r *MockToolRegistry) Register(tool interfaces.Tool, permissions ...string) error {
	r.tools[tool.Name()] = tool
	return nil
}

func (r *MockToolRegistry) GetTool(name string) (interfaces.Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *MockToolRegistry) ListTools() []interfaces.Tool {
	var list []interfaces.Tool
	for _, t := range r.tools {
		list = append(list, t)
	}
	return list
}

func (r *MockToolRegistry) ExecuteConcurrent(ctx context.Context, calls []core.ToolCallPart) []core.ToolResultPart {
	results := make([]core.ToolResultPart, len(calls))
	for i, call := range calls {
		tool, ok := r.tools[call.ToolName]
		if !ok {
			results[i] = core.ToolResultPart{CallID: call.CallID, Result: "tool not found", IsError: true}
			continue
		}
		
		res, err := tool.Execute(ctx, call.Arguments)
		if err != nil {
			results[i] = core.ToolResultPart{CallID: call.CallID, Result: err.Error(), IsError: true}
		} else {
			results[i] = core.ToolResultPart{CallID: call.CallID, Result: res, IsError: false}
		}
	}
	return results
}

func (r *MockToolRegistry) AuthorizeCall(ctx context.Context, toolName string) error {
	return nil
}
