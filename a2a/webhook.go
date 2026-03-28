package a2a

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
)

// WebhookDispatcher listens to the EventBus and pushes events to registered webhooks.
type WebhookDispatcher struct {
	taskStore interfaces.TaskStore
	bus       interfaces.EventBus
	httpClient *http.Client
}

func NewWebhookDispatcher(ts interfaces.TaskStore, bus interfaces.EventBus) *WebhookDispatcher {
	return &WebhookDispatcher{
		taskStore:  ts,
		bus:        bus,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Start begins listening to the EventBus.
func (d *WebhookDispatcher) Start(ctx context.Context) error {
	handler := func(ev interfaces.Event) {
		go d.dispatch(ev)
	}

	// Subscribe to relevant events
	points := []interfaces.EventType{
		interfaces.EventAgentPaused,
		interfaces.EventTaskCompleted,
		interfaces.EventNodeCompleted,
	}

	for _, p := range points {
		if _, err := d.bus.Subscribe(p, handler); err != nil {
			return err
		}
	}

	return nil
}

func (d *WebhookDispatcher) dispatch(ev interfaces.Event) {
	if ev.TaskID == "" {
		return
	}

	// Need a context for task store retrieval
	ctx := context.Background()
	// How to get TenantID? 
	// Event should probably carry TenantID or it's embedded in SessionID.
	// For now we assume default tenant if not provided.
	
	task, err := d.taskStore.GetByID(ctx, ev.TaskID)
	if err != nil {
		return
	}

	if len(task.Webhooks) == 0 {
		return
	}

	payload, _ := json.Marshal(ev)

	for _, cfg := range task.Webhooks {
		// Filter by event type if configured
		if len(cfg.EventTypes) > 0 {
			match := false
			for _, t := range cfg.EventTypes {
				if interfaces.EventType(t) == ev.Type {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		d.sendWithRetry(cfg, payload)
	}
}

func (d *WebhookDispatcher) sendWithRetry(cfg core.WebhookConfig, payload []byte) {
	// Simple retry logic (can be enhanced with exponential backoff)
	for i := 0; i < 3; i++ {
		if err := d.send(cfg, payload); err == nil {
			return
		}
		time.Sleep(time.Duration(i+1) * time.Second)
	}
}

func (d *WebhookDispatcher) send(cfg core.WebhookConfig, payload []byte) error {
	req, err := http.NewRequest("POST", cfg.URL, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-A2A-Event", "fluxgraph")

	// Custom headers
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	// HMAC Signature
	if cfg.Secret != "" {
		sig := generateSignature(payload, cfg.Secret)
		req.Header.Set("X-A2A-Signature", sig)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook failed with status %d", resp.StatusCode)
	}

	return nil
}

func generateSignature(payload []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}
