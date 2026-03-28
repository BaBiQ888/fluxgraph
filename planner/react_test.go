package planner_test

import (
	"context"
	"testing"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/planner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReActPlanner_Plan_SeedsThinkStep(t *testing.T) {
	p := planner.NewReActPlanner(10)
	state := core.NewState()

	out, err := p.Plan(context.Background(), "find the weather in Paris", state)
	require.NoError(t, err)
	assert.Equal(t, planner.ThinkNodeID, out.NextNodeID)

	// Plan is embedded in the returned state.
	planRaw := out.State.Variables["__plan__"]
	require.NotNil(t, planRaw)
	plan := planRaw.(*planner.Plan)
	assert.Equal(t, "find the weather in Paris", plan.Goal)
	assert.Len(t, plan.Steps, 1)
}

func TestReActPlanner_Revise_ToolCallDetected(t *testing.T) {
	p := planner.NewReActPlanner(10)
	state := core.NewState()

	out, _ := p.Plan(context.Background(), "goal", state)
	// Simulate LLMNode setting a tool_call.
	toolCall := &core.ToolCallPart{CallID: "c1", ToolName: "get_weather"}
	stateWithTool := out.State.WithVariable("tool_call", toolCall)

	out2, err := p.Revise(context.Background(), "", stateWithTool)
	require.NoError(t, err)
	assert.Equal(t, planner.ToolNodeID, out2.NextNodeID)
}

func TestReActPlanner_Revise_ToolResultContinuesThinking(t *testing.T) {
	p := planner.NewReActPlanner(10)
	state := core.NewState()

	out, _ := p.Plan(context.Background(), "goal", state)
	// No pending tool call; observation provided → Think again.
	noTool := out.State.WithVariable("tool_call", nil)

	out2, err := p.Revise(context.Background(), "Sunny 25C", noTool)
	require.NoError(t, err)
	assert.Equal(t, planner.ThinkNodeID, out2.NextNodeID)
}

func TestReActPlanner_Revise_NoToolCallTerminates(t *testing.T) {
	p := planner.NewReActPlanner(10)
	state := core.NewState()

	out, _ := p.Plan(context.Background(), "goal", state)
	// No tool call, no observation → final answer.
	noTool := out.State.WithVariable("tool_call", nil)

	out2, err := p.Revise(context.Background(), "", noTool)
	require.NoError(t, err)
	assert.Equal(t, planner.TerminalID, out2.NextNodeID)
}

func TestReActPlanner_MaxRoundsEnforced(t *testing.T) {
	p := planner.NewReActPlanner(2) // very low cap
	state := core.NewState()

	out, _ := p.Plan(context.Background(), "goal", state)
	tc := &core.ToolCallPart{CallID: "cx", ToolName: "any"}

	current := out.State.WithVariable("tool_call", tc)
	for i := 0; i < 10; i++ {
		out2, err := p.Revise(context.Background(), "", current)
		require.NoError(t, err)
		if out2.NextNodeID == planner.TerminalID {
			return // forced termination reached ✓
		}
		// Carry over the updated plan state and keep tool_call present.
		current = out2.State.WithVariable("tool_call", tc)
	}
	t.Fatal("expected planner to terminate after maxRounds")
}

