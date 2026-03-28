package interfaces

import (
	"context"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
)

type CheckpointMeta struct {
	CheckpointID string
	SessionID    string
	CreatedAt    time.Time
}

// MemoryStore defines standard semantics for storing and retrieving AgentState footprints.
type MemoryStore interface {
	Save(ctx context.Context, sessionID string, state *core.AgentState) (string, error) // Returns new CheckpointID after Snapshot
	Load(ctx context.Context, sessionID string) (*core.AgentState, error)
	LoadCheckpoint(ctx context.Context, checkpointID string) (*core.AgentState, error)
	ListCheckpoints(ctx context.Context, sessionID string) ([]CheckpointMeta, error)
	
	// AppendMessages prevents sending the heavy full State graph to store, limiting write io bounds. 
	AppendMessages(ctx context.Context, sessionID string, messages []core.Message) error
	
	// Search acts as semantic or keyword integration (Phase 1 interface, phase later implementations)
	Search(ctx context.Context, sessionID string, query string, topK int) ([]core.Message, error)
}
