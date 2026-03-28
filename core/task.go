package core

import (
	"time"
)

// TaskState represents the lifecycle stage of an A2A task.
type TaskState string

const (
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateInputRequired TaskState = "input_required"
	TaskStateAuthRequired  TaskState = "auth_required"
	TaskStateCompleted     TaskState = "completed"
	TaskStateFailed        TaskState = "failed"
	TaskStateCanceled      TaskState = "canceled"
	TaskStateRejected      TaskState = "rejected"
)

// WebhookConfig defines the destination and security for event push.
type WebhookConfig struct {
	ID         string            `json:"id"`
	URL        string            `json:"url"`
	Secret     string            `json:"secret,omitempty"` // HMAC secret
	EventTypes []string          `json:"eventTypes,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}

// TaskStatus provides detailed status information for a Task.
type TaskStatus struct {
	State     TaskState `json:"state"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message,omitempty"`
}

// Task is the job-level abstraction for A2A communication.
type Task struct {
	ID        string         `json:"id"`
	ContextID string         `json:"contextId"`
	TenantID  string         `json:"tenantId,omitempty"`
	Status    TaskStatus     `json:"status"`
	History   []Message      `json:"history"`
	Artifacts []Artifact     `json:"artifacts"`
	Webhooks  []WebhookConfig `json:"webhooks,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// MapAgentStatusToTaskState translates internal engine status to A2A state.
func MapAgentStatusToTaskState(s AgentStatus) TaskState {
	switch s {
	case StatusRunning:
		return TaskStateWorking
	case StatusPaused:
		return TaskStateInputRequired
	case StatusWaitingHuman:
		return TaskStateInputRequired
	case StatusCompleted:
		return TaskStateCompleted
	case StatusFailed:
		return TaskStateFailed
	default:
		return TaskStateWorking
	}
}
