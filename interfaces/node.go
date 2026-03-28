package interfaces

import (
	"context"

	"github.com/FluxGraph/fluxgraph/core"
)

type InterruptType string

const (
	InterruptHumanApproval InterruptType = "HumanApproval"
	InterruptInputRequired InterruptType = "InputRequired"
	InterruptExternalEvent InterruptType = "ExternalEvent"
)

type InterruptSignal struct {
	Type      InterruptType
	Payload   map[string]any
	ResumeKey string // Key used in EventBus to resume execution
}

// NodeResult is emitted when a Node accomplishes processing. 
type NodeResult struct {
	State     *core.AgentState // Provides potentially updated AgentState clone
	NextNodes []string         // Next steps indicating priority override branching logic for GraphBuilder 
	Interrupt *InterruptSignal // HumanInTheLoop interception trigger
}

// Node acts as a step entity within the overall FluxGraph workflow mesh.
type Node interface {
	ID() string
	Process(ctx context.Context, state *core.AgentState) (*NodeResult, error)
}
