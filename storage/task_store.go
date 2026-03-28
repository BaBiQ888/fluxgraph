package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
)

// RedisTaskStore implements interfaces.TaskStore using Redis.
type RedisTaskStore struct {
	client    RedisClient
	keyPrefix string // e.g. "fluxgraph"
	ttl       time.Duration
}

func NewRedisTaskStore(client RedisClient, keyPrefix string, ttl time.Duration) *RedisTaskStore {
	if keyPrefix == "" {
		keyPrefix = "fluxgraph"
	}
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour // 1 week default for tasks
	}
	return &RedisTaskStore{
		client:    client,
		keyPrefix: keyPrefix,
		ttl:       ttl,
	}
}

// Key formats:
// Task Data: fluxgraph:{tenantID}:task:{taskID} (Hash)
// Task History: fluxgraph:{tenantID}:task:{taskID}:history (List)
// Task Artifacts: fluxgraph:{tenantID}:task:{taskID}:artifacts (List)
// Context Index: fluxgraph:{tenantID}:context:{contextID}:tasks (List)
// Tenant Index: fluxgraph:{tenantID}:tasks (Sorted Set, score=updatedAt)

func (s *RedisTaskStore) taskKey(tenantID, taskID string) string {
	return fmt.Sprintf("%s:%s:task:%s", s.keyPrefix, tenantID, taskID)
}

func (s *RedisTaskStore) historyKey(tenantID, taskID string) string {
	return fmt.Sprintf("%s:%s:task:%s:history", s.keyPrefix, tenantID, taskID)
}

func (s *RedisTaskStore) artifactsKey(tenantID, taskID string) string {
	return fmt.Sprintf("%s:%s:task:%s:artifacts", s.keyPrefix, tenantID, taskID)
}

func (s *RedisTaskStore) contextKey(tenantID, contextID string) string {
	return fmt.Sprintf("%s:%s:context:%s:tasks", s.keyPrefix, tenantID, contextID)
}

func (s *RedisTaskStore) tenantIndexKey(tenantID string) string {
	return fmt.Sprintf("%s:%s:tasks", s.keyPrefix, tenantID)
}

func (s *RedisTaskStore) Create(ctx context.Context, task *core.Task) error {
	tenantID := task.TenantID
	if tenantID == "" {
		tenantID = "default"
	}
	
	key := s.taskKey(tenantID, task.ID)
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}

	return s.client.TxExec(ctx, func(tx RedisClient) error {
		// 1. Save main task data
		if err := tx.Set(ctx, key, string(data), s.ttl); err != nil {
			return err
		}
		// 2. Index by context
		if task.ContextID != "" {
			if err := tx.RPush(ctx, s.contextKey(tenantID, task.ContextID), task.ID); err != nil {
				return err
			}
		}
		// 3. Index by tenant (for listing)
		if err := tx.ZAdd(ctx, s.tenantIndexKey(tenantID), float64(task.UpdatedAt.UnixMilli()), task.ID); err != nil {
			return err
		}
		return nil
	})
}

func (s *RedisTaskStore) GetByID(ctx context.Context, taskID string) (*core.Task, error) {
	tenantID := tenantFromCtx(ctx)
	raw, err := s.client.Get(ctx, s.taskKey(tenantID, taskID))
	if err != nil {
		if errors.Is(err, ErrRedisKeyNotFound) {
			return nil, fmt.Errorf("task %s not found", taskID)
		}
		return nil, err
	}
	
	var task core.Task
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		return nil, err
	}
	
	// Hydrate history and artifacts if needed (optional optimization)
	return &task, nil
}

func (s *RedisTaskStore) UpdateStatus(ctx context.Context, taskID string, status core.TaskStatus) error {
	tenantID := tenantFromCtx(ctx)
	task, err := s.GetByID(ctx, taskID)
	if err != nil {
		return err
	}
	
	task.Status = status
	task.UpdatedAt = time.Now()
	
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	
	return s.client.TxExec(ctx, func(tx RedisClient) error {
		if err := tx.Set(ctx, s.taskKey(tenantID, taskID), string(data), s.ttl); err != nil {
			return err
		}
		return tx.ZAdd(ctx, s.tenantIndexKey(tenantID), float64(task.UpdatedAt.UnixMilli()), taskID)
	})
}

func (s *RedisTaskStore) AppendMessage(ctx context.Context, taskID string, message core.Message) error {
	tenantID := tenantFromCtx(ctx)
	key := s.historyKey(tenantID, taskID)
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return s.client.RPush(ctx, key, string(data))
}

func (s *RedisTaskStore) AppendArtifact(ctx context.Context, taskID string, artifact core.Artifact) error {
	tenantID := tenantFromCtx(ctx)
	key := s.artifactsKey(tenantID, taskID)
	data, err := json.Marshal(artifact)
	if err != nil {
		return err
	}
	return s.client.RPush(ctx, key, string(data))
}

func (s *RedisTaskStore) AddWebhook(ctx context.Context, taskID string, config core.WebhookConfig) error {
	tenantID := tenantFromCtx(ctx)
	task, err := s.GetByID(ctx, taskID)
	if err != nil {
		return err
	}

	task.Webhooks = append(task.Webhooks, config)
	task.UpdatedAt = time.Now()

	data, err := json.Marshal(task)
	if err != nil {
		return err
	}

	return s.client.TxExec(ctx, func(tx RedisClient) error {
		if err := tx.Set(ctx, s.taskKey(tenantID, taskID), string(data), s.ttl); err != nil {
			return err
		}
		return tx.ZAdd(ctx, s.tenantIndexKey(tenantID), float64(task.UpdatedAt.UnixMilli()), taskID)
	})
}

func (s *RedisTaskStore) ListByContextID(ctx context.Context, contextID string) ([]*core.Task, error) {
	tenantID := tenantFromCtx(ctx)
	ids, err := s.client.LRange(ctx, s.contextKey(tenantID, contextID), 0, -1)
	if err != nil {
		return nil, err
	}
	
	tasks := make([]*core.Task, 0, len(ids))
	for _, id := range ids {
		t, err := s.GetByID(ctx, id)
		if err == nil {
			tasks = append(tasks, t)
		}
	}
	return tasks, nil
}

func (s *RedisTaskStore) ListByTenantID(ctx context.Context, tenantID string, limit int, cursor string) ([]*core.Task, string, error) {
	// Simple implementation using ZRange (can be improved for cursor based pagination)
	ids, err := s.client.ZRange(ctx, s.tenantIndexKey(tenantID), 0, int64(limit-1))
	if err != nil {
		return nil, "", err
	}
	
	tasks := make([]*core.Task, 0, len(ids))
	for _, id := range ids {
		t, err := s.GetByID(ctx, id)
		if err == nil {
			tasks = append(tasks, t)
		}
	}
	
	return tasks, "", nil
}
