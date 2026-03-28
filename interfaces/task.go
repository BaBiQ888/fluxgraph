package interfaces

import (
	"context"

	"github.com/FluxGraph/fluxgraph/core"
)

// TaskStore manages the persistence and retrieval of A2A Task objects.
type TaskStore interface {
	// Create saves a new task to the store.
	Create(ctx context.Context, task *core.Task) error

	// GetByID retrieves a single task by its unique TaskID.
	GetByID(ctx context.Context, taskID string) (*core.Task, error)

	// UpdateStatus updates only the status field of a task.
	UpdateStatus(ctx context.Context, taskID string, status core.TaskStatus) error

	// AppendMessage adds a message to the task's history.
	AppendMessage(ctx context.Context, taskID string, message core.Message) error

	// AppendArtifact adds an artifact to the task's artifacts list.
	AppendArtifact(ctx context.Context, taskID string, artifact core.Artifact) error

	// AddWebhook adds a new webhook configuration to the task.
	AddWebhook(ctx context.Context, taskID string, config core.WebhookConfig) error

	// ListByContextID returns all tasks associated with a given contextId.
	ListByContextID(ctx context.Context, contextID string) ([]*core.Task, error)

	// ListByTenantID returns tasks for a tenant, supporting optional filtering and pagination.
	ListByTenantID(ctx context.Context, tenantID string, limit int, cursor string) ([]*core.Task, string, error)
}
