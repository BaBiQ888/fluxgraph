package interfaces

import (
	"context"

	"github.com/FluxGraph/fluxgraph/core"
)

type PlanStrategy string

const (
	StrategySequential PlanStrategy = "Sequential"
	StrategyParallel   PlanStrategy = "Parallel"
	StrategyAdaptive   PlanStrategy = "Adaptive"
)

type PlanStep struct {
	ID            string
	Description   string
	MappedNodeIDs []string // The target graph entities responsible for the step
	Dependencies  []string // List of prerequisite Step IDs before resolving this step
}

type Plan struct {
	Steps    []PlanStep
	Strategy PlanStrategy
}

// PlannerOutput is returned by Plan() and Revise() conveying both the routing
// decision and an updated AgentState enriched by planning logic.
type PlannerOutput struct {
	NextNodeID string
	State      *core.AgentState
}

// Planner operates cognitively ahead of the execution layer building Node stepping logic.
type Planner interface {
	Plan(ctx context.Context, goal string, state *core.AgentState) (*PlannerOutput, error)
	Revise(ctx context.Context, observation string, state *core.AgentState) (*PlannerOutput, error)
}
