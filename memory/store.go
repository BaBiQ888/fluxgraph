package memory

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/google/uuid"
)

var (
	ErrSessionNotFound    = errors.New("session not found")
	ErrCheckpointNotFound = errors.New("checkpoint not found")
)

// InMemoryStore implements MemoryStore leveraging RWMutex boundaries ensuring completely isolated snapshot copies utilizing json marshalling.
type InMemoryStore struct {
	mu          sync.RWMutex
	states      map[string][]byte
	checkpoints map[string][]interfaces.CheckpointMeta
	ckptData    map[string][]byte
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		states:      make(map[string][]byte),
		checkpoints: make(map[string][]interfaces.CheckpointMeta),
		ckptData:    make(map[string][]byte),
	}
}

func (s *InMemoryStore) Save(ctx context.Context, sessionID string, state *core.AgentState) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}

	ckptID := uuid.New().String()
	meta := interfaces.CheckpointMeta{
		CheckpointID: ckptID,
		SessionID:    sessionID,
		CreatedAt:    time.Now(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.states[sessionID] = data
	s.checkpoints[sessionID] = append(s.checkpoints[sessionID], meta)
	s.ckptData[ckptID] = data

	return ckptID, nil
}

func (s *InMemoryStore) Load(ctx context.Context, sessionID string) (*core.AgentState, error) {
	s.mu.RLock()
	data, ok := s.states[sessionID]
	s.mu.RUnlock()

	if !ok {
		return nil, ErrSessionNotFound
	}

	var state core.AgentState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *InMemoryStore) LoadCheckpoint(ctx context.Context, checkpointID string) (*core.AgentState, error) {
	s.mu.RLock()
	data, ok := s.ckptData[checkpointID]
	s.mu.RUnlock()

	if !ok {
		return nil, ErrCheckpointNotFound
	}

	var state core.AgentState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *InMemoryStore) ListCheckpoints(ctx context.Context, sessionID string) ([]interfaces.CheckpointMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	meta, ok := s.checkpoints[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	
	result := make([]interfaces.CheckpointMeta, len(meta))
	copy(result, meta)
	return result, nil
}

func (s *InMemoryStore) AppendMessages(ctx context.Context, sessionID string, messages []core.Message) error {
	state, err := s.Load(ctx, sessionID)
	if err != nil {
		return err
	}
	state.Messages = append(state.Messages, messages...)
	_, err = s.Save(ctx, sessionID, state)
	return err
}

func (s *InMemoryStore) Search(ctx context.Context, sessionID string, query string, topK int) ([]core.Message, error) {
	// Not implemented for Phase 1
	return nil, nil
}
