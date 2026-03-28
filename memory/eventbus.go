package memory

import (
	"sync"

	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/google/uuid"
)

// InMemoryEventBus delivers events synchronously immediately to attached handlers in local process boundaries.
type InMemoryEventBus struct {
	mu          sync.RWMutex
	subscribers map[interfaces.EventType]map[interfaces.SubscriptionID]interfaces.EventHandler
}

func NewInMemoryEventBus() *InMemoryEventBus {
	return &InMemoryEventBus{
		subscribers: make(map[interfaces.EventType]map[interfaces.SubscriptionID]interfaces.EventHandler),
	}
}

func (b *InMemoryEventBus) Publish(event interfaces.Event) error {
	b.mu.RLock()
	handlersMap, ok := b.subscribers[event.Type]
	b.mu.RUnlock()

	if !ok {
		return nil
	}

	// Copy bounds to avoid deadlocking if an event handler adds/removes to Bus
	handlers := make([]interfaces.EventHandler, 0, len(handlersMap))
	
	b.mu.RLock()
	for _, h := range handlersMap {
		handlers = append(handlers, h)
	}
	b.mu.RUnlock()

	for _, handler := range handlers {
		handler(event)
	}
	return nil
}

func (b *InMemoryEventBus) Subscribe(eventType interfaces.EventType, handler interfaces.EventHandler) (interfaces.SubscriptionID, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscribers[eventType]; !ok {
		b.subscribers[eventType] = make(map[interfaces.SubscriptionID]interfaces.EventHandler)
	}

	subID := interfaces.SubscriptionID(uuid.New().String())
	b.subscribers[eventType][subID] = handler

	return subID, nil
}

func (b *InMemoryEventBus) Unsubscribe(id interfaces.SubscriptionID) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, handlers := range b.subscribers {
		if _, exists := handlers[id]; exists {
			delete(handlers, id)
			return nil
		}
	}
	return nil
}
