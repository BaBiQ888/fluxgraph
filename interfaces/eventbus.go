package interfaces

import (
	"time"
)

type EventType string

const (
	EventAgentPaused    EventType = "AgentPaused"
	EventAgentResumed   EventType = "AgentResumed"
	EventTaskCompleted  EventType = "TaskCompleted"
	EventNodeStarted    EventType = "NodeStarted"
	EventNodeCompleted  EventType = "NodeCompleted"
	EventToolCalled     EventType = "ToolCalled"
)

type Event struct {
	Type      EventType
	SessionID string
	TaskID    string
	Payload   map[string]any
	Timestamp time.Time
}

type SubscriptionID string

type EventHandler func(event Event)

// EventBus is responsible for scalable message broadcasts through intra-process or remote backplanes. 
type EventBus interface {
	Publish(event Event) error
	Subscribe(eventType EventType, handler EventHandler) (SubscriptionID, error)
	Unsubscribe(id SubscriptionID) error
}
