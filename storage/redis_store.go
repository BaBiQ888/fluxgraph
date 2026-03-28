package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/google/uuid"
)

// RedisClient is the minimal interface we need from go-redis, allowing easy
// mocking in tests without importing the full go-redis package here.
// The concrete wiring lives in storage/redis_client.go.
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	RPush(ctx context.Context, key string, values ...string) error
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	LTrim(ctx context.Context, key string, start, stop int64) error
	ZAdd(ctx context.Context, key string, score float64, member string) error
	ZRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	ZRemRangeByRank(ctx context.Context, key string, start, stop int64) error
	// TxPipeline returns a closure that executes commands atomically.
	TxExec(ctx context.Context, fn func(tx RedisClient) error) error
}

var (
	ErrRedisKeyNotFound = errors.New("redis: key not found")
	ErrStateCorrupted   = errors.New("state corrupted: concurrent write conflict")
)

// RedisMemoryStore implements interfaces.MemoryStore using Redis for fast,
// persistent, multi-tenant state management.
type RedisMemoryStore struct {
	client        RedisClient
	keyPrefix     string        // e.g. "fluxgraph"
	stateTTL      time.Duration // session expiry
	maxCheckpoints int
	maxMessages   int           // sliding window size
}

type RedisMemoryStoreOptions struct {
	KeyPrefix      string
	StateTTL       time.Duration
	MaxCheckpoints int
	MaxMessages    int
}

func NewRedisMemoryStore(client RedisClient, opts RedisMemoryStoreOptions) *RedisMemoryStore {
	if opts.KeyPrefix == "" {
		opts.KeyPrefix = "fluxgraph"
	}
	if opts.StateTTL == 0 {
		opts.StateTTL = 24 * time.Hour
	}
	if opts.MaxCheckpoints == 0 {
		opts.MaxCheckpoints = 50
	}
	if opts.MaxMessages == 0 {
		opts.MaxMessages = 200
	}
	return &RedisMemoryStore{
		client:         client,
		keyPrefix:      opts.KeyPrefix,
		stateTTL:       opts.StateTTL,
		maxCheckpoints: opts.MaxCheckpoints,
		maxMessages:    opts.MaxMessages,
	}
}

// ---- key helpers ----

func (s *RedisMemoryStore) stateKey(tenantID, sessionID string) string {
	return fmt.Sprintf("%s:%s:%s:state", s.keyPrefix, tenantID, sessionID)
}

func (s *RedisMemoryStore) ckptIndexKey(tenantID, sessionID string) string {
	return fmt.Sprintf("%s:%s:%s:ckpts", s.keyPrefix, tenantID, sessionID)
}

func (s *RedisMemoryStore) ckptDataKey(checkpointID string) string {
	return fmt.Sprintf("%s:ckpt:%s", s.keyPrefix, checkpointID)
}

func (s *RedisMemoryStore) messagesKey(tenantID, sessionID string) string {
	return fmt.Sprintf("%s:%s:%s:msgs", s.keyPrefix, tenantID, sessionID)
}

func tenantFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value("tenantID").(string); ok {
		return v
	}
	return "default"
}

// ---- Save ----

func (s *RedisMemoryStore) Save(ctx context.Context, sessionID string, state *core.AgentState) (string, error) {
	tenantID := tenantFromCtx(ctx)
	ckptID := uuid.New().String()
	now := time.Now()

	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}

	ckptMeta, err := json.Marshal(interfaces.CheckpointMeta{
		CheckpointID: ckptID,
		SessionID:    sessionID,
		CreatedAt:    now,
	})
	if err != nil {
		return "", err
	}

	err = s.client.TxExec(ctx, func(tx RedisClient) error {
		// 1. Persist the main state snapshot.
		if e := tx.Set(ctx, s.stateKey(tenantID, sessionID), string(data), s.stateTTL); e != nil {
			return e
		}
		// 2. Store checkpoint data.
		if e := tx.Set(ctx, s.ckptDataKey(ckptID), string(data), s.stateTTL*2); e != nil {
			return e
		}
		// 3. Add to sorted-set index (score = unix timestamp for ordered recall).
		if e := tx.ZAdd(ctx, s.ckptIndexKey(tenantID, sessionID), float64(now.UnixMilli()), string(ckptMeta)); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	// Prune excess checkpoints outside the transaction (best-effort).
	go s.pruneCheckpoints(context.Background(), tenantID, sessionID)

	return ckptID, nil
}

func (s *RedisMemoryStore) pruneCheckpoints(ctx context.Context, tenantID, sessionID string) {
	key := s.ckptIndexKey(tenantID, sessionID)
	// Keep only the latest maxCheckpoints; remove all earlier ones.
	_ = s.client.ZRemRangeByRank(ctx, key, 0, int64(-s.maxCheckpoints-1))
}

// ---- Load ----

func (s *RedisMemoryStore) Load(ctx context.Context, sessionID string) (*core.AgentState, error) {
	tenantID := tenantFromCtx(ctx)
	raw, err := s.client.Get(ctx, s.stateKey(tenantID, sessionID))
	if err != nil {
		if errors.Is(err, ErrRedisKeyNotFound) {
			return nil, fmt.Errorf("session %s not found", sessionID)
		}
		return nil, err
	}
	var state core.AgentState
	if e := json.Unmarshal([]byte(raw), &state); e != nil {
		return nil, e
	}
	return &state, nil
}

// ---- LoadCheckpoint ----

func (s *RedisMemoryStore) LoadCheckpoint(ctx context.Context, checkpointID string) (*core.AgentState, error) {
	raw, err := s.client.Get(ctx, s.ckptDataKey(checkpointID))
	if err != nil {
		if errors.Is(err, ErrRedisKeyNotFound) {
			return nil, fmt.Errorf("checkpoint %s not found", checkpointID)
		}
		return nil, err
	}
	var state core.AgentState
	if e := json.Unmarshal([]byte(raw), &state); e != nil {
		return nil, e
	}
	return &state, nil
}

// ---- ListCheckpoints ----

func (s *RedisMemoryStore) ListCheckpoints(ctx context.Context, sessionID string) ([]interfaces.CheckpointMeta, error) {
	tenantID := tenantFromCtx(ctx)
	members, err := s.client.ZRange(ctx, s.ckptIndexKey(tenantID, sessionID), 0, -1)
	if err != nil {
		return nil, err
	}
	metas := make([]interfaces.CheckpointMeta, 0, len(members))
	for _, m := range members {
		var meta interfaces.CheckpointMeta
		if e := json.Unmarshal([]byte(m), &meta); e == nil {
			metas = append(metas, meta)
		}
	}
	// Return most-recent first.
	for i, j := 0, len(metas)-1; i < j; i, j = i+1, j-1 {
		metas[i], metas[j] = metas[j], metas[i]
	}
	return metas, nil
}

// ---- AppendMessages ----

func (s *RedisMemoryStore) AppendMessages(ctx context.Context, sessionID string, messages []core.Message) error {
	tenantID := tenantFromCtx(ctx)
	key := s.messagesKey(tenantID, sessionID)

	for _, msg := range messages {
		b, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		if err := s.client.RPush(ctx, key, string(b)); err != nil {
			return err
		}
	}

	// Sliding-window trim: keep only the most recent maxMessages.
	return s.client.LTrim(ctx, key, int64(-s.maxMessages), -1)
}

// ---- Search (Phase 5: Vector RAG stub) ----

func (s *RedisMemoryStore) Search(_ context.Context, _ string, _ string, _ int) ([]core.Message, error) {
	return nil, nil // Full vector search implemented in Phase 5
}
