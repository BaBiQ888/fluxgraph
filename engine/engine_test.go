package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
	"github.com/FluxGraph/fluxgraph/graph"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/FluxGraph/fluxgraph/memory"
	"github.com/FluxGraph/fluxgraph/mock"
	"github.com/stretchr/testify/assert"
)

// LLMNode simulates generating tool calls or producing end-user terminal answers.
type LLMNode struct {
	llm interfaces.LLMProvider
}

func (n *LLMNode) ID() string { return "LLMNode" }
func (n *LLMNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	resp, err := n.llm.Generate(ctx, state)
	if err != nil {
		return nil, err
	}
	
	nextState := state.WithMessage(resp.Message)

	hasToolCall := false
	for _, p := range resp.Message.Parts {
		if p.Type == core.PartTypeToolCall {
			hasToolCall = true
			nextState = nextState.WithVariable("tool_call", p.ToolCall)
			break
		}
	}
	
	if !hasToolCall {
		nextState = nextState.WithVariable("tool_call", nil)
	}

	return &interfaces.NodeResult{State: nextState}, nil
}

// ToolExecutorNode executes registered tooling functionalities synchronously updating message arrays.
type ToolExecutorNode struct {
	registry interfaces.ToolRegistry
}

func (n *ToolExecutorNode) ID() string { return "ToolExecutorNode" }
func (n *ToolExecutorNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	tcRaw, ok := state.Variables["tool_call"]
	if !ok || tcRaw == nil {
		return &interfaces.NodeResult{State: state}, nil
	}
	tc := tcRaw.(*core.ToolCallPart)

	results := n.registry.ExecuteConcurrent(ctx, []core.ToolCallPart{*tc})
	resMessage := core.Message{
		Role:  core.RoleTool,
		Parts: []core.Part{{Type: core.PartTypeToolResult, ToolResult: &results[0]}},
	}
	
	nextState := state.WithMessage(resMessage).WithVariable("tool_call", nil)
	return &interfaces.NodeResult{State: nextState}, nil
}

func TestReActLoopEndToEnd(t *testing.T) {
	mockLLM := mock.NewMockLLMProvider([]interfaces.LLMResponse{
		{
			Message: core.Message{
				Role: core.RoleAssistant,
				Parts: []core.Part{{Type: core.PartTypeToolCall, ToolCall: &core.ToolCallPart{
					CallID: "call_1", ToolName: "get_weather", Arguments: map[string]any{"city": "Paris"},
				}}},
			},
		},
		{
			Message: core.Message{
				Role: core.RoleAssistant,
				Parts: []core.Part{{Type: core.PartTypeText, Text: "The weather in Paris is sunny."}},
			},
		},
	}, nil)

	mockReg := mock.NewMockToolRegistry()
	_ = mockReg.Register(&mock.MockTool{
		ToolName:    "get_weather",
		FixedResult: "Sunny, 25C",
	})

	llmNode := &LLMNode{llm: mockLLM}
	toolNode := &ToolExecutorNode{registry: mockReg}

	builder := graph.NewBuilder()
	_ = builder.AddNode(llmNode)
	_ = builder.AddNode(toolNode)
	builder.SetEntry("LLMNode")
	builder.SetTerminal("LLMNode")

	builder.AddConditionalEdge("LLMNode", func(ctx context.Context, state *core.AgentState) (string, error) {
		tcRaw, ok := state.Variables["tool_call"]
		if ok && tcRaw != nil {
			return "ToolExecutorNode", nil
		}
		return "", nil 
	})
	builder.AddEdge("ToolExecutorNode", "LLMNode")

	g, err := builder.Build()
	assert.NoError(t, err)

	ctx := context.Background()
	store := memory.NewInMemoryStore()
	bus := memory.NewInMemoryEventBus()
	eng := engine.NewEngine(g, store, bus)
	sessionID := "session-react-loop"

	finalState, err := eng.Start(ctx, sessionID, core.NewState())
	assert.NoError(t, err)
	assert.NotNil(t, finalState)
	assert.Equal(t, core.StatusCompleted, finalState.Status)

	assert.Len(t, finalState.Messages, 3) // Initial Request(0) absent so: Call(0) + Exec(1) + FinalSummary(2).
	
	finalMsg := finalState.Messages[2]
	assert.Equal(t, core.RoleAssistant, finalMsg.Role)
	assert.True(t, strings.Contains(finalMsg.Parts[0].Text, "sunny"))
}

func TestEngineInterruptResume(t *testing.T) {
	interruptNode := &MockInterruptNode{}
	
	builder := graph.NewBuilder()
	_ = builder.AddNode(interruptNode)
	builder.SetEntry("InterruptNode")
	builder.SetTerminal("InterruptNode")

	g, _ := builder.Build()
	store := memory.NewInMemoryStore()
	eng := engine.NewEngine(g, store, nil)
	
	state, err := eng.Start(context.Background(), "session-hitl", nil)
	assert.NoError(t, err)
	assert.Equal(t, core.StatusPaused, state.Status) // Interrupt properly saved.

	resumedState, err := eng.Resume(context.Background(), "session-hitl", map[string]any{"approved": true})
	assert.NoError(t, err)
	assert.Equal(t, core.StatusCompleted, resumedState.Status)
	assert.Equal(t, true, resumedState.Variables["approved"])
}

type MockInterruptNode struct{}
func (m *MockInterruptNode) ID() string { return "InterruptNode" }
func (m *MockInterruptNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	if approved, ok := state.Variables["approved"].(bool); ok && approved {
		return &interfaces.NodeResult{State: state}, nil
	}
	return &interfaces.NodeResult{
		State: state,
		Interrupt: &interfaces.InterruptSignal{
			Type: interfaces.InterruptHumanApproval,
		},
	}, nil
}
