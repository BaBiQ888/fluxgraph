package observability

import (
	"context"
	"fmt"
	"sort"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
)

// StateSnapshot represents a point-in-time view of an agent session.
type StateSnapshot struct {
	CheckpointID string          `json:"checkpointId"`
	Timestamp     int64           `json:"timestamp"`
	NodeID        string          `json:"nodeId"`
	Status        core.AgentStatus `json:"status"`
	StepCount     int             `json:"stepCount"`
}

// StateInspector provides debugging and introspection capabilities for agent sessions.
type StateInspector struct {
	memoryStore interfaces.MemoryStore
}

func NewStateInspector(mem interfaces.MemoryStore) *StateInspector {
	return &StateInspector{memoryStore: mem}
}

// GetTimeline returns a chronological list of checkpoints for a specific session.
func (i *StateInspector) GetTimeline(ctx context.Context, sessionID string) ([]StateSnapshot, error) {
	metas, err := i.memoryStore.ListCheckpoints(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	snapshots := make([]StateSnapshot, 0, len(metas))
	for _, m := range metas {
		// We might need to load the full state to get NodeID/Status/StepCount
		// but typically ListCheckpoints might return enough metadata if optimized.
		// For now, we load the full state for each checkpoint (simplified implementation).
		state, err := i.memoryStore.LoadCheckpoint(ctx, m.CheckpointID)
		if err != nil {
			continue
		}
		snapshots = append(snapshots, StateSnapshot{
			CheckpointID: m.CheckpointID,
			Timestamp:     m.CreatedAt.UnixMilli(),
			NodeID:        state.LastNodeID,
			Status:        state.Status,
			StepCount:     state.StepCount,
		})
	}

	// Ensure chronological order
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp < snapshots[j].Timestamp
	})

	return snapshots, nil
}

// StateDiff summarizes the changes between two agent states.
type StateDiff struct {
	NewMessages  []core.Message         `json:"newMessages"`
	ChangedVars  map[string]any         `json:"changedVariables"`
	NewArtifacts []core.Artifact        `json:"newArtifacts"`
}

// DiffCheckpoints calculates what changed from cpA to cpB.
func (i *StateInspector) DiffCheckpoints(ctx context.Context, cpA, cpB string) (*StateDiff, error) {
	stateA, err := i.memoryStore.LoadCheckpoint(ctx, cpA)
	if err != nil {
		return nil, err
	}
	stateB, err := i.memoryStore.LoadCheckpoint(ctx, cpB)
	if err != nil {
		return nil, err
	}

	diff := &StateDiff{
		ChangedVars: make(map[string]any),
	}

	// 1. Messages diff (simple append-only assumption)
	if len(stateB.Messages) > len(stateA.Messages) {
		diff.NewMessages = stateB.Messages[len(stateA.Messages):]
	}

	// 2. Variables diff
	for k, vB := range stateB.Variables {
		if vA, ok := stateA.Variables[k]; !ok || fmt.Sprintf("%v", vA) != fmt.Sprintf("%v", vB) {
			diff.ChangedVars[k] = vB
		}
	}

	// 3. Artifacts diff
	if len(stateB.Artifacts) > len(stateA.Artifacts) {
		diff.NewArtifacts = stateB.Artifacts[len(stateA.Artifacts):]
	}

	return diff, nil
}

// MermaidGantt converts snapshots to a Mermaid-compatible task timeline.
func (i *StateInspector) MermaidGantt(snapshots []StateSnapshot) string {
	out := "gantt\n    title Agent Session Timeline\n    dateFormat X\n    axisFormat %H:%M:%S\n"
	for idx, s := range snapshots {
		end := s.Timestamp + 1000 // default 1s if it's the last one
		if idx < len(snapshots)-1 {
			end = snapshots[idx+1].Timestamp
		}
		out += fmt.Sprintf("    section Node %s\n", s.NodeID)
		out += fmt.Sprintf("    Step %d : %d, %d\n", s.StepCount, s.Timestamp/1000, end/1000)
	}
	return out
}

// ReplayFrom prepares a state for re-execution from a specific checkpoint.
func (i *StateInspector) ReplayFrom(ctx context.Context, checkpointID string) (*core.AgentState, error) {
	state, err := i.memoryStore.LoadCheckpoint(ctx, checkpointID)
	if err != nil {
		return nil, err
	}

	// Prepare for re-execution: 
	// 1. Set status back to Running (or let Engine handle it)
	// 2. We keep memory/history as is, but the engine will pick up from LastNodeID.
	replayState := state.WithStatus(core.StatusRunning)
	return replayState, nil
}
