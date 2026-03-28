package planner

import (
	"context"
	"errors"
	"fmt"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
)

// StepStatus tracks the lifecycle of each planned step.
type StepStatus string

const (
	StepPending   StepStatus = "Pending"
	StepRunning   StepStatus = "Running"
	StepCompleted StepStatus = "Completed"
	StepFailed    StepStatus = "Failed"
	StepSkipped   StepStatus = "Skipped"
)

// PlanStep extends the base concept with execution tracking fields.
type PlanStep struct {
	NodeID       string
	ActualNodeID string     // may differ from NodeID if rerouted
	Status       StepStatus
	Observation  string     // result fed back after execution
}

// Plan holds the ordered sequence of steps for the current session.
type Plan struct {
	Goal  string
	Steps []PlanStep
}

// CurrentStep returns the first Pending or Running step, or nil when all
// steps are done.
func (p *Plan) CurrentStep() *PlanStep {
	for i := range p.Steps {
		if p.Steps[i].Status == StepPending || p.Steps[i].Status == StepRunning {
			return &p.Steps[i]
		}
	}
	return nil
}

// Append adds a step at the end of the plan.
func (p *Plan) Append(nodeID string) {
	p.Steps = append(p.Steps, PlanStep{NodeID: nodeID, Status: StepPending})
}

// --- ReActPlanner ---

// thinkNodeID and toolNodeID are the symbolic node names the planner emits;
// the owning graph must register nodes with these exact IDs.
const (
	ThinkNodeID = "LLMNode"
	ToolNodeID  = "ToolExecutorNode"
	TerminalID  = "__terminal__"
)

// ReActPlanner implements a dynamic single-step Think→Act→Observe planning
// strategy. It reads the current AgentState to decide what the next step
// should be and stores the evolving Plan in AgentState.Variables["__plan__"].
type ReActPlanner struct {
	maxRounds int
}

func NewReActPlanner(maxRounds int) *ReActPlanner {
	if maxRounds <= 0 {
		maxRounds = 20
	}
	return &ReActPlanner{maxRounds: maxRounds}
}

func getPlan(state *core.AgentState) *Plan {
	if state.Variables == nil {
		return nil
	}
	raw, ok := state.Variables["__plan__"]
	if !ok {
		return nil
	}
	plan, _ := raw.(*Plan)
	return plan
}

func setPlan(state *core.AgentState, plan *Plan) *core.AgentState {
	return state.WithVariable("__plan__", plan)
}

// Plan initialises a new plan for the given goal and seeds the first Think step.
func (r *ReActPlanner) Plan(
	ctx context.Context,
	goal string,
	state *core.AgentState,
) (*interfaces.PlannerOutput, error) {
	plan := &Plan{
		Goal:  goal,
		Steps: []PlanStep{{NodeID: ThinkNodeID, Status: StepPending}},
	}
	newState := setPlan(state.WithVariable("goal", goal), plan)
	return &interfaces.PlannerOutput{
		NextNodeID: ThinkNodeID,
		State:      newState,
	}, nil
}

// Revise is called after each node execution. It inspects the LLM output /
// tool result to decide whether to append a ToolExecutorNode step, terminate,
// or append another Think step.
func (r *ReActPlanner) Revise(
	ctx context.Context,
	observation string,
	state *core.AgentState,
) (*interfaces.PlannerOutput, error) {
	plan := getPlan(state)
	if plan == nil {
		return nil, errors.New("ReActPlanner: no active plan in state")
	}

	// Mark the most-recently running step as completed.
	for i := range plan.Steps {
		if plan.Steps[i].Status == StepRunning {
			plan.Steps[i].Status = StepCompleted
			plan.Steps[i].Observation = observation
			break
		}
	}

	// Guard against infinite loops: count all completed steps.
	completedTotal := 0
	for _, s := range plan.Steps {
		if s.Status == StepCompleted {
			completedTotal++
		}
	}
	if completedTotal >= r.maxRounds {
		plan.Append(TerminalID)
		newState := setPlan(state, plan)
		return &interfaces.PlannerOutput{NextNodeID: TerminalID, State: newState}, nil
	}

	// Inspect state for tool call signal set by LLMNode.
	tcRaw, hasToolCall := state.Variables["tool_call"]
	toolCallPresent := hasToolCall && tcRaw != nil

	var nextNodeID string
	if toolCallPresent {
		// LLM wants to call a tool → Act step.
		plan.Append(ToolNodeID)
		nextNodeID = ToolNodeID
	} else if observation != "" {
		// We just got a tool result → Think again (Observe → Think).
		plan.Append(ThinkNodeID)
		nextNodeID = ThinkNodeID
	} else {
		// No tool call, no observation → final answer, terminate.
		plan.Append(TerminalID)
		nextNodeID = TerminalID
	}

	// Mark the new step as Running.
	for i := range plan.Steps {
		if plan.Steps[i].Status == StepPending {
			plan.Steps[i].Status = StepRunning
			break
		}
	}

	newState := setPlan(state, plan)
	return &interfaces.PlannerOutput{NextNodeID: nextNodeID, State: newState}, nil
}

// Summarise returns a human-readable plan dump (useful for debugging / logs).
func (r *ReActPlanner) Summarise(plan *Plan) string {
	out := fmt.Sprintf("Goal: %s\n", plan.Goal)
	for i, s := range plan.Steps {
		out += fmt.Sprintf("  [%d] %s → %s\n", i+1, s.NodeID, s.Status)
		if s.Observation != "" {
			out += fmt.Sprintf("       obs: %s\n", s.Observation)
		}
	}
	return out
}
