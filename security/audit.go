package security

import (
	"fmt"
	"log"
	"os"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
)

// AuditLogHook records security-relevant events in a structured, append-only log.
// It uses a channel to ensure logging is asynchronous and non-blocking.
type AuditLogHook struct {
	logger *log.Logger
	ch     chan string
}

func NewAuditLogHook(logPath string) (*AuditLogHook, error) {
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	
	h := &AuditLogHook{
		logger: log.New(file, "SECURITY_AUDIT: ", log.Ldate|log.Ltime|log.LUTC),
		ch:     make(chan string, 1000), // Buffer size for non-blocking audit
	}

	// Logging loop
	go func() {
		for msg := range h.ch {
			h.logger.Println(msg)
		}
	}()

	return h, nil
}

func (h *AuditLogHook) OnHook(state *core.AgentState, meta engine.HookMeta) {
	tenantID := "default"
	if state != nil && state.Variables != nil {
		if tid, ok := state.Variables["tenant_id"].(string); ok && tid != "" {
			tenantID = tid
		}
	}

	switch meta.Point {
	case engine.HookBeforeNode:
		if meta.StepCount == 0 {
			h.log(fmt.Sprintf("[SESSION_STARTED] Tenant=%s Session=%s TaskID=%s", tenantID, state.ContextID, state.TaskID))
		}

	case engine.HookOnError:
		h.log(fmt.Sprintf("[EXECUTION_ERROR] Tenant=%s Node=%s Error=%v", tenantID, meta.NodeID, meta.Err))
	}
}

func (h *AuditLogHook) log(msg string) {
	select {
	case h.ch <- msg:
	default:
		// Channel full, drop or block? For audit logs, blocking might be required for compliance,
		// but dropping is safer for uptime. 
		// For this project, we'll try to log to stderr if channel is full.
		log.Printf("AUDIT_LOG_BUS_FULL: %s", msg)
	}
}

// LogViolation records a specific policy violation.
func (h *AuditLogHook) LogViolation(tenantID, sessionID, violationType, detail string) {
	h.log(fmt.Sprintf("[POLICY_VIOLATION] Type=%s Tenant=%s Session=%s Details=%s", 
		violationType, tenantID, sessionID, detail))
}

// Close gracefully stops the logging loop.
func (h *AuditLogHook) Close() {
	close(h.ch)
}
