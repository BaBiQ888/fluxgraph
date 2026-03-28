package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/FluxGraph/fluxgraph/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- in-process stub implementing storage.RedisClient ----

type stubRedis struct {
	mu      sync.Mutex
	strings map[string]string
	lists   map[string][]string
	zsets   map[string][]zMember
}

type zMember struct {
	score  float64
	member string
}

func newStubRedis() *stubRedis {
	return &stubRedis{
		strings: make(map[string]string),
		lists:   make(map[string][]string),
		zsets:   make(map[string][]zMember),
	}
}

func (r *stubRedis) Get(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.strings[key]
	if !ok {
		return "", storage.ErrRedisKeyNotFound
	}
	return v, nil
}
func (r *stubRedis) Set(_ context.Context, key, value string, _ time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.strings[key] = value
	return nil
}
func (r *stubRedis) RPush(_ context.Context, key string, vals ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lists[key] = append(r.lists[key], vals...)
	return nil
}
func (r *stubRedis) LRange(_ context.Context, key string, start, stop int64) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := r.lists[key]
	if len(l) == 0 {
		return nil, nil
	}
	n := int64(len(l))
	if stop < 0 { stop = n + stop }
	if start < 0 { start = n + start }
	if start < 0 { start = 0 }
	if stop >= n { stop = n - 1 }
	return append([]string{}, l[start:stop+1]...), nil
}
func (r *stubRedis) LTrim(_ context.Context, key string, start, stop int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := r.lists[key]
	if len(l) == 0 {
		return nil
	}
	n := int64(len(l))
	if start < 0 { start = n + start }
	if stop < 0 { stop = n + stop }
	if start < 0 { start = 0 }
	if stop >= n { stop = n - 1 }
	r.lists[key] = l[start : stop+1]
	return nil
}
func (r *stubRedis) ZAdd(_ context.Context, key string, score float64, member string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.zsets[key] = append(r.zsets[key], zMember{score, member})
	return nil
}
func (r *stubRedis) ZRange(_ context.Context, key string, _, _ int64) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	z := r.zsets[key]
	out := make([]string, 0, len(z))
	for _, m := range z {
		out = append(out, m.member)
	}
	return out, nil
}
func (r *stubRedis) ZRemRangeByRank(_ context.Context, key string, start, stop int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	z := r.zsets[key]
	n := int64(len(z))
	if n == 0 {
		return nil
	}
	if start < 0 { start = n + start }
	if stop < 0 { stop = n + stop }
	if start < 0 { start = 0 }
	if stop >= n { stop = n - 1 }
	if start > stop {
		return nil
	}
	r.zsets[key] = append(z[:start], z[stop+1:]...)
	return nil
}
func (r *stubRedis) TxExec(_ context.Context, fn func(tx storage.RedisClient) error) error {
	return fn(r)
}

// ---- Tests ----

func TestRedisMemoryStore_SaveAndLoad(t *testing.T) {
	stub := newStubRedis()
	store := storage.NewRedisMemoryStore(stub, storage.RedisMemoryStoreOptions{})

	state := core.NewState().WithStatus(core.StatusRunning)
	state = state.WithMessage(core.Message{
		Role:  core.RoleUser,
		Parts: []core.Part{{Type: core.PartTypeText, Text: "hello"}},
	})

	ckptID, err := store.Save(context.Background(), "sess1", state)
	require.NoError(t, err)
	assert.NotEmpty(t, ckptID)

	loaded, err := store.Load(context.Background(), "sess1")
	require.NoError(t, err)
	assert.Equal(t, core.StatusRunning, loaded.Status)
	assert.Len(t, loaded.Messages, 1)
}

func TestRedisMemoryStore_LoadCheckpoint(t *testing.T) {
	stub := newStubRedis()
	store := storage.NewRedisMemoryStore(stub, storage.RedisMemoryStoreOptions{})

	state := core.NewState().WithStatus(core.StatusCompleted)
	ckptID, err := store.Save(context.Background(), "sess2", state)
	require.NoError(t, err)

	loaded, err := store.LoadCheckpoint(context.Background(), ckptID)
	require.NoError(t, err)
	assert.Equal(t, core.StatusCompleted, loaded.Status)
}

func TestRedisMemoryStore_ListCheckpoints(t *testing.T) {
	stub := newStubRedis()
	store := storage.NewRedisMemoryStore(stub, storage.RedisMemoryStoreOptions{})

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, err := store.Save(ctx, "sess3", core.NewState())
		require.NoError(t, err)
	}

	metas, err := store.ListCheckpoints(ctx, "sess3")
	require.NoError(t, err)
	assert.Len(t, metas, 3)
	// Most-recent first.
	for _, m := range metas {
		assert.Equal(t, "sess3", m.SessionID)
	}
}

func TestRedisMemoryStore_Load_NotFound(t *testing.T) {
	stub := newStubRedis()
	store := storage.NewRedisMemoryStore(stub, storage.RedisMemoryStoreOptions{})

	_, err := store.Load(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestRedisMemoryStore_AppendMessages_SlidingWindow(t *testing.T) {
	stub := newStubRedis()
	store := storage.NewRedisMemoryStore(stub, storage.RedisMemoryStoreOptions{MaxMessages: 3})

	// First save to create session.
	_, err := store.Save(context.Background(), "sess4", core.NewState())
	require.NoError(t, err)

	msgs := make([]core.Message, 5)
	for i := range msgs {
		msgs[i] = core.Message{Role: core.RoleUser, Parts: []core.Part{{Type: core.PartTypeText, Text: "m"}}}
	}
	err = store.AppendMessages(context.Background(), "sess4", msgs)
	require.NoError(t, err)

	// List key should be trimmed to 3.
	key := "fluxgraph:default:sess4:msgs"
	list := stub.lists[key]
	assert.LessOrEqual(t, len(list), 3)
}

// Verify CheckpointMeta round-trips through JSON correctly (used by ZAdd/ZRange).
func TestCheckpointMetaRoundTrip(t *testing.T) {
	meta := interfaces.CheckpointMeta{
		CheckpointID: "abc",
		SessionID:    "s1",
		CreatedAt:    time.Now().UTC().Truncate(time.Millisecond),
	}
	b, err := json.Marshal(meta)
	require.NoError(t, err)
	var out interfaces.CheckpointMeta
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, meta.CheckpointID, out.CheckpointID)
	assert.Equal(t, meta.SessionID, out.SessionID)
}

// Stub that always returns ErrRedisKeyNotFound to test error paths.
type errRedis struct{ stubRedis }

func (e *errRedis) Get(_ context.Context, _ string) (string, error) {
	return "", storage.ErrRedisKeyNotFound
}

func TestRedisMemoryStore_LoadCheckpoint_NotFound(t *testing.T) {
	store := storage.NewRedisMemoryStore(&errRedis{*newStubRedis()}, storage.RedisMemoryStoreOptions{})
	_, err := store.LoadCheckpoint(context.Background(), "bad-ckpt")
	assert.Error(t, err)
	assert.False(t, errors.Is(err, storage.ErrRedisKeyNotFound)) // wrapped
}
